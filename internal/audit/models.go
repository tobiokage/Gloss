package audit

type RecordInput struct {
	TenantID          string
	StoreID           string
	EntityType        string
	EntityID          string
	Action            string
	PerformedByUserID string
	Metadata          map[string]any
}
