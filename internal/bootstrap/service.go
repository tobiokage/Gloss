package bootstrap

import (
	"context"

	apperrors "gloss/internal/shared/errors"
)

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetStoreBootstrap(ctx context.Context, tenantID string, storeID string) (BootstrapResponse, error) {
	if tenantID == "" || storeID == "" {
		return BootstrapResponse{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}

	store, err := s.repo.GetActiveStore(ctx, tenantID, storeID)
	if err != nil {
		return BootstrapResponse{}, err
	}

	catalogueItems, err := s.repo.GetActiveCatalogueItems(ctx, tenantID)
	if err != nil {
		return BootstrapResponse{}, err
	}

	staffList, err := s.repo.GetActiveStoreStaff(ctx, tenantID, storeID)
	if err != nil {
		return BootstrapResponse{}, err
	}

	return BootstrapResponse{
		Store:          store,
		CatalogueItems: catalogueItems,
		Staff:          staffList,
	}, nil
}
