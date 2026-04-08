package billing

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gloss/internal/auth"
	platformdb "gloss/internal/platform/db"
	"gloss/internal/shared/enums"
	apperrors "gloss/internal/shared/errors"
	"gloss/internal/shared/idempotency"
)

type auditRecorder interface {
	RecordBillCreated(
		ctx context.Context,
		tenantID string,
		storeID string,
		billID string,
		performedByUserID string,
		metadata map[string]any,
	) error
}

type Service struct {
	db               *sql.DB
	repo             *Repo
	idempotencyStore *idempotency.Store
	auditRecorder    auditRecorder
	logger           *slog.Logger
}

func NewService(
	db *sql.DB,
	repo *Repo,
	idempotencyStore *idempotency.Store,
	auditRecorder auditRecorder,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		db:               db,
		repo:             repo,
		idempotencyStore: idempotencyStore,
		auditRecorder:    auditRecorder,
		logger:           logger,
	}
}

func (s *Service) CreateBill(ctx context.Context, authCtx auth.AuthContext, req CreateBillRequest) (CreateBillResponse, error) {
	scope, err := validateCreateBillScope(authCtx)
	if err != nil {
		return CreateBillResponse{}, err
	}

	clientBillRef := strings.TrimSpace(req.ClientBillRef)
	if clientBillRef == "" {
		return CreateBillResponse{}, apperrors.New(apperrors.CodeInvalidRequest, "client_bill_ref is required")
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		return CreateBillResponse{}, apperrors.New(apperrors.CodeInvalidRequest, "idempotency_key is required")
	}

	validatedRequest, err := ValidateCreateBillRequest(req)
	if err != nil {
		return CreateBillResponse{}, err
	}
	if validatedRequest.Payment.Mode != PaymentModeCash {
		return CreateBillResponse{}, apperrors.New(
			apperrors.CodeInvalidRequest,
			"payment.mode CASH is the only supported mode for create bill in this milestone",
		)
	}

	requestHash, err := idempotency.CanonicalRequestHash(struct {
		ClientBillRef  string                    `json:"client_bill_ref"`
		Items          []ValidatedCreateBillItem `json:"items"`
		DiscountAmount int64                     `json:"discount_amount"`
		TipAmount      int64                     `json:"tip_amount"`
		TipAllocations []TipAllocation           `json:"tip_allocations"`
		PaymentMode    string                    `json:"payment_mode"`
		CashAmount     int64                     `json:"cash_amount"`
	}{
		ClientBillRef:  clientBillRef,
		Items:          validatedRequest.Items,
		DiscountAmount: validatedRequest.DiscountAmount,
		TipAmount:      validatedRequest.TipAmount,
		TipAllocations: validatedRequest.TipAllocations,
		PaymentMode:    string(validatedRequest.Payment.Mode),
		CashAmount:     validatedRequest.Payment.CashAmount,
	})
	if err != nil {
		return CreateBillResponse{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to prepare create bill request",
			map[string]any{"reason": err.Error()},
		)
	}

	createInput := CreateBillInput{
		ClientBillRef:  clientBillRef,
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		UserID:         scope.UserID,
		TenantID:       scope.TenantID,
		StoreID:        scope.StoreID,
		Request:        validatedRequest,
	}

	operationTime := time.Now().UTC()
	var (
		billID     string
		billNumber string
		created    bool
	)

	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		claim, err := s.idempotencyStore.ClaimCreateBill(
			ctx,
			tx,
			createInput.TenantID,
			createInput.StoreID,
			createInput.IdempotencyKey,
			createInput.RequestHash,
		)
		if err != nil {
			return err
		}
		if claim.Completed {
			billID = claim.ResponseBillID
			return nil
		}

		store, err := s.repo.GetActiveStoreSnapshot(ctx, tx, createInput.TenantID, createInput.StoreID)
		if err != nil {
			return err
		}

		authoritativeItems, err := s.repo.GetActiveCatalogueLinesByIDs(
			ctx,
			tx,
			createInput.TenantID,
			collectCatalogueItemIDs(createInput.Request.Items),
		)
		if err != nil {
			return err
		}
		if err := validateAuthoritativeCatalogueItems(createInput.Request.Items, authoritativeItems); err != nil {
			return err
		}

		authoritativeStaff, err := s.repo.GetActiveStoreStaffByIDs(
			ctx,
			tx,
			createInput.TenantID,
			createInput.StoreID,
			collectStaffIDs(createInput.Request),
		)
		if err != nil {
			return err
		}
		if err := validateAuthoritativeStaffAssignments(createInput.Request, authoritativeStaff); err != nil {
			return err
		}

		sequence, err := s.repo.LockAndIncrementBillCounter(ctx, tx, createInput.StoreID, operationTime)
		if err != nil {
			return err
		}

		billNumber, err = FormatBillNumber(BillNumberInput{
			StoreCode: store.Code,
			Date:      operationTime,
			Sequence:  sequence,
		})
		if err != nil {
			return err
		}

		calculatorInput, err := BuildCalculatorInput(createInput.Request, authoritativeItems)
		if err != nil {
			return err
		}

		calculation, err := CalculateBill(calculatorInput)
		if err != nil {
			return err
		}

		billID, err = newServiceUUIDString()
		if err != nil {
			return apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create bill",
				map[string]any{"reason": err.Error()},
			)
		}

		paidAt := operationTime
		if err := s.repo.InsertBill(ctx, tx, InsertBillInput{
			ID:                 billID,
			TenantID:           createInput.TenantID,
			StoreID:            createInput.StoreID,
			BillNumber:         billNumber,
			Status:             string(calculation.Status),
			ServiceGrossAmount: calculation.Totals.ServiceGrossAmount,
			DiscountAmount:     calculation.Totals.DiscountAmount,
			ServiceNetAmount:   calculation.Totals.ServiceNetAmount,
			TipAmount:          calculation.Totals.TipAmount,
			TaxableBaseAmount:  calculation.Totals.TaxableBaseAmount,
			TaxAmount:          calculation.Totals.TaxAmount,
			TotalAmount:        calculation.Totals.TotalAmount,
			AmountPaid:         calculation.Totals.AmountPaid,
			AmountDue:          calculation.Totals.AmountDue,
			PaymentModeSummary: string(calculation.PaymentMode),
			CreatedByUserID:    createInput.UserID,
			CreatedAt:          operationTime,
			PaidAt:             &paidAt,
		}); err != nil {
			return err
		}

		persistedItems, err := s.repo.InsertBillItems(ctx, tx, billID, calculation.Lines, operationTime)
		if err != nil {
			return err
		}

		tipAllocationRows, err := buildTipAllocationRows(billID, calculation.TipAllocations, operationTime)
		if err != nil {
			return err
		}
		if err := s.repo.InsertBillTipAllocations(ctx, tx, tipAllocationRows); err != nil {
			return err
		}

		commissionRows, err := buildCommissionLedgerRows(billID, persistedItems, operationTime)
		if err != nil {
			return err
		}
		if err := s.repo.InsertCommissionLedgerRows(ctx, tx, commissionRows); err != nil {
			return err
		}

		paymentID, err := newServiceUUIDString()
		if err != nil {
			return apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create cash payment",
				map[string]any{"reason": err.Error()},
			)
		}

		if err := s.repo.InsertPayment(ctx, tx, InsertPaymentInput{
			ID:            paymentID,
			BillID:        billID,
			PaymentMethod: string(PaymentModeCash),
			Amount:        calculation.Totals.TotalAmount,
			Status:        string(enums.PaymentStatusSuccess),
			VerifiedAt:    &operationTime,
			CreatedAt:     operationTime,
			UpdatedAt:     operationTime,
		}); err != nil {
			return err
		}

		if err := s.idempotencyStore.CompleteCreateBill(
			ctx,
			tx,
			createInput.TenantID,
			createInput.StoreID,
			createInput.IdempotencyKey,
			billID,
		); err != nil {
			return err
		}

		created = true
		return nil
	})
	if err != nil {
		s.logger.Error(
			"create bill failed",
			"tenant_id", createInput.TenantID,
			"store_id", createInput.StoreID,
			"user_id", createInput.UserID,
			"client_bill_ref", createInput.ClientBillRef,
			"idempotency_key", createInput.IdempotencyKey,
			"error", err,
		)
		return CreateBillResponse{}, err
	}
	if strings.TrimSpace(billID) == "" {
		return CreateBillResponse{}, apperrors.New(apperrors.CodeInternalError, "Create bill did not resolve a bill reference")
	}

	graph, err := s.repo.GetBillGraph(ctx, billID, createInput.TenantID, createInput.StoreID)
	if err != nil {
		return CreateBillResponse{}, err
	}

	if created {
		if s.auditRecorder != nil {
			if err := s.auditRecorder.RecordBillCreated(
				ctx,
				createInput.TenantID,
				createInput.StoreID,
				billID,
				createInput.UserID,
				map[string]any{
					"bill_number":       billNumber,
					"client_bill_ref":   createInput.ClientBillRef,
					"idempotency_key":   createInput.IdempotencyKey,
					"payment_mode":      string(createInput.Request.Payment.Mode),
					"total_amount":      graph.Bill.TotalAmount,
					"amount_paid":       graph.Bill.AmountPaid,
					"amount_due":        graph.Bill.AmountDue,
					"service_net":       graph.Bill.ServiceNetAmount,
					"discount_amount":   graph.Bill.DiscountAmount,
					"tip_amount":        graph.Bill.TipAmount,
					"payment_row_count": len(graph.Payments),
				},
			); err != nil {
				s.logger.Error(
					"bill created but audit write failed",
					"bill_id", billID,
					"tenant_id", createInput.TenantID,
					"store_id", createInput.StoreID,
					"error", err,
				)
			}
		}

		s.logger.Info(
			"bill created",
			"bill_id", billID,
			"bill_number", billNumber,
			"tenant_id", createInput.TenantID,
			"store_id", createInput.StoreID,
			"user_id", createInput.UserID,
			"payment_mode", string(createInput.Request.Payment.Mode),
			"total_amount", graph.Bill.TotalAmount,
		)
	} else {
		s.logger.Info(
			"create bill idempotency replay",
			"bill_id", billID,
			"tenant_id", createInput.TenantID,
			"store_id", createInput.StoreID,
			"idempotency_key", createInput.IdempotencyKey,
		)
	}

	return MapBillGraphToCreateBillResponse(graph), nil
}

