package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OrganizerOnboardingHandler struct {
	db                 *gorm.DB
	cfg                *config.Config
	fileStorageService *services.FileStorageService
}

func NewOrganizerOnboardingHandler(cfg *config.Config, fileStorageService *services.FileStorageService) *OrganizerOnboardingHandler {
	return &OrganizerOnboardingHandler{
		db:                 database.GetDB(),
		cfg:                cfg,
		fileStorageService: fileStorageService,
	}
}

// @Summary Get organizer onboarding status
// @Description Get the current onboarding status for the authenticated organizer
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=models.OrganizerOnboardingStatusResponse} "Onboarding status"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/status [get]
func (h *OrganizerOnboardingHandler) GetOnboardingStatus(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	organizerID := userID.(uuid.UUID)

	var onboarding models.OrganizerOnboarding
	if err := h.db.Where("organizer_id = ?", organizerID).First(&onboarding).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new onboarding record
			onboarding = models.OrganizerOnboarding{
				OrganizerID: organizerID,
			}
			if err := h.db.Create(&onboarding).Error; err != nil {
				utils.DatabaseErrorResponse(c, "Failed to create onboarding record", err)
				return
			}
		} else {
			utils.DatabaseErrorResponse(c, "Failed to get onboarding status", err)
			return
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Onboarding status retrieved successfully", onboarding.GetStatusResponse())
}

