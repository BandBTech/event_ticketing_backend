package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AdminManagementHandler struct {
	db                             *gorm.DB
	fileStorageService             *services.FileStorageService
	universalTicketTemplateService *services.UniversalTicketTemplateService
	emailQueueService              *services.EmailQueueService
}

func NewAdminManagementHandler(fileStorageService *services.FileStorageService, universalTicketTemplateService *services.UniversalTicketTemplateService, emailQueueService *services.EmailQueueService) *AdminManagementHandler {
	return &AdminManagementHandler{
		db:                             database.GetDB(),
		fileStorageService:             fileStorageService,
		universalTicketTemplateService: universalTicketTemplateService,
		emailQueueService:              emailQueueService,
	}
}

// Company Information Management

// @Summary Get company information (Admin)
// @Description Get company information for admin management
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=models.CompanyInfoResponse} "Company information"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/company-info [get]
func (h *AdminManagementHandler) GetCompanyInfo(c *gin.Context) {
	var companyInfo models.CompanyInfo

	if err := h.db.First(&companyInfo).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create default company info if not exists
			companyInfo = models.CompanyInfo{
				Name:        "Timro Ticket",
				Description: "Event ticketing platform",
				Email:       "info@timroticket.com",
			}
			if err := h.db.Create(&companyInfo).Error; err != nil {
				utils.DatabaseErrorResponse(c, "Failed to create company info", err)
				return
			}
		} else {
			utils.DatabaseErrorResponse(c, "Failed to get company information", err)
			return
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Company information retrieved successfully", companyInfo.ToResponse())
}

// @Summary Update company information (Admin)
// @Description Update website company information
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param name formData string false "Company name"
// @Param description formData string false "Company description"
// @Param logo formData file false "Company logo image"
// @Param email formData string false "Company email"
// @Param phone formData string false "Company phone"
// @Param address formData string false "Company address"
// @Param website_url formData string false "Company website URL"
// @Param facebook_url formData string false "Facebook URL"
// @Param twitter_url formData string false "Twitter URL"
// @Param instagram_url formData string false "Instagram URL"
// @Param linkedin_url formData string false "LinkedIn URL"
// @Param youtube_url formData string false "YouTube URL"
// @Success 200 {object} utils.Response{data=models.CompanyInfoResponse} "Company information updated"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/company-info [put]
func (h *AdminManagementHandler) UpdateCompanyInfo(c *gin.Context) {
	// Get user from context (set by auth middleware)
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userIDStr := userIDInterface.(string)
	userID, _ := uuid.Parse(userIDStr)

	// Parse multipart form
	_, err := c.MultipartForm()
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to parse multipart form", err)
		return
	}

	var companyInfo models.CompanyInfo
	if err := h.db.First(&companyInfo).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new record if not exists
			companyInfo = models.CompanyInfo{}
		} else {
			utils.DatabaseErrorResponse(c, "Failed to get company information", err)
			return
		}
	}

	// Update fields from form data
	if name := c.PostForm("name"); name != "" {
		companyInfo.Name = name
	}
	if description := c.PostForm("description"); description != "" {
		companyInfo.Description = description
	}
	if email := c.PostForm("email"); email != "" {
		companyInfo.Email = email
	}
	if phone := c.PostForm("phone"); phone != "" {
		companyInfo.Phone = phone
	}
	if address := c.PostForm("address"); address != "" {
		companyInfo.Address = address
	}
	if websiteURL := c.PostForm("website_url"); websiteURL != "" {
		companyInfo.WebsiteURL = websiteURL
	}
	if facebookURL := c.PostForm("facebook_url"); facebookURL != "" {
		companyInfo.FacebookURL = facebookURL
	}
	if twitterURL := c.PostForm("twitter_url"); twitterURL != "" {
		companyInfo.TwitterURL = twitterURL
	}
	if instagramURL := c.PostForm("instagram_url"); instagramURL != "" {
		companyInfo.InstagramURL = instagramURL
	}
	if linkedinURL := c.PostForm("linkedin_url"); linkedinURL != "" {
		companyInfo.LinkedInURL = linkedinURL
	}
	if youtubeURL := c.PostForm("youtube_url"); youtubeURL != "" {
		companyInfo.YouTubeURL = youtubeURL
	}

	// Handle logo upload
	if logoFile, header, err := c.Request.FormFile("logo"); err == nil {
		defer logoFile.Close()

		// Upload company logo
		logoURL, err := h.fileStorageService.UploadFile(logoFile, header, models.FileCategoryCompanyLogo, userID, &services.FileUploadOptions{
			AltText:     fmt.Sprintf("Company logo for %s", companyInfo.Name),
			Description: fmt.Sprintf("Company logo for %s", companyInfo.Name),
		})
		if err != nil {
			utils.InternalServerErrorResponse(c, "Failed to upload company logo", err)
			return
		}
		companyInfo.LogoURL = logoURL
	}

	if err := h.db.Save(&companyInfo).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to update company information", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Company information updated successfully", companyInfo.ToResponse())
}

