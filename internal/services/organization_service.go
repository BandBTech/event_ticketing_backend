package services

import (
	"errors"
	"fmt"
	"strings"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrganizationService provides methods for managing organizations
type OrganizationService struct {
	db           *gorm.DB
	emailService *EmailService
}

// NewOrganizationService creates a new organization service
func NewOrganizationService(emailService *EmailService) *OrganizationService {
	return &OrganizationService{
		db:           database.DB,
		emailService: emailService,
	}
}

// CreateOrganization creates a new organization with the given user as organizer
func (s *OrganizationService) CreateOrganization(organizerID uuid.UUID, req *models.CreateOrganizationRequest) (*models.OrganizationResponse, error) {
	// Verify the user exists
	var organizer models.User
	if err := s.db.First(&organizer, "id = ?", organizerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Organizer not found")
		}
		return nil, err
	}

	// Check if user already has an organizer role
	var organizerRole models.Role
	if err := s.db.Where("name = ?", "organizer").First(&organizerRole).Error; err != nil {
		return nil, fmt.Errorf("organizer role not found: %w", err)
	}

	// Create the organization
	org := models.Organization{
		Name:        req.Name,
		Description: req.Description,
		WebsiteURL:  req.WebsiteURL,
		LogoURL:     req.LogoURL,
		OrganizerID: organizerID,
	}

	// Start a transaction
	tx := s.db.Begin()

	// Create organization
	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Add organizer role to the user if they don't have it already
	var hasOrganizerRole bool
	if err := tx.Model(&organizer).Association("Roles").Find(&organizerRole); err == nil {
		hasOrganizerRole = true
	}

	if !hasOrganizerRole {
		if err := tx.Model(&organizer).Association("Roles").Append(&organizerRole); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	resp := org.ToResponse()
	return &resp, nil
}

// CreateOrgUser creates a new user under an organization
func (s *OrganizationService) CreateOrgUser(organizerID uuid.UUID, orgID uuid.UUID, req *models.CreateOrgUserRequest) (*models.UserResponse, error) {
	// Check if the organization exists and the organizer is authorized
	var org models.Organization
	if err := s.db.First(&org, "id = ? AND organizer_id = ?", orgID, organizerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Organization not found or you are not authorized to manage this organization")
		}
		return nil, err
	}

	// Check if user with the email already exists
	var existingUser models.User
	if err := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&existingUser).Error; err == nil {
		return nil, errors.New("User with this email already exists")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Get the role
	var role models.Role
	if err := s.db.Where("name = ?", req.RoleName).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("role '%s' not found", req.RoleName)
		}
		return nil, err
	}

	// Store original plain password to send in email
	plainPassword := req.Password

	// Create user
	user := models.User{
		Email:           strings.ToLower(req.Email),
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		OrganizationID:  &orgID,
		CreatedBy:       &organizerID,
		IsEmailVerified: true, // Auto-verify users created by organizers
	}

	// Hash password
	if err := user.HashPassword(req.Password); err != nil {
		return nil, err
	}

	// Start transaction
	tx := s.db.Begin()

	// Create user
	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Assign role
	if err := tx.Model(&user).Association("Roles").Append(&role); err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Load relations for response
	if err := s.db.Preload("Roles").Preload("Organization").First(&user, user.ID).Error; err != nil {
		return nil, err
	}

	// Send welcome email with credentials if email service is available
	if s.emailService != nil {
		if err := s.emailService.SendWelcomeEmailWithCredentials(&user, plainPassword, org.Name); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Failed to send welcome email: %v\n", err)
		}
	}

	resp := user.ToResponse()
	return &resp, nil
}

// GetOrganizationByID retrieves an organization by its ID
func (s *OrganizationService) GetOrganizationByID(orgID uuid.UUID) (*models.OrganizationResponse, error) {
	var org models.Organization
	if err := s.db.First(&org, "id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Organization not found")
		}
		return nil, err
	}

	// Load organizer
	if err := s.db.Model(&org).Association("Organizer").Find(&org.Organizer); err != nil {
		return nil, err
	}

	resp := org.ToResponse()
	return &resp, nil
}

// GetUserOrganizations gets all organizations for a user
func (s *OrganizationService) GetUserOrganizations(userID uuid.UUID) ([]models.OrganizationResponse, error) {
	var organizations []models.Organization

	// If user is an organizer, get organizations they created
	if err := s.db.Where("organizer_id = ?", userID).Find(&organizations).Error; err != nil {
		return nil, err
	}

	// If user is a member, get organizations they belong to
	var user models.User
	if err := s.db.Preload("Organization").First(&user, "id = ?", userID).Error; err == nil && user.Organization != nil {
		// Check if this organization is already in the list
		found := false
		for _, org := range organizations {
			if org.ID == user.Organization.ID {
				found = true
				break
			}
		}
		if !found {
			organizations = append(organizations, *user.Organization)
		}
	}

	// Convert to response objects
	responses := make([]models.OrganizationResponse, len(organizations))
	for i, org := range organizations {
		responses[i] = org.ToResponse()
	}

	return responses, nil
}

