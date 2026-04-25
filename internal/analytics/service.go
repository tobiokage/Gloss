package analytics

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gloss/internal/auth"
	"gloss/internal/shared/enums"
	apperrors "gloss/internal/shared/errors"
)

const (
	defaultAdminBillsLimit = 50
	maxAdminBillsLimit     = 200
)

var analyticsUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListAdminBills(ctx context.Context, authCtx auth.AuthContext, query AdminBillsQuery) ([]AdminBillResponse, error) {
	if err := requireSuperAdminScope(authCtx); err != nil {
		return nil, err
	}

	filters, err := validateAdminBillFilters(query)
	if err != nil {
		return nil, err
	}
	if err := s.validateStoreScope(ctx, authCtx.TenantID, filters.StoreID); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListAdminBills(ctx, authCtx.TenantID, filters)
	if err != nil {
		return nil, err
	}

	response := make([]AdminBillResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, AdminBillResponse{
			BillID:             row.BillID,
			BillNumber:         row.BillNumber,
			CreatedAt:          row.CreatedAt,
			StoreID:            row.StoreID,
			StoreName:          row.StoreName,
			Status:             row.Status,
			TotalAmount:        row.TotalAmount,
			AmountPaid:         row.AmountPaid,
			AmountDue:          row.AmountDue,
			PaymentModeSummary: row.PaymentModeSummary,
			CancellationReason: row.CancellationReason,
		})
	}
	return response, nil
}

func (s *Service) GetAdminAnalyticsSummary(
	ctx context.Context,
	authCtx auth.AuthContext,
	query AnalyticsSummaryQuery,
) (AnalyticsSummaryResponse, error) {
	if err := requireSuperAdminScope(authCtx); err != nil {
		return AnalyticsSummaryResponse{}, err
	}

	filters, err := validateAnalyticsSummaryFilters(query)
	if err != nil {
		return AnalyticsSummaryResponse{}, err
	}
	if err := s.validateStoreScope(ctx, authCtx.TenantID, filters.StoreID); err != nil {
		return AnalyticsSummaryResponse{}, err
	}

	summary, err := s.repo.GetAdminAnalyticsSummary(ctx, authCtx.TenantID, filters)
	if err != nil {
		return AnalyticsSummaryResponse{}, err
	}
	return AnalyticsSummaryResponse{
		TotalBills:         summary.TotalBills,
		TotalSales:         summary.TotalSales,
		CancelledBillCount: summary.CancelledBillCount,
		CancelledAmount:    summary.CancelledAmount,
		TotalTax:           summary.TotalTax,
		TotalCommission:    summary.TotalCommission,
		TotalTip:           summary.TotalTip,
	}, nil
}

func (s *Service) validateStoreScope(ctx context.Context, tenantID string, storeID string) error {
	if storeID == "" {
		return nil
	}

	exists, err := s.repo.StoreBelongsToTenant(ctx, tenantID, storeID)
	if err != nil {
		return err
	}
	if !exists {
		return apperrors.New(apperrors.CodeNotFound, "Store not found")
	}
	return nil
}

func requireSuperAdminScope(authCtx auth.AuthContext) error {
	if strings.TrimSpace(authCtx.TenantID) == "" {
		return apperrors.New(apperrors.CodeUnauthorized, "Invalid auth scope")
	}
	return auth.RequireRole(authCtx, enums.RoleSuperAdmin)
}

func validateAdminBillFilters(query AdminBillsQuery) (adminBillFilters, error) {
	filters, err := validateSharedFilters(query.StoreID, query.DateFrom, query.DateTo, query.Status)
	if err != nil {
		return adminBillFilters{}, err
	}

	limit, err := parseLimit(query.Limit)
	if err != nil {
		return adminBillFilters{}, err
	}
	offset, err := parseOffset(query.Offset)
	if err != nil {
		return adminBillFilters{}, err
	}

	return adminBillFilters{
		StoreID:  filters.StoreID,
		DateFrom: filters.DateFrom,
		DateTo:   filters.DateTo,
		Status:   filters.Status,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func validateAnalyticsSummaryFilters(query AnalyticsSummaryQuery) (analyticsSummaryFilters, error) {
	filters, err := validateSharedFilters(query.StoreID, query.DateFrom, query.DateTo, query.Status)
	if err != nil {
		return analyticsSummaryFilters{}, err
	}
	return analyticsSummaryFilters(filters), nil
}

func validateSharedFilters(storeID string, dateFromRaw string, dateToRaw string, statusRaw string) (analyticsSummaryFilters, error) {
	normalizedStoreID := strings.TrimSpace(storeID)
	if normalizedStoreID != "" && !analyticsUUIDPattern.MatchString(normalizedStoreID) {
		return analyticsSummaryFilters{}, apperrors.New(apperrors.CodeInvalidRequest, "store_id must be a valid UUID")
	}

	dateFrom, err := parseOptionalDateTime("date_from", dateFromRaw, false)
	if err != nil {
		return analyticsSummaryFilters{}, err
	}
	dateTo, err := parseOptionalDateTime("date_to", dateToRaw, true)
	if err != nil {
		return analyticsSummaryFilters{}, err
	}
	if dateFrom != nil && dateTo != nil && !dateFrom.Before(*dateTo) {
		return analyticsSummaryFilters{}, apperrors.New(apperrors.CodeInvalidRequest, "date_from must be before date_to")
	}

	status, err := validateBillStatus(statusRaw)
	if err != nil {
		return analyticsSummaryFilters{}, err
	}

	return analyticsSummaryFilters{
		StoreID:  normalizedStoreID,
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Status:   status,
	}, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminBillsLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, apperrors.New(apperrors.CodeInvalidRequest, "limit must be a positive integer")
	}
	if limit > maxAdminBillsLimit {
		return 0, apperrors.New(apperrors.CodeInvalidRequest, "limit exceeds maximum allowed value")
	}
	return limit, nil
}

func parseOffset(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		return 0, apperrors.New(apperrors.CodeInvalidRequest, "offset must be a non-negative integer")
	}
	return offset, nil
}

func parseOptionalDateTime(fieldName string, raw string, dateOnlyEndExclusive bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, nil
	}

	parsedDate, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, fieldName+" must be YYYY-MM-DD or RFC3339")
	}
	if dateOnlyEndExclusive {
		parsedDate = parsedDate.AddDate(0, 0, 1)
	}
	return &parsedDate, nil
}

func validateBillStatus(raw string) (string, error) {
	status := strings.ToUpper(strings.TrimSpace(raw))
	if status == "" {
		return "", nil
	}

	switch enums.BillStatus(status) {
	case enums.BillStatusDraft,
		enums.BillStatusPaid,
		enums.BillStatusPaymentPending,
		enums.BillStatusPaymentFailed,
		enums.BillStatusPartiallyPaid,
		enums.BillStatusCancelled:
		return status, nil
	default:
		return "", apperrors.New(apperrors.CodeInvalidRequest, "status must be a valid bill status")
	}
}
