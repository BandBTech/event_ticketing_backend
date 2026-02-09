package middleware

import (
	"errors"
	"net/http"
	"strings"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthMiddleware is a middleware that verifies JWT tokens and user status
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use centralized comprehensive validation
		_, valid := utils.ValidateAuthToken(c, cfg)
		if !valid {
			c.Abort()
			return
		}

		c.Next()
	}
}

// RoleRequired middleware checks if the user has a specific role
func RoleRequired(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get roles from context
		roles, exists := c.Get("roles")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
			c.Abort()
			return
		}

		// Check if the user has the required role
		userRoles := roles.([]string)
		for _, r := range userRoles {
			if r == role {
				c.Next()
				return
			}
		}

		// User doesn't have the required role
		utils.ErrorResponse(c, http.StatusForbidden, "Permission denied: Required role not found", nil)
		c.Abort()
	}
}

// AnyRoleRequired middleware checks if the user has any of the specified roles
func AnyRoleRequired(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get roles from context
		userRolesInterface, exists := c.Get("roles")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
			c.Abort()
			return
		}

		// Check if the user has any of the required roles
		userRoles := userRolesInterface.([]string)
		for _, userRole := range userRoles {
			for _, requiredRole := range roles {
				if userRole == requiredRole {
					c.Next()
					return
				}
			}
		}

		// User doesn't have any of the required roles
		utils.ErrorResponse(c, http.StatusForbidden, "Permission denied: Required role not found", nil)
		c.Abort()
	}
}

// IsOrganizer checks if the user is an organizer (or has admin rights)
func IsOrganizer() gin.HandlerFunc {
	return AnyRoleRequired("admin", "subadmin", "organizer")
}

// IsApprovedOrganizer checks if the user is an approved organizer
func IsApprovedOrganizer(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly instead of creating new service instance
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles from database
		var user models.User
		if err := db.Preload("Roles.Permissions").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check if user has organizer role (strict check)
		isOrganizer := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				isOrganizer = true
				break
			}
		}

		// If not organizer, deny access
		if !isOrganizer {
			utils.ForbiddenErrorResponse(c, "Permission denied: Organizer role required", nil)
			c.Abort()
			return
		}

		// Check if organizer is approved
		if user.OrganizerStatus != "approved" {
			var err error
			switch user.OrganizerStatus {
			case "inactive":
				err = utils.NewOrganizerInactiveError()
			case "pending":
				err = utils.NewOrganizerPendingError()
			case "rejected":
				err = utils.NewOrganizerRejectedError()
			default:
				err = utils.NewOrganizerInactiveError()
			}
			utils.HandleError(c, err)
			c.Abort()
			return
		}

		c.Next()
	}
}

// IsApprovedOrganizerOrManager checks if user is an approved organizer OR a manager OR staff
func IsApprovedOrganizerOrManager(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly instead of creating new service instance
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles from database
		var user models.User
		if err := db.Preload("Roles.Permissions").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check if user has organizer, manager, or staff role
		isOrganizer := false
		isManager := false
		isStaff := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				isOrganizer = true
			}
			if role.Name == "manager" {
				isManager = true
			}
			if role.Name == "staff" {
				isStaff = true
			}
		}

		// If manager or staff, allow access
		if isManager || isStaff {
			c.Next()
			return
		}

		// If not organizer, deny access
		if !isOrganizer {
			utils.ForbiddenErrorResponse(c, "Permission denied: Organizer or Manager role required", nil)
			c.Abort()
			return
		}

		// Check if organizer is approved
		if user.OrganizerStatus != "approved" {
			var err error
			switch user.OrganizerStatus {
			case "inactive":
				err = utils.NewOrganizerInactiveError()
			case "pending":
				err = utils.NewOrganizerPendingError()
			case "rejected":
				err = utils.NewOrganizerRejectedError()
			default:
				err = utils.NewOrganizerInactiveError()
			}
			utils.HandleError(c, err)
			c.Abort()
			return
		}

		c.Next()
	}
}

// IsStaffOrManager checks if the user is a staff or manager
func IsStaffOrManager() gin.HandlerFunc {
	return AnyRoleRequired("staff", "manager")
}