// Category Management

// @Summary Get all categories (Admin)
// @Description Get all categories including inactive ones for admin management
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=[]models.CategoryResponse} "Categories list"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/categories [get]
func (h *AdminManagementHandler) GetAllCategories(c *gin.Context) {
	var categories []models.Category

	if err := h.db.Order("sort_order ASC, name ASC").Find(&categories).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get categories", err)
		return
	}

	var response []models.CategoryResponse
	for _, category := range categories {
		response = append(response, category.ToResponse())
	}

	utils.SuccessResponse(c, http.StatusOK, "Categories retrieved successfully", response)
}

// @Summary Create new category (Admin)
// @Description Create a new event category
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.CreateCategoryRequest true "Category data"
// @Success 201 {object} utils.Response{data=models.CategoryResponse} "Category created"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 409 {object} utils.Response "Category already exists"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/categories [post]
func (h *AdminManagementHandler) CreateCategory(c *gin.Context) {
	var request models.CreateCategoryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if category already exists
	var existing models.Category
	if err := h.db.Where("name = ?", request.Name).First(&existing).Error; err == nil {
		utils.ConflictErrorResponse(c, "Category with this name already exists", nil)
		return
	}

	category := models.Category{
		Name:        request.Name,
		Description: request.Description,
		IconURL:     request.IconURL,
		IsActive:    request.IsActive,
		SortOrder:   request.SortOrder,
	}

	if err := h.db.Create(&category).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to create category", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Category created successfully", category.ToResponse())
}

// @Summary Update category (Admin)
// @Description Update an existing category
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Category ID"
// @Param request body models.UpdateCategoryRequest true "Category update data"
// @Success 200 {object} utils.Response{data=models.CategoryResponse} "Category updated"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Category not found"
// @Failure 409 {object} utils.Response "Category name already exists"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/categories/{id} [put]
func (h *AdminManagementHandler) UpdateCategory(c *gin.Context) {
	categoryID := c.Param("id")
	if categoryID == "" {
		utils.BadRequestErrorResponse(c, "Category ID is required", nil)
		return
	}

	categoryUUID, err := uuid.Parse(categoryID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid category ID", err)
		return
	}

	var request models.UpdateCategoryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	var category models.Category
	if err := h.db.First(&category, categoryUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Category not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get category", err)
		return
	}

	// Check if name is being changed and if it already exists
	if request.Name != "" && request.Name != category.Name {
		var existing models.Category
		if err := h.db.Where("name = ? AND id != ?", request.Name, categoryUUID).First(&existing).Error; err == nil {
			utils.ConflictErrorResponse(c, "Category with this name already exists", nil)
			return
		}
		category.Name = request.Name
	}

	// Update fields
	if request.Description != "" {
		category.Description = request.Description
	}
	if request.IconURL != "" {
		category.IconURL = request.IconURL
	}
	if request.IsActive != nil {
		category.IsActive = *request.IsActive
	}
	if request.SortOrder != 0 {
		category.SortOrder = request.SortOrder
	}

	if err := h.db.Save(&category).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to update category", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Category updated successfully", category.ToResponse())
}

// @Summary Delete category (Admin)
// @Description Soft delete a category
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Category ID"
// @Success 200 {object} utils.Response "Category deleted successfully"
// @Failure 400 {object} utils.Response "Invalid category ID"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Category not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/categories/{id} [delete]
func (h *AdminManagementHandler) DeleteCategory(c *gin.Context) {
	categoryID := c.Param("id")
	if categoryID == "" {
		utils.BadRequestErrorResponse(c, "Category ID is required", nil)
		return
	}

	categoryUUID, err := uuid.Parse(categoryID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid category ID", err)
		return
	}

	var category models.Category
	if err := h.db.First(&category, categoryUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Category not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get category", err)
		return
	}

	if err := h.db.Delete(&category).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to delete category", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Category deleted successfully", nil)
}

