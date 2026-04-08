package catalogue

import "time"

type CatalogueItem struct {
	ID        string
	Name      string
	Category  string
	ListPrice int64
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateCatalogueItemInput struct {
	TenantID  string
	Name      string
	Category  string
	ListPrice int64
}

type UpdateCatalogueItemInput struct {
	ItemID    string
	TenantID  string
	Name      string
	Category  string
	ListPrice int64
}
