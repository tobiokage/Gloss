package bootstrap

import (
	"context"
	"strings"

	apperrors "gloss/internal/shared/errors"
)

type Service struct {
	repo              *Repo
	hdfcOnlineEnabled bool
}

func NewService(repo *Repo, hdfcOnlineEnabled bool) *Service {
	return &Service{repo: repo, hdfcOnlineEnabled: hdfcOnlineEnabled}
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
		Store:               store,
		CatalogueItems:      catalogueItems,
		Staff:               staffList,
		PaymentCapabilities: buildPaymentCapabilities(store.HDFCTerminalTID, s.hdfcOnlineEnabled),
	}, nil
}

func buildPaymentCapabilities(tid string, hdfcOnlineEnabled bool) PaymentCapabilitiesDTO {
	tid = strings.TrimSpace(tid)
	hasTerminalConfig := len(tid) == 8
	modes := []string{"CASH"}
	if hdfcOnlineEnabled && hasTerminalConfig {
		modes = append(modes, "ONLINE", "SPLIT")
	}

	var indicator *string
	if hasTerminalConfig {
		masked := "****" + tid[len(tid)-4:]
		indicator = &masked
	}

	return PaymentCapabilitiesDTO{
		HDFCOnlineEnabled:     hdfcOnlineEnabled,
		HasHDFCTerminalConfig: hasTerminalConfig,
		AvailablePaymentModes: modes,
		TerminalTIDIndicator:  indicator,
	}
}
