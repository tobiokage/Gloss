package bootstrap

type StoreDTO struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	Name            string `json:"name"`
	Code            string `json:"code"`
	Location        string `json:"location"`
	HDFCTerminalTID string `json:"-"`
}

type CatalogueItemDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	ListPrice int64  `json:"list_price"`
}

type StaffDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BootstrapResponse struct {
	Store               StoreDTO               `json:"store"`
	CatalogueItems      []CatalogueItemDTO     `json:"catalogue_items"`
	Staff               []StaffDTO             `json:"staff"`
	PaymentCapabilities PaymentCapabilitiesDTO `json:"payment_capabilities"`
}

type PaymentCapabilitiesDTO struct {
	HDFCOnlineEnabled     bool     `json:"hdfc_online_enabled"`
	HasHDFCTerminalConfig bool     `json:"has_hdfc_terminal_config"`
	AvailablePaymentModes []string `json:"available_payment_modes"`
	TerminalTIDIndicator  *string  `json:"terminal_tid_indicator,omitempty"`
}
