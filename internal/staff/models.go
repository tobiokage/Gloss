package staff

import "time"

type Staff struct {
	ID        string
	Name      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type StaffStoreMapping struct {
	ID        string
	StaffID   string
	StoreID   string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateStaffInput struct {
	TenantID string
	Name     string
}

type CreateStaffStoreMappingInput struct {
	StaffID string
	StoreID string
}
