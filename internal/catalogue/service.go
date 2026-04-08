package catalogue

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

func (s *Service) ListCatalogueItems(ctx context.Context, tenantID string) ([]CatalogueItem, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return nil, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	return s.repo.ListByTenant(ctx, normalizedTenantID)
}

func (s *Service) CreateCatalogueItem(ctx context.Context, tenantID string, req UpsertCatalogueItemRequest) (CatalogueItem, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return CatalogueItem{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	validatedInput, err := ValidateCatalogueItemInput(req.Name, req.Category, req.ListPrice)
	if err != nil {
		return CatalogueItem{}, err
	}

	return s.repo.Create(ctx, CreateCatalogueItemInput{
		TenantID:  normalizedTenantID,
		Name:      validatedInput.Name,
		Category:  validatedInput.Category,
		ListPrice: validatedInput.ListPrice,
	})
}

func (s *Service) UpdateCatalogueItem(
	ctx context.Context,
	tenantID string,
	itemID string,
	req UpsertCatalogueItemRequest,
) (CatalogueItem, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return CatalogueItem{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedItemID, err := ValidateItemID(itemID)
	if err != nil {
		return CatalogueItem{}, err
	}

	validatedInput, err := ValidateCatalogueItemInput(req.Name, req.Category, req.ListPrice)
	if err != nil {
		return CatalogueItem{}, err
	}

	if _, err := s.repo.GetByIDAndTenant(ctx, normalizedItemID, normalizedTenantID); err != nil {
		return CatalogueItem{}, err
	}

	return s.repo.Update(ctx, UpdateCatalogueItemInput{
		ItemID:    normalizedItemID,
		TenantID:  normalizedTenantID,
		Name:      validatedInput.Name,
		Category:  validatedInput.Category,
		ListPrice: validatedInput.ListPrice,
	})
}

func (s *Service) DeactivateCatalogueItem(ctx context.Context, tenantID string, itemID string) (CatalogueItem, error) {
	normalizedTenantID := strings.TrimSpace(tenantID)
	if normalizedTenantID == "" {
		return CatalogueItem{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	normalizedItemID, err := ValidateItemID(itemID)
	if err != nil {
		return CatalogueItem{}, err
	}

	item, err := s.repo.GetByIDAndTenant(ctx, normalizedItemID, normalizedTenantID)
	if err != nil {
		return CatalogueItem{}, err
	}

	if !item.Active {
		return item, nil
	}

	return s.repo.Deactivate(ctx, normalizedItemID, normalizedTenantID)
}
