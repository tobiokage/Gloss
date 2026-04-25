package audit

import (
	"context"
	"strings"

	apperrors "gloss/internal/shared/errors"
)

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) RecordBillCreated(
	ctx context.Context,
	tenantID string,
	storeID string,
	billID string,
	performedByUserID string,
	metadata map[string]any,
) error {
	tenantID = strings.TrimSpace(tenantID)
	storeID = strings.TrimSpace(storeID)
	billID = strings.TrimSpace(billID)
	performedByUserID = strings.TrimSpace(performedByUserID)

	if tenantID == "" || billID == "" {
		return apperrors.New(apperrors.CodeInternalError, "Invalid audit log input")
	}

	return s.repo.Insert(ctx, RecordInput{
		TenantID:          tenantID,
		StoreID:           storeID,
		EntityType:        "BILL",
		EntityID:          billID,
		Action:            "BILL_CREATED",
		PerformedByUserID: performedByUserID,
		Metadata:          metadata,
	})
}

func (s *Service) RecordBillCancelled(
	ctx context.Context,
	tenantID string,
	storeID string,
	billID string,
	performedByUserID string,
	metadata map[string]any,
) error {
	tenantID = strings.TrimSpace(tenantID)
	storeID = strings.TrimSpace(storeID)
	billID = strings.TrimSpace(billID)
	performedByUserID = strings.TrimSpace(performedByUserID)

	if tenantID == "" || billID == "" {
		return apperrors.New(apperrors.CodeInternalError, "Invalid audit log input")
	}

	return s.repo.Insert(ctx, RecordInput{
		TenantID:          tenantID,
		StoreID:           storeID,
		EntityType:        "BILL",
		EntityID:          billID,
		Action:            "BILL_CANCELLED",
		PerformedByUserID: performedByUserID,
		Metadata:          metadata,
	})
}

func (s *Service) RecordPaymentEvent(
	ctx context.Context,
	tenantID string,
	storeID string,
	paymentID string,
	performedByUserID string,
	action string,
	metadata map[string]any,
) error {
	tenantID = strings.TrimSpace(tenantID)
	storeID = strings.TrimSpace(storeID)
	paymentID = strings.TrimSpace(paymentID)
	performedByUserID = strings.TrimSpace(performedByUserID)
	action = strings.TrimSpace(action)

	if tenantID == "" || paymentID == "" || action == "" {
		return apperrors.New(apperrors.CodeInternalError, "Invalid audit log input")
	}

	return s.repo.Insert(ctx, RecordInput{
		TenantID:          tenantID,
		StoreID:           storeID,
		EntityType:        "PAYMENT",
		EntityID:          paymentID,
		Action:            action,
		PerformedByUserID: performedByUserID,
		Metadata:          metadata,
	})
}
