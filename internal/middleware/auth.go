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

// AuthMiddleware is a middleware that verifies JWT tokens
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	jwtService := utils.NewJWTService(&cfg.JWT)

	return func(c *gin.Context) {
		// Get Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Authorization header missing", nil)
			c.Abort()
			return
		}

		// Check if it's a Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Invalid authorization format", nil)
			c.Abort()
			return
		}

		// Extract token
		tokenString := parts[1]

		// Validate token
		claims, err := jwtService.ValidateToken(tokenString)
		if err != nil {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Invalid or expired token", err)
			c.Abort()
			return
		}

		// Set user info in context
		c.Set("user_id", claims.UserID)
		c.Set("userID", claims.UserID) // Keep for backward compatibility
		c.Set("email", claims.Email)
		c.Set("roles", claims.Roles)

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

// IsApprovedOrganizer checks if the user is an approved organizer (or has admin rights)
func IsApprovedOrganizer(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Get user ID from context
		userIDInterface, exists := c.Get("user_id")
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

		// Check if user has admin or subadmin role (they bypass organizer approval)
		isAdmin := false
		isSubAdmin := false
		isOrganizer := false
		for _, role := range user.Roles {
			if role.Name == "admin" {
				isAdmin = true
				break
			}
			if role.Name == "subadmin" {
				isSubAdmin = true
			}
			if role.Name == "organizer" {
				isOrganizer = true
			}
		}

		// If admin or subadmin, allow access
		if isAdmin || isSubAdmin {
			c.Next()
			return
		}

		// If not organizer, deny access
		if !isOrganizer {
			utils.ForbiddenErrorResponse(c, "Permission denied: Organizer role required", nil)
			c.Abort()
			return
		}

		// Check if organizer is approved
		if user.OrganizerStatus != "approved" {
			var message string
			switch user.OrganizerStatus {
			case "inactive":
				message = "Your organizer account is inactive. Please complete your profile and submit for approval."
			case "pending":
				message = "Your organizer account is pending approval. Please wait for admin review."
			case "rejected":
				message = "Your organizer account has been rejected. Please contact support for more information."
			default:
				message = "Your organizer account requires approval. Please contact support."
			}
			utils.ForbiddenErrorResponse(c, message, nil)
			c.Abort()
			return
		}

		c.Next()
	}
}

// IsUser checks if the user has the "user" role
func IsUser() gin.HandlerFunc {
	return RoleRequired("user")
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

// GetUserFromToken extracts user info from token and attaches to the context
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
		c.Set("user_id", claims.UserID)
		c.Set("userID", claims.UserID) // Keep for backward compatibility
		c.Set("email", claims.Email)
		c.Set("roles", claims.Roles)
		c.Set("authenticated", true)

		c.Next()
	}
}
