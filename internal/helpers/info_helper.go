package helpers

import (
	"event-ticketing-backend/internal/models"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// OrganizerInfo holds resolved display info for an organizer.
type OrganizerInfo struct {
	ID              string
	BusinessName    string
	BusinessLogoURL string
}

// CompanyInfo holds resolved display info for the platform company.
type CompanyInfo struct {
	ID      string
	Name    string
	LogoURL string
	Email   string
}

// GetCompanyInfo loads the single platform CompanyInfo record.
// Returns an empty CompanyInfo (not an error) if no record exists yet.
func GetCompanyInfo(db *gorm.DB) (*CompanyInfo, error) {
	var company models.CompanyInfo
	if err := db.First(&company).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &CompanyInfo{}, nil
		}
		return nil, fmt.Errorf("failed to load company info: %w", err)
	}
	return &CompanyInfo{
		ID:      company.ID.String(),
		Name:    company.Name,
		LogoURL: company.LogoURL,
		Email:   company.Email,
	}, nil
}

// GetOrganizerInfo resolves an organizer's display name and logo by user ID.
//
// Resolution order:
//   - BusinessName:    OrganizerOnboarding.BusinessName → User.FirstName + " " + User.LastName
//   - BusinessLogoURL: OrganizerOnboarding.BusinessLogoURL (no fallback)
func GetOrganizerInfo(db *gorm.DB, organizerID string) (*OrganizerInfo, error) {
	if organizerID == "" {
		return &OrganizerInfo{}, nil
	}

	// 1. Load the user row
	var user models.User
	if err := db.Where("id = ?", organizerID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &OrganizerInfo{ID: organizerID}, nil
		}
		return nil, fmt.Errorf("failed to load user %s: %w", organizerID, err)
	}

	info := &OrganizerInfo{ID: organizerID}

	// 2. Try to load OrganizerOnboarding
	var onboarding models.OrganizerOnboarding
	err := db.Where("organizer_id = ?", organizerID).First(&onboarding).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to load organizer onboarding for %s: %w", organizerID, err)
	}

	// 3. Resolve business name
	if err == nil && strings.TrimSpace(onboarding.BusinessName) != "" {
		info.BusinessName = onboarding.BusinessName
	} else {
		// Fallback: first_name + last_name from users table
		full := strings.TrimSpace(user.FirstName + " " + user.LastName)
		info.BusinessName = full
	}

	// 4. Resolve logo (only from onboarding, no fallback)
	if err == nil {
		info.BusinessLogoURL = onboarding.BusinessLogoURL
	}

	return info, nil
}
