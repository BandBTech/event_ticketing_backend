package utils

import (
	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
)

// CreateOrganizerPublicResponse creates a standardized OrganizerPublicResponse from a User model
// This centralizes the logic for creating organizer responses with business information
func CreateOrganizerPublicResponse(user *models.User) *models.OrganizerPublicResponse {
	if user == nil {
		return nil
	}

	response := &models.OrganizerPublicResponse{
		ID: user.ID,
	}

	// Add business information if onboarding exists
	if user.OrganizerOnboarding != nil {
		response.BusinessName = user.OrganizerOnboarding.BusinessName
		response.BusinessLogoURL = user.OrganizerOnboarding.BusinessLogoURL
	}

	return response
}

// CreateMinimalOrganizerResponse creates a standardized MinimalOrganizerResponse from a User model
// This centralizes the logic for creating minimal organizer responses with business information
func CreateMinimalOrganizerResponse(user *models.User) MinimalOrganizerResponse {
	response := MinimalOrganizerResponse{
		ID: user.ID,
	}

	// Add business information if onboarding exists
	if user.OrganizerOnboarding != nil {
		response.BusinessName = user.OrganizerOnboarding.BusinessName
		response.BusinessLogoURL = user.OrganizerOnboarding.BusinessLogoURL
	}

	return response
}

// MinimalOrganizerResponse represents a minimal organizer response for admin endpoints
type MinimalOrganizerResponse struct {
	ID              uuid.UUID `json:"id"`
	BusinessName    string    `json:"business_name"`
	BusinessLogoURL string    `json:"business_logo_url"`
}