func validateCreateBillScope(authCtx auth.AuthContext) (auth.AuthContext, error) {
	if err := auth.RequireRole(authCtx, enums.RoleStoreManager); err != nil {
		return auth.AuthContext{}, err
	}

	authCtx.TenantID = strings.TrimSpace(authCtx.TenantID)
	authCtx.StoreID = strings.TrimSpace(authCtx.StoreID)
	authCtx.UserID = strings.TrimSpace(authCtx.UserID)
	if authCtx.TenantID == "" || authCtx.StoreID == "" || authCtx.UserID == "" {
		return auth.AuthContext{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	return authCtx, nil
}

func collectCatalogueItemIDs(items []ValidatedCreateBillItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CatalogueItemID)
	}
	return ids
}

func collectStaffIDs(req ValidatedCreateBillRequest) []string {
	ids := make([]string, 0, len(req.Items)+len(req.TipAllocations))
	for _, item := range req.Items {
		ids = append(ids, item.AssignedStaffID)
	}
	for _, allocation := range req.TipAllocations {
		ids = append(ids, allocation.StaffID)
	}
	return ids
}

func validateAuthoritativeCatalogueItems(
	items []ValidatedCreateBillItem,
	authoritative map[string]AuthoritativeCatalogueLine,
) error {
	for i, item := range items {
		line, ok := authoritative[item.CatalogueItemID]
		if !ok {
			return apperrors.New(
				apperrors.CodeInvalidRequest,
				"items["+itoa(i)+"].catalogue_item_id is not active for this tenant",
			)
		}
		if strings.TrimSpace(line.ServiceName) == "" || line.UnitPrice <= 0 {
			return apperrors.New(
				apperrors.CodeInternalError,
				"Authoritative catalogue data is incomplete",
			)
		}
	}

	return nil
}

