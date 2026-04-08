package catalogue

import "time"

type UpsertCatalogueItemRequest struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	ListPrice int64  `json:"list_price"`
}

type CatalogueItemResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	ListPrice int64     `json:"list_price"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DeactivateCatalogueItemResponse struct {
	ID        string    `json:"id"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}
