package middleware

import (
	"net/http"
	"strings"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
		c.Set("userID", claims.UserID)
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

// PermissionRequired middleware checks if the user has a specific permission
func PermissionRequired(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context
		userID, exists := c.Get("userID")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
			c.Abort()
			return
		}

		// Get user with roles and permissions
		authService := services.NewAuthService(nil) // This isn't ideal, should be injected
		user, err := authService.GetUserByID(userID.(uuid.UUID))
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to load user data", nil)
			c.Abort()
			return
		}

		// Check if user has the required permission
		if utils.HasPermission(user, resource, action) {
			c.Next()
			return
		}

		// User doesn't have the required permission
		utils.ErrorResponse(c, http.StatusForbidden, "Permission denied: Required permission not found", nil)
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
	return AnyRoleRequired("admin", "organizer")
}

// IsAdmin checks if the user is an admin
func IsAdmin() gin.HandlerFunc {
	return RoleRequired("admin")
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
		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("roles", claims.Roles)
		c.Set("authenticated", true)

		c.Next()
	}
}
