package staff

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

func (s *Service) ListStaff(ctx context.Context, tenantID string) ([]Staff, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return nil, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	return s.repo.ListByTenant(ctx, normalizedTenantID)
}

func (s *Service) CreateStaff(ctx context.Context, tenantID string, req CreateStaffRequest) (Staff, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return Staff{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedName, err := ValidateStaffName(req.Name)
	if err != nil {
		return Staff{}, err
	}

	return s.repo.Create(ctx, CreateStaffInput{
		TenantID: normalizedTenantID,
		Name:     normalizedName,
	})
}

func (s *Service) DeactivateStaff(ctx context.Context, tenantID string, staffID string) (Staff, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return Staff{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedStaffID, err := ValidateStaffID(staffID)
	if err != nil {
		return Staff{}, err
	}

	member, err := s.repo.GetByIDAndTenant(ctx, normalizedStaffID, normalizedTenantID)
	if err != nil {
		return Staff{}, err
	}

	if !member.Active {
		return member, nil
	}

	return s.repo.Deactivate(ctx, normalizedStaffID, normalizedTenantID)
}

func (s *Service) AssignStaffToStore(
	ctx context.Context,
	tenantID string,
	staffID string,
	storeID string,
) (StaffStoreMapping, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return StaffStoreMapping{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedStaffID, err := ValidateStaffID(staffID)
	if err != nil {
		return StaffStoreMapping{}, err
	}

	normalizedStoreID, err := ValidateStoreID(storeID)
	if err != nil {
		return StaffStoreMapping{}, err
	}

	member, err := s.repo.GetByIDAndTenant(ctx, normalizedStaffID, normalizedTenantID)
	if err != nil {
		return StaffStoreMapping{}, err
	}
	if !member.Active {
		return StaffStoreMapping{}, apperrors.New(apperrors.CodeInvalidRequest, "Inactive staff cannot be mapped")
	}

	storeExists, err := s.repo.StoreExistsForTenant(ctx, normalizedStoreID, normalizedTenantID)
	if err != nil {
		return StaffStoreMapping{}, err
	}
	if !storeExists {
		return StaffStoreMapping{}, apperrors.New(apperrors.CodeNotFound, "Store not found")
	}

	return s.repo.CreateMapping(ctx, CreateStaffStoreMappingInput{
		StaffID: normalizedStaffID,
		StoreID: normalizedStoreID,
	})
}

func (s *Service) ValidateActiveStaffStoreMapping(
	ctx context.Context,
	tenantID string,
	storeID string,
	staffID string,
) error {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedStoreID, err := ValidateStoreID(storeID)
	if err != nil {
		return err
	}

	normalizedStaffID, err := ValidateStaffID(staffID)
	if err != nil {
		return err
	}

	isValid, err := s.repo.IsActiveMappedStaff(ctx, normalizedTenantID, normalizedStoreID, normalizedStaffID)
	if err != nil {
		return err
	}
	if !isValid {
		return apperrors.New(apperrors.CodeInvalidRequest, "Staff is not active for this store")
	}

	return nil
}