// @Summary Update event categories
// @Description Update preferred event categories during onboarding
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param business_name formData string true "Business name"
// @Param business_description formData string false "Business description"
// @Param business_logo formData file false "Business logo image"
// @Success 200 {object} utils.Response{data=models.OrganizerOnboarding} "Profile updated successfully"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/profile [put]
func (h *OrganizerOnboardingHandler) UpdateProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	organizerID := userID.(uuid.UUID)

	// Verify organizer exists
	var organizer models.User
	if err := h.db.Where("id = ? AND deleted_at IS NULL", organizerID).First(&organizer).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Organizer not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to verify organizer", err)
		return
	}

	// Parse multipart form
	_, err := c.MultipartForm()
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to parse multipart form", err)
		return
	}

	// Extract form data - only business name, description, and logo
	var request models.UpdateOrganizerProfileRequest
	request.BusinessName = c.PostForm("business_name")
	request.BusinessDescription = c.PostForm("business_description")

	// Handle business logo upload
	if logoFile, header, err := c.Request.FormFile("business_logo"); err == nil {
		defer logoFile.Close()

		// Validate file before upload
		if header.Size == 0 {
			utils.BadRequestErrorResponse(c, "Business logo file is empty", nil)
			return
		}

		// Check file size (2MB limit)
		if header.Size > 2*1024*1024 {
			utils.BadRequestErrorResponse(c, "Business logo file size must be less than 2MB", nil)
			return
		}

		// Check file type
		contentType := header.Header.Get("Content-Type")
		allowedTypes := []string{"image/jpeg", "image/jpg", "image/png", "image/webp"}
		isValidType := false
		for _, t := range allowedTypes {
			if contentType == t {
				isValidType = true
				break
			}
		}
		if !isValidType {
			utils.BadRequestErrorResponse(c, "Business logo must be a JPEG, PNG, or WebP image", nil)
			return
		}

		// Upload business logo
		logoURL, err := h.fileStorageService.UploadFile(logoFile, header, models.FileCategoryOrganizerLogo, organizerID, &services.FileUploadOptions{
			AltText:     fmt.Sprintf("Business logo for %s", request.BusinessName),
			Description: fmt.Sprintf("Business logo for organizer: %s", request.BusinessName),
		})
		if err != nil {
			// Log the detailed error for debugging
			fmt.Printf("S3 upload error: %v\n", err)

			// Check for specific error types
			if strings.Contains(err.Error(), "NoCredentialsProvided") {
				utils.InternalServerErrorResponse(c, "S3 credentials not configured", nil)
				return
			}
			if strings.Contains(err.Error(), "NoSuchBucket") {
				utils.InternalServerErrorResponse(c, "S3 bucket not found", nil)
				return
			}
			if strings.Contains(err.Error(), "AccessDenied") {
				utils.InternalServerErrorResponse(c, "S3 access denied", nil)
				return
			}
			if strings.Contains(err.Error(), "InvalidAccessKeyId") {
				utils.InternalServerErrorResponse(c, "Invalid S3 access key", nil)
				return
			}
			if strings.Contains(err.Error(), "image dimensions") {
				utils.BadRequestErrorResponse(c, "Business logo image dimensions must be between 100x100 and 500x500 pixels", err)
				return
			}
			if strings.Contains(err.Error(), "file type") {
				utils.BadRequestErrorResponse(c, err.Error(), nil)
				return
			}
			if strings.Contains(err.Error(), "file size") {
				utils.BadRequestErrorResponse(c, err.Error(), nil)
				return
			}

			// Generic error
			utils.InternalServerErrorResponse(c, fmt.Sprintf("Failed to upload business logo: %s", err.Error()), err)
			return
		}
		request.BusinessLogoURL = logoURL
	} else if err != http.ErrMissingFile {
		// Only return error if it's not a "missing file" error
		utils.BadRequestErrorResponse(c, "Invalid business logo file", err)
		return
	}

	var onboarding models.OrganizerOnboarding
	if err := h.db.Where("organizer_id = ?", organizerID).First(&onboarding).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new onboarding record
			onboarding = models.OrganizerOnboarding{
				OrganizerID: organizerID,
			}
		} else {
			// Log the detailed error for debugging
			fmt.Printf("Database error getting onboarding record: %v\n", err)
			utils.DatabaseErrorResponse(c, "Failed to get onboarding record", err)
			return
		}
	}

	// Start database transaction
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Update business information - only name, description, and logo
	onboarding.BusinessName = request.BusinessName
	onboarding.BusinessDescription = request.BusinessDescription
	if request.BusinessLogoURL != "" {
		onboarding.BusinessLogoURL = request.BusinessLogoURL
	}

	// Mark onboarding as complete if we have all required fields
	onboarding.IsComplete = onboarding.BusinessName != "" && onboarding.BusinessLogoURL != "" && onboarding.BusinessDescription != ""

	// Use Create or Save based on whether record exists
	var dbErr error
	if onboarding.ID == uuid.Nil {
		// New record - use Create
		dbErr = tx.Create(&onboarding).Error
	} else {
		// Existing record - use Save
		dbErr = tx.Save(&onboarding).Error
	}

	if dbErr != nil {
		tx.Rollback()
		// Log the detailed error for debugging
		fmt.Printf("Database error saving onboarding record: %v\n", dbErr)

		// Check for specific database errors
		if strings.Contains(dbErr.Error(), "duplicate key") {
			utils.ConflictErrorResponse(c, "Organizer onboarding record already exists", dbErr)
			return
		}
		if strings.Contains(dbErr.Error(), "violates foreign key constraint") {
			utils.BadRequestErrorResponse(c, "Invalid organizer ID", dbErr)
			return
		}
		if strings.Contains(dbErr.Error(), "value too long") {
			utils.BadRequestErrorResponse(c, "One or more fields exceed maximum length", dbErr)
			return
		}
		if strings.Contains(dbErr.Error(), "connection") {
			utils.InternalServerErrorResponse(c, "Database connection error", dbErr)
			return
		}

		// Generic database error
		utils.DatabaseErrorResponse(c, "Failed to update profile", dbErr)
		return
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.DatabaseErrorResponse(c, "Failed to commit profile update", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Profile updated successfully", nil)
}

// @Summary Get organizer profile
// @Description Get the complete organizer profile information
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=models.OrganizerOnboarding} "Organizer profile"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/profile [get]
func (h *OrganizerOnboardingHandler) GetProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	organizerID := userID.(uuid.UUID)

	var onboarding models.OrganizerOnboarding
	if err := h.db.Where("organizer_id = ?", organizerID).First(&onboarding).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new onboarding record
			onboarding = models.OrganizerOnboarding{
				OrganizerID: organizerID,
			}
			if err := h.db.Create(&onboarding).Error; err != nil {
				utils.DatabaseErrorResponse(c, "Failed to create onboarding record", err)
				return
			}
		} else {
			utils.DatabaseErrorResponse(c, "Failed to get organizer profile", err)
			return
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer profile retrieved successfully", onboarding)
}
