package models

import "time"

// TestTicketRequest represents the request payload for testing ticket templates
type TestTicketRequest struct {
	Email        string `json:"email" binding:"required,email" example:"admin@example.com"`
	EventID      string `json:"event_id" binding:"required" example:"550e8400-e29b-41d4-a716-446655440000"`
	TicketTierID string `json:"ticket_tier_id" binding:"required" example:"550e8400-e29b-41d4-a716-446655440001"`
}

// TestTicketResponse represents the response for test ticket operations
type TestTicketResponse struct {
	Message  string    `json:"message" example:"Test ticket sent successfully"`
	TicketID string    `json:"ticket_id" example:"550e8400-e29b-41d4-a716-446655440002"`
	SentAt   time.Time `json:"sent_at" example:"2024-01-15T10:30:00Z"`
}
