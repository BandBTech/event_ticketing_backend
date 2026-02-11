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
	if user == nil {
		return MinimalOrganizerResponse{
			ID:   uuid.Nil,
			Name: "Organizer Not Found",
			Logo: "",
		}
	}

	response := MinimalOrganizerResponse{
		ID: user.ID,
	}

	// Add business information if onboarding exists
	if user.OrganizerOnboarding != nil {
		response.Name = user.OrganizerOnboarding.BusinessName
		response.Logo = user.OrganizerOnboarding.BusinessLogoURL
	}

	// Fallback to first_name + last_name if business name is empty
	if response.Name == "" {
		response.Name = user.FirstName + " " + user.LastName
	}

	return response
}

// MinimalOrganizerResponse represents a minimal organizer response for admin endpoints
type MinimalOrganizerResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Logo string    `json:"logo"`
}
