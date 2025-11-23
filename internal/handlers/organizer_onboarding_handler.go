package handlers

import (
	"fmt"
	"net/http"
	"strconv"
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
// @Param business_website_url formData string false "Business website URL"
// @Param business_email formData string true "Business email"
// @Param business_phone formData string false "Business phone"
// @Param business_address formData string false "Business address"
// @Param years_of_experience formData int false "Years of experience"
// @Param specialties formData string false "Specialties (comma-separated)"
// @Param services_offered formData string false "Services offered (comma-separated)"
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

	// Parse multipart form
	_, err := c.MultipartForm()
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to parse multipart form", err)
		return
	}

	// Extract form data
	var request models.UpdateOrganizerProfileRequest
	request.BusinessName = c.PostForm("business_name")
	request.BusinessDescription = c.PostForm("business_description")
	request.BusinessWebsiteURL = c.PostForm("business_website_url")
	request.BusinessEmail = c.PostForm("business_email")
	request.BusinessPhone = c.PostForm("business_phone")
	request.BusinessAddress = c.PostForm("business_address")

	// Parse years of experience
	if yearsStr := c.PostForm("years_of_experience"); yearsStr != "" {
		if years, err := strconv.Atoi(yearsStr); err == nil {
			request.YearsOfExperience = years
		}
	}

	// Parse specialties
	if specialtiesStr := c.PostForm("specialties"); specialtiesStr != "" {
		request.Specialties = strings.Split(specialtiesStr, ",")
		// Trim spaces
		for i, specialty := range request.Specialties {
			request.Specialties[i] = strings.TrimSpace(specialty)
		}
	}

	// Parse services offered
	if servicesStr := c.PostForm("services_offered"); servicesStr != "" {
		request.ServicesOffered = strings.Split(servicesStr, ",")
		// Trim spaces
		for i, service := range request.ServicesOffered {
			request.ServicesOffered[i] = strings.TrimSpace(service)
		}
	}

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
			utils.DatabaseErrorResponse(c, "Failed to get onboarding record", err)
			return
		}
	}

	// Update business information
	onboarding.BusinessName = request.BusinessName
	onboarding.BusinessDescription = request.BusinessDescription
	onboarding.BusinessLogoURL = request.BusinessLogoURL
	onboarding.BusinessWebsiteURL = request.BusinessWebsiteURL
	onboarding.BusinessEmail = request.BusinessEmail
	onboarding.BusinessPhone = request.BusinessPhone
	onboarding.BusinessAddress = request.BusinessAddress
	onboarding.YearsOfExperience = request.YearsOfExperience
	onboarding.Specialties = request.Specialties
	onboarding.ServicesOffered = request.ServicesOffered
	onboarding.IsBusinessInfoComplete = true

	// Check if profile is complete (business info + categories)
	if onboarding.IsBusinessInfoComplete && onboarding.IsCategoriesSelected {
		onboarding.IsProfileComplete = true
		onboarding.IsOnboardingComplete = true
	}

	if err := h.db.Save(&onboarding).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to update profile", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Profile updated successfully", onboarding)
}

// @Summary Select organizer categories
// @Description Select business categories for organizer onboarding
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.SelectOrganizerCategoriesRequest true "Categories selection"
// @Success 200 {object} utils.Response{data=models.OrganizerOnboarding} "Categories updated successfully"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/categories [put]
func (h *OrganizerOnboardingHandler) SelectCategories(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	organizerID := userID.(uuid.UUID)

	var request models.SelectOrganizerCategoriesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Validate that all provided categories exist
	var categoryCount int64
	if err := h.db.Model(&models.Category{}).Where("name IN ? AND is_active = ?", request.Categories, true).Count(&categoryCount).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to validate categories", err)
		return
	}

	if categoryCount != int64(len(request.Categories)) {
		utils.BadRequestErrorResponse(c, "One or more categories are invalid or inactive", nil)
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
			utils.DatabaseErrorResponse(c, "Failed to get onboarding record", err)
			return
		}
	}

	// Update categories
	onboarding.BusinessCategories = request.Categories
	onboarding.IsCategoriesSelected = true

	// Check if profile is complete (business info + categories)
	if onboarding.IsBusinessInfoComplete && onboarding.IsCategoriesSelected {
		onboarding.IsProfileComplete = true
		onboarding.IsOnboardingComplete = true
	}

	if err := h.db.Save(&onboarding).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to update categories", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Categories updated successfully", onboarding)
}

// @Summary Complete onboarding
// @Description Mark the organizer onboarding as complete
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=models.OrganizerOnboarding} "Onboarding completed successfully"
// @Failure 400 {object} utils.Response "Onboarding requirements not met"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/complete [post]
func (h *OrganizerOnboardingHandler) CompleteOnboarding(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	organizerID := userID.(uuid.UUID)

	var onboarding models.OrganizerOnboarding
	if err := h.db.Where("organizer_id = ?", organizerID).First(&onboarding).Error; err != nil {
		utils.NotFoundErrorResponse(c, "Onboarding record not found", err)
		return
	}

	// Check if all requirements are met
	if !onboarding.IsBusinessInfoComplete || !onboarding.IsCategoriesSelected {
		utils.BadRequestErrorResponse(c, "Please complete all onboarding steps before finishing", nil)
		return
	}

	// Mark onboarding as complete
	onboarding.IsProfileComplete = true
	onboarding.IsOnboardingComplete = true

	if err := h.db.Save(&onboarding).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to complete onboarding", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Onboarding completed successfully", onboarding)
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
