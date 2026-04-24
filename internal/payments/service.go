package payments

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gloss/internal/payments/hdfc"
	"gloss/internal/shared/enums"
	apperrors "gloss/internal/shared/errors"
)

type hdfcSaleClient interface {
	CreateSale(ctx context.Context, req hdfc.CreateSaleRequest) (hdfc.TransactionResponse, error)
}

type auditRecorder interface {
	RecordPaymentEvent(
		ctx context.Context,
		tenantID string,
		storeID string,
		paymentID string,
		performedByUserID string,
		action string,
		metadata map[string]any,
	) error
}

type Service struct {
	repo   *Repo
	client hdfcSaleClient
	audit  auditRecorder
	logger *slog.Logger
	now    func() time.Time
}

func NewService(repo *Repo, client hdfcSaleClient, audit auditRecorder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:   repo,
		client: client,
		audit:  audit,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) EnsureStoreReadyForOnline(ctx context.Context, tenantID string, storeID string) error {
	cfg, err := s.repo.GetStoreTerminalConfig(ctx, tenantID, storeID)
	if err != nil {
		return err
	}
	return validateTerminalTID(cfg.HDFCTerminalTID)
}

func (s *Service) InitiateBillOnlinePayment(
	ctx context.Context,
	tenantID string,
	storeID string,
	billID string,
	paymentID string,
	performedByUserID string,
) error {
	if s.client == nil {
		return apperrors.New(apperrors.CodeInternalError, "HDFC payment client is not configured")
	}

	payment, err := s.repo.GetPaymentForSale(ctx, tenantID, storeID, billID, paymentID)
	if err != nil {
		return err
	}
	if payment.Status != string(enums.PaymentStatusInitiated) {
		return nil
	}
	if payment.ProviderSaleRequestedAt != nil {
		s.logger.Info(
			"HDFC sale initiation skipped because sale was already requested",
			"tenant_id", tenantID,
			"store_id", storeID,
			"bill_id", billID,
			"payment_id", paymentID,
			"provider_request_id", payment.ProviderRequestID,
		)
		return nil
	}

	storeConfig, err := s.repo.GetStoreTerminalConfig(ctx, tenantID, storeID)
	if err != nil {
		return err
	}
	if err := validateTerminalTID(storeConfig.HDFCTerminalTID); err != nil {
		return err
	}

	saleTxnID := strings.TrimSpace(payment.ProviderRequestID)
	if saleTxnID == "" {
		saleTxnID = newSaleTxnID(payment.ID)
	}

	requestedAt := s.now()
	claim, err := s.repo.ClaimSaleRequest(ctx, payment.ID, saleTxnID, storeConfig.HDFCTerminalTID, requestedAt)
	if err != nil {
		return err
	}

	response, err := s.client.CreateSale(ctx, hdfc.CreateSaleRequest{
		TID:         claim.TerminalTID,
		SaleTxnID:   claim.ProviderRequestID,
		AmountPaise: payment.Amount,
		Description: payment.BillNumber,
	})
	if err != nil {
		if markErr := s.repo.MarkSaleRequestUnresolved(ctx, payment.ID, s.now()); markErr != nil {
			s.logger.Error(
				"failed to mark unresolved HDFC sale request",
				"tenant_id", tenantID,
				"store_id", storeID,
				"bill_id", billID,
				"payment_id", paymentID,
				"provider_request_id", claim.ProviderRequestID,
				"error", markErr,
			)
		}
		s.logger.Error(
			"HDFC sale initiation failed",
			"tenant_id", tenantID,
			"store_id", storeID,
			"bill_id", billID,
			"payment_id", paymentID,
			"provider_request_id", claim.ProviderRequestID,
			"error", err,
		)
		return err
	}

	status, failedToStart := mapHDFCSaleInitiationStatus(response)
	updatedAt := s.now()

	updateInput := SaleUpdateInput{
		PaymentID:             payment.ID,
		BillID:                payment.BillID,
		Status:                status,
		ProviderRequestID:     nonEmpty(response.SaleTxnID, claim.ProviderRequestID),
		ProviderTxnID:         response.BHTxnID,
		TerminalTID:           claim.TerminalTID,
		ProviderStatusCode:    response.StatusCode,
		ProviderStatusMessage: response.StatusMessage,
		ProviderTxnStatus:     response.TxnStatus,
		ProviderTxnMessage:    response.TxnMessage,
		ResponsePayload:       response.RawPayload,
		UpdatedAt:             updatedAt,
	}

	if failedToStart {
		if err := s.repo.UpdatePaymentAfterSaleAndRecomputeBill(ctx, updateInput); err != nil {
			return err
		}
	} else if err := s.repo.UpdatePaymentAfterSale(ctx, updateInput); err != nil {
		return err
	}

	s.recordPaymentAudit(ctx, tenantID, storeID, payment.ID, performedByUserID, status, response)

	s.logger.Info(
		"HDFC sale initiated",
		"tenant_id", tenantID,
		"store_id", storeID,
		"bill_id", billID,
		"payment_id", payment.ID,
		"provider_request_id", nonEmpty(response.SaleTxnID, claim.ProviderRequestID),
		"provider_txn_id", response.BHTxnID,
		"payment_status", status,
	)

	return nil
}

func (s *Service) recordPaymentAudit(
	ctx context.Context,
	tenantID string,
	storeID string,
	paymentID string,
	performedByUserID string,
	status string,
	response hdfc.TransactionResponse,
) {
	if s.audit == nil {
		return
	}

	action := "PAYMENT_PENDING"
	switch enums.PaymentStatus(status) {
	case enums.PaymentStatusSuccess:
		action = "PAYMENT_SUCCESS"
	case enums.PaymentStatusFailed:
		action = "PAYMENT_FAILED"
	case enums.PaymentStatusCancelled:
		action = "PAYMENT_CANCELLED"
	}

	if err := s.audit.RecordPaymentEvent(
		ctx,
		tenantID,
		storeID,
		paymentID,
		performedByUserID,
		action,
		map[string]any{
			"gateway":                 GatewayHDFC,
			"provider_request_id":     response.SaleTxnID,
			"provider_txn_id":         response.BHTxnID,
			"provider_status_code":    response.StatusCode,
			"provider_status_message": response.StatusMessage,
			"provider_txn_status":     response.TxnStatus,
			"provider_txn_message":    response.TxnMessage,
		},
	); err != nil {
		s.logger.Error("payment audit write failed", "payment_id", paymentID, "error", err)
	}
}

func newSaleTxnID(paymentID string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(paymentID), "-", "")
	if normalized != "" {
		return providerRequestIDPrefix + normalized
	}

	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%s%d", providerRequestIDPrefix, time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%s%x", providerRequestIDPrefix, buffer)
}

func nonEmpty(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