// Featured Events Management

// @Summary Toggle event featured status (Admin)
// @Description Mark/unmark an event as featured
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param request body map[string]bool true "Featured status"
// @Success 200 {object} utils.Response{data=models.Event} "Event featured status updated"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Event not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/events/{id}/featured [put]
func (h *AdminManagementHandler) ToggleEventFeatured(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		utils.BadRequestErrorResponse(c, "Event ID is required", nil)
		return
	}

	eventUUID, err := uuid.Parse(eventID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	var request map[string]bool
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	isFeatured, exists := request["is_featured"]
	if !exists {
		utils.BadRequestErrorResponse(c, "is_featured field is required", nil)
		return
	}

	var event models.Event
	if err := h.db.First(&event, eventUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Event not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get event", err)
		return
	}

	// Update featured status
	event.IsFeatured = isFeatured

	if err := h.db.Save(&event).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to update event featured status", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event featured status updated successfully", event)
}

// Ticket Template Testing

// @Summary Test ticket template generation (Admin)
// @Description Generate and email a test ticket PDF for template testing
// @Tags Admin Management
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TestTicketRequest true "Test ticket data"
// @Success 200 {object} utils.Response "Test ticket sent successfully"
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Event not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/test-ticket [post]
func (h *AdminManagementHandler) TestTicketTemplate(c *gin.Context) {
	var request models.TestTicketRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Get user from context (set by auth middleware)
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userIDStr := userIDInterface.(string)
	adminID, _ := uuid.Parse(userIDStr)

	// Get admin user details for email
	var adminUser models.User
	if err := h.db.First(&adminUser, adminID).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get admin user", err)
		return
	}

	// Get event details
	eventUUID, err := uuid.Parse(request.EventID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	var event models.Event
	if err := h.db.Preload("Organizer").First(&event, eventUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Event not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get event", err)
		return
	}

	// Create a mock individual ticket for testing
	mockTicket := &models.IndividualTicket{
		TicketID:     uuid.New(), // Mock ticket ID
		TicketNumber: fmt.Sprintf("TEST-%s-%d", adminID.String()[:8], time.Now().Unix()),
		Ticket: &models.Ticket{
			EventID: event.ID,
			Event:   &event,
			User:    &adminUser, // Use admin as the "attendee" for testing
		},
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Generate QR code data for the test ticket
	qrData := map[string]interface{}{
		"ticket_id":    mockTicket.TicketNumber,
		"event_id":     event.ID.String(),
		"test_mode":    true,
		"admin_test":   true,
		"generated_at": time.Now().Unix(),
	}

	qrJSON, _ := json.Marshal(qrData)
	mockTicket.QRCode = base64.StdEncoding.EncodeToString(qrJSON)

	// Generate ticket PDF
	attachment, err := h.universalTicketTemplateService.GenerateTicketAttachment(mockTicket)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to generate test ticket PDF", err)
		return
	}

	// Prepare test email data
	emailData := map[string]interface{}{
		"Title":         "Test Ticket Template - TIMRO TICKETS",
		"Message":       fmt.Sprintf("This is a test ticket for template verification. Event: %s. Generated for admin testing purposes.", event.Title),
		"EventTitle":    event.Title,
		"EventDate":     event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"EventLocation": event.Location,
		"TicketNumber":  mockTicket.TicketNumber,
		"AttendeeName":  fmt.Sprintf("%s %s (Admin Test)", adminUser.FirstName, adminUser.LastName),
		"TierName":      "Test Tier",
		"TicketURL":     "#", // Not applicable for test
		"EventURL":      "#", // Not applicable for test
	}

	// Send test email with attachment
	err = h.emailQueueService.QueueTicketWithAttachmentEmail(
		adminUser.Email,
		emailData,
		attachment,
	)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to send test ticket email", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Test ticket sent successfully to your email", map[string]interface{}{
		"ticket_number": mockTicket.TicketNumber,
		"sent_to":       adminUser.Email,
		"event_title":   event.Title,
		"test_mode":     true,
	})
}