// IsAdmin checks if the user is an admin
func IsAdmin() gin.HandlerFunc {
	return RoleRequired("admin")
}

// IsAdminOrSubAdmin checks if the user is an admin or subadmin
func IsAdminOrSubAdmin() gin.HandlerFunc {
	return AnyRoleRequired("admin", "subadmin")
}

// IsOrganizerStaff checks if the user is an organizer, staff, or manager
func IsOrganizerStaff() gin.HandlerFunc {
	return AnyRoleRequired("organizer", "staff", "manager")
}

// IsManager checks if the user is a manager
func IsManager() gin.HandlerFunc {
	return RoleRequired("manager")
}

// IsUser checks if the user has the "user" role
func IsUser() gin.HandlerFunc {
	return RoleRequired("user")
}

// IsOrganizerOrManager checks if the user is an organizer or manager
func IsOrganizerOrManager(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles
		var user models.User
		if err := db.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check roles
		isOrganizer := false
		isManager := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				isOrganizer = true
			}
			if role.Name == "manager" {
				isManager = true
			}
		}

		if isOrganizer || isManager {
			c.Next()
			return
		}

		utils.ForbiddenErrorResponse(c, "Permission denied: Organizer or Manager role required", nil)
		c.Abort()
	}
}

// IsTicketAccessAllowed checks if the user can access ticket operations (staff, manager, or approved organizer)
func IsTicketAccessAllowed(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles
		var user models.User
		if err := db.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check roles
		isOrganizer := false
		isStaff := false
		isManager := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				isOrganizer = true
			}
			if role.Name == "staff" {
				isStaff = true
			}
			if role.Name == "manager" {
				isManager = true
			}
		}

		if isStaff || isManager {
			c.Next()
			return
		}

		if isOrganizer {
			c.Next()
			return
		}

		utils.ForbiddenErrorResponse(c, "Permission denied: Insufficient permissions for ticket operations", nil)
		c.Abort()
	}
}

// RequirePermission checks if the user has the required permission
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles and permissions
		var user models.User
		if err := db.Preload("Roles.Permissions").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check if any role has the permission
		hasPermission := false
		for _, role := range user.Roles {
			for _, perm := range role.Permissions {
				if perm.Name == permission {
					hasPermission = true
					break
				}
			}
			if hasPermission {
				break
			}
		}

		if !hasPermission {
			utils.ForbiddenErrorResponse(c, "Permission denied", nil)
			c.Abort()
			return
		}

		c.Next()
	}
}

func GetUserFromToken(cfg *config.Config) gin.HandlerFunc {
	jwtService := utils.NewJWTService(&cfg.JWT)

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No token, continue as unauthenticated
			c.Next()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			// Invalid format, continue as unauthenticated
			c.Next()
			return
		}

		tokenString := parts[1]
		claims, err := jwtService.ValidateToken(tokenString)
		if err != nil {
			// Invalid token, continue as unauthenticated
			c.Next()
			return
		}

		// Set user info in context
		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("roles", claims.Roles)
		c.Set("authenticated", true)

		c.Next()
	}
}

// IsOrganizerRole checks if the user has organizer role (regardless of approval status)
// Used for profile management where organizers need access before approval
func IsOrganizerRole(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("userID")
		if !exists {
			utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
			c.Abort()
			return
		}

		userID := userIDInterface.(uuid.UUID)

		// Use database directly instead of creating new service instance
		db := database.GetDB()
		if db == nil {
			utils.InternalServerErrorResponse(c, "Database connection unavailable", nil)
			c.Abort()
			return
		}

		// Get user with roles from database
		var user models.User
		if err := db.Preload("Roles.Permissions").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.UnauthorizedErrorResponse(c, "User not found", nil)
			} else {
				utils.InternalServerErrorResponse(c, "Failed to load user data", err)
			}
			c.Abort()
			return
		}

		// Check if user has organizer role only (strict check)
		hasAccess := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			utils.ForbiddenErrorResponse(c, "Permission denied: Organizer role required", nil)
			c.Abort()
			return
		}

		c.Next()
	}
}
