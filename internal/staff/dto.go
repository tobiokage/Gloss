package staff

import "time"

type CreateStaffRequest struct {
	Name string `json:"name"`
}

type StaffResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DeactivateStaffResponse struct {
	ID        string    `json:"id"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AssignStaffToStoreResponse struct {
	StaffID   string    `json:"staff_id"`
	StoreID   string    `json:"store_id"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}
