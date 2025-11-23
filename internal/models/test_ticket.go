package models

// TestTicketRequest represents the request payload for testing ticket templates
type TestTicketRequest struct {
	Email        string `json:"email" binding:"required,email" example:"admin@example.com"`
	EventID      string `json:"event_id" binding:"required" example:"550e8400-e29b-41d4-a716-446655440000"`
	TicketTierID string `json:"ticket_tier_id" binding:"required" example:"550e8400-e29b-41d4-a716-446655440001"`
}
