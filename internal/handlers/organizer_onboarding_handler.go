package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OrganizerOnboardingHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewOrganizerOnboardingHandler(cfg *config.Config) *OrganizerOnboardingHandler {
	return &OrganizerOnboardingHandler{
		db:  database.GetDB(),
		cfg: cfg,
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
// @Accept json
// @Produce json
// @Param request body models.UpdateOrganizerProfileRequest true "Business profile data"
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

	var request models.UpdateOrganizerProfileRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
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
