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