// GetOrganizationUsers gets all users in an organization
func (s *OrganizationService) GetOrganizationUsers(orgID uuid.UUID) ([]models.UserResponse, error) {
	var users []models.User
	if err := s.db.Where("organization_id = ?", orgID).Preload("Roles").Find(&users).Error; err != nil {
		return nil, err
	}

	responses := make([]models.UserResponse, len(users))
	for i, user := range users {
		resp := user.ToResponse()
		responses[i] = resp
	}

	return responses, nil
}

// UpdateOrganizationUser updates a user's role within an organization
func (s *OrganizationService) UpdateOrganizationUser(orgID uuid.UUID, userID uuid.UUID, req *models.UpdateOrgUserRequest) (*models.UserResponse, error) {
	// Check if the user exists in the organization
	var user models.User
	if err := s.db.Where("id = ? AND organization_id = ?", userID, orgID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("User not found in this organization")
		}
		return nil, err
	}

	// Update role if specified
	if req.RoleType != "" {
		// Find the role
		var role models.Role
		if err := s.db.Where("name = ?", req.RoleType).First(&role).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("role '%s' not found", req.RoleType)
			}
			return nil, err
		}

		// Start transaction for role update
		tx := s.db.Begin()

		// Remove existing roles
		if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
			tx.Rollback()
			return nil, err
		}

		// Assign new role
		if err := tx.Model(&user).Association("Roles").Append(&role); err != nil {
			tx.Rollback()
			return nil, err
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return nil, err
		}
	}

	// Update active status if provided
	if req.Active != nil {
		if err := s.db.Model(&user).Update("is_active", *req.Active).Error; err != nil {
			return nil, err
		}
	}

	// Refresh user data
	if err := s.db.Preload("Roles").Preload("Organization").First(&user, user.ID).Error; err != nil {
		return nil, err
	}

	resp := user.ToResponse()
	return &resp, nil
}

// DeleteOrganizationUser removes a user from an organization
func (s *OrganizationService) DeleteOrganizationUser(orgID uuid.UUID, userID uuid.UUID) error {
	// Check if the user exists in the organization
	result := s.db.Where("id = ? AND organization_id = ?", userID, orgID).Delete(&models.User{})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("User not found in this organization")
	}

	return nil
}

// UpdateOrganization updates an organization's details
func (s *OrganizationService) UpdateOrganization(orgID uuid.UUID, req *models.UpdateOrganizationRequest) (*models.OrganizationResponse, error) {
	// Find the organization
	var org models.Organization
	if err := s.db.First(&org, "id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Organization not found")
		}
		return nil, err
	}

	// Update fields
	if req.Name != "" {
		org.Name = req.Name
	}
	if req.Description != "" {
		org.Description = req.Description
	}
	if req.WebsiteURL != "" {
		org.WebsiteURL = req.WebsiteURL
	}
	if req.LogoURL != "" {
		org.LogoURL = req.LogoURL
	}

	// Save changes
	if err := s.db.Save(&org).Error; err != nil {
		return nil, err
	}

	// Load organizer for response
	if err := s.db.Model(&org).Association("Organizer").Find(&org.Organizer); err != nil {
		return nil, err
	}

	resp := org.ToResponse()
	return &resp, nil
}

// DeleteOrganization deletes an organization
func (s *OrganizationService) DeleteOrganization(orgID uuid.UUID) error {
	// Delete organization (this will use soft delete if configured)
	result := s.db.Delete(&models.Organization{}, "id = ?", orgID)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("Organization not found")
	}

	return nil
}

// UpdateOrgUserRole updates a user's role within an organization (deprecated, use UpdateOrganizationUser instead)
func (s *OrganizationService) UpdateOrgUserRole(organizerID uuid.UUID, orgID uuid.UUID, req *models.UpdateUserRoleRequest) error {
	// Parse user ID
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return errors.New("Invalid user ID")
	}

	// Check if the organization exists and the organizer is authorized
	var org models.Organization
	if err := s.db.First(&org, "id = ? AND organizer_id = ?", orgID, organizerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("Organization not found or you are not authorized to manage this organization")
		}
		return err
	}

	// Check if the user exists and belongs to the organization
	var user models.User
	if err := s.db.First(&user, "id = ? AND organization_id = ?", userID, orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("User not found in this organization")
		}
		return err
	}

	// Get the role
	var role models.Role
	if err := s.db.Where("name = ?", req.RoleName).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("role '%s' not found", req.RoleName)
		}
		return err
	}

	// Start transaction
	tx := s.db.Begin()

	// Remove existing roles
	if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
		tx.Rollback()
		return err
	}

	// Assign new role
	if err := tx.Model(&user).Association("Roles").Append(&role); err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
}

// GetOrganizationUsersForOrganizer gets all users in an organization for a specific organizer (deprecated)
func (s *OrganizationService) GetOrganizationUsersForOrganizer(organizerID uuid.UUID, orgID uuid.UUID) ([]models.UserResponse, error) {
	// Check if the organization exists and the organizer is authorized
	var org models.Organization
	if err := s.db.First(&org, "id = ? AND organizer_id = ?", orgID, organizerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Organization not found or you are not authorized to manage this organization")
		}
		return nil, err
	}

	// Get all users in the organization
	var users []models.User
	if err := s.db.Preload("Roles").Where("organization_id = ?", orgID).Find(&users).Error; err != nil {
		return nil, err
	}

	// Convert to response objects
	responses := make([]models.UserResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	return responses, nil
}