func validateAuthoritativeStaffAssignments(
	req ValidatedCreateBillRequest,
	authoritative map[string]AuthoritativeStaffMember,
) error {
	for i, item := range req.Items {
		if _, ok := authoritative[item.AssignedStaffID]; !ok {
			return apperrors.New(
				apperrors.CodeInvalidRequest,
				"items["+itoa(i)+"].assigned_staff_id is not active for this store",
			)
		}
	}

	for i, allocation := range req.TipAllocations {
		if _, ok := authoritative[allocation.StaffID]; !ok {
			return apperrors.New(
				apperrors.CodeInvalidRequest,
				"tip_allocations["+itoa(i)+"].staff_id is not active for this store",
			)
		}
	}

	return nil
}

func buildTipAllocationRows(
	billID string,
	allocations []TipAllocation,
	createdAt time.Time,
) ([]InsertTipAllocationInput, error) {
	rows := make([]InsertTipAllocationInput, 0, len(allocations))
	for _, allocation := range allocations {
		allocationID, err := newServiceUUIDString()
		if err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create bill tip allocation",
				map[string]any{"reason": err.Error()},
			)
		}
		rows = append(rows, InsertTipAllocationInput{
			ID:        allocationID,
			BillID:    billID,
			StaffID:   allocation.StaffID,
			TipAmount: allocation.TipAmount,
			CreatedAt: createdAt,
		})
	}
	return rows, nil
}

func buildCommissionLedgerRows(
	billID string,
	items []PersistedBillItem,
	createdAt time.Time,
) ([]InsertCommissionLedgerInput, error) {
	rows := make([]InsertCommissionLedgerInput, 0, len(items))
	for _, item := range items {
		ledgerID, err := newServiceUUIDString()
		if err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create commission ledger row",
				map[string]any{"reason": err.Error()},
			)
		}
		rows = append(rows, InsertCommissionLedgerInput{
			ID:                   ledgerID,
			BillID:               billID,
			BillItemID:           item.ID,
			StaffID:              item.AssignedStaffID,
			BaseAmount:           item.CommissionBaseAmount,
			CommissionPercentBPS: int(commissionPercent * 100),
			CommissionAmount:     item.CommissionAmount,
			CreatedAt:            createdAt,
		})
	}
	return rows, nil
}

func newServiceUUIDString() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		buffer[0:4],
		buffer[4:6],
		buffer[6:8],
		buffer[8:10],
		buffer[10:16],
	), nil
}
