package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Claims defines the claims in the JWT
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Roles  []string  `json:"roles"`
	jwt.RegisteredClaims
}

// TicketClaims defines the claims for ticket access JWT
type TicketClaims struct {
	CheckoutToken string     `json:"ctoken"`
	EventID       uuid.UUID  `json:"event_id"`
	ActorID       *uuid.UUID `json:"actor_id,omitempty"`
	Type          string     `json:"type"`

	jwt.RegisteredClaims
}

// JWTService provides methods for JWT operations
type JWTService struct {
	config *config.JWTConfig
}

// NewJWTService creates a new JWT service
func NewJWTService(config *config.JWTConfig) *JWTService {
	return &JWTService{
		config: config,
	}
}

func (j *JWTService) GenerateTicketToken(
	event models.Event,
	actorID uuid.UUID,
	checkoutToken string,
) (string, error) {

	now := time.Now()

	claims := TicketClaims{
		CheckoutToken: checkoutToken,
		EventID:       event.ID,
		ActorID:       &actorID,
		Type:          "ticket_access",

		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(
				event.EndDate.Add(24 * time.Hour),
			),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.Secret))
}

func (j *JWTService) ParseTicketToken(tokenStr string) (*TicketClaims, error) {
	claims := &TicketClaims{}

	token, err := jwt.ParseWithClaims(
		tokenStr,
		claims,
		func(t *jwt.Token) (interface{}, error) {
			return []byte(j.config.Secret), nil
		},
	)

	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired ticket token")
	}

	if claims.Type != "ticket_access" {
		return nil, errors.New("invalid token type")
	}

	return claims, nil
}

// GenerateTokens creates a new pair of access and refresh tokens
func (j *JWTService) GenerateTokens(user *models.User) (*models.TokenResponse, error) {
	// Extract roles for the claims
	roles := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roles[i] = role.Name
	}

	// Create access token
	accessTokenExpiry := time.Now().Add(j.config.AccessTokenTTL)
	accessTokenClaims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Roles:  roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessTokenExpiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    j.config.Issuer,
			Subject:   user.ID.String(),
			Audience:  []string{j.config.Audience},
			ID:        uuid.New().String(),
		},
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessTokenClaims).SignedString([]byte(j.config.Secret))
	if err != nil {
		return nil, NewInternalServerError("Failed to create access token.", err)
	}

	// Create refresh token
	refreshTokenExpiry := time.Now().Add(j.config.RefreshTokenTTL)
	refreshTokenClaims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshTokenExpiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    j.config.Issuer,
			Subject:   user.ID.String(),
			Audience:  []string{j.config.Audience},
			ID:        uuid.New().String(),
		},
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshTokenClaims).SignedString([]byte(j.config.Secret))
	if err != nil {
		return nil, NewInternalServerError("Failed to create refresh token.", err)
	}

	// Return token response
	return &models.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// ValidateToken validates a JWT token
func (j *JWTService) ValidateToken(tokenString string) (*Claims, error) {
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, NewInvalidTokenError()
		}
		return []byte(j.config.Secret), nil
	})

	if err != nil {
		// Check for specific JWT error messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "token is expired") {
			return nil, NewTokenExpiredError()
		}
		if strings.Contains(errMsg, "signature is invalid") {
			return nil, NewInvalidTokenError()
		}
		return nil, NewInvalidTokenError()
	}

	// Check if token is valid
	if !token.Valid {
		return nil, NewInvalidTokenError()
	}

	// Extract the claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, NewInternalServerError("Failed to extract claims from token.", nil)
	}

	return claims, nil
}

// HashToken creates a secure hash of a token for database storage
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// ValidateTokenWithUser validates JWT token and user status, returning appropriate HTTP responses
func (j *JWTService) ValidateTokenWithUser(c *gin.Context, cfg *config.Config) (*Claims, bool) {
	// Get Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		UnauthorizedErrorResponse(c, "Authorization header missing", nil)
		return nil, false
	}

	// Check if it's a Bearer token
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		UnauthorizedErrorResponse(c, "Invalid authorization format", nil)
		return nil, false
	}

	// Extract token
	tokenString := parts[1]

	// Validate token
	claims, err := j.ValidateToken(tokenString)
	if err != nil {
		HandleError(c, err)
		return nil, false
	}

	// Validate user exists and is active
	userID := claims.UserID

	// Import database here to avoid circular imports
	// This is a bit of a compromise, but necessary for centralized validation
	db := database.GetDB()
	if db == nil {
		InternalServerErrorResponse(c, "Database connection unavailable", nil)
		return nil, false
	}

	// Get user with minimal fields needed for validation
	var user models.User
	if err := db.Select("id, email, account_status").Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			UnauthorizedErrorResponse(c, "User not found", nil)
		} else {
			InternalServerErrorResponse(c, "Failed to validate user", err)
		}
		return nil, false
	}

	// Check account status
	if user.AccountStatus != "active" {
		switch user.AccountStatus {
		case "inactive":
			HandleError(c, NewAccountInactiveError())
		case "suspended":
			HandleError(c, NewAccountSuspendedError())
		default:
			HandleError(c, NewAccountInactiveError())
		}
		return nil, false
	}

	// Set user info in context for successful validation
	c.Set("user_id", userID) // Use snake_case for consistency
	c.Set("userID", userID)  // Keep camelCase for backward compatibility
	c.Set("email", claims.Email)
	c.Set("roles", claims.Roles)

	return claims, true
}

// ValidateTokenForHandler validates JWT token and user status for use in handlers
// Returns claims if valid, or sends appropriate error response and returns false
func ValidateTokenForHandler(c *gin.Context, cfg *config.Config) (*Claims, bool) {
	jwtService := NewJWTService(&cfg.JWT)
	return jwtService.ValidateTokenWithUser(c, cfg)
}

// ValidateAuthToken provides comprehensive JWT and user validation for authenticated APIs
// This function should be called at the beginning of any handler that requires authentication
// It validates the token, checks user existence, account status, and handles all error responses
func ValidateAuthToken(c *gin.Context, cfg *config.Config) (*Claims, bool) {
	jwtService := NewJWTService(&cfg.JWT)

	// Extract and validate Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		HandleError(c, NewUnauthorizedError("AUTHORIZATION_HEADER_MISSING"))
		return nil, false
	}

	// Validate Bearer token format
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		HandleError(c, NewUnauthorizedError("INVALID_AUTHORIZATION_FORMAT"))
		return nil, false
	}

	tokenString := parts[1]
	if tokenString == "" {
		UnauthorizedErrorResponse(c, "TOKEN_MISSING", nil)
		return nil, false
	}

	// Validate JWT token structure and signature
	claims, err := jwtService.ValidateToken(tokenString)
	if err != nil {
		HandleError(c, err)
		return nil, false
	}

	// Validate user exists in database
	db := database.GetDB()
	if db == nil {
		InternalServerErrorResponse(c, "DATABASE_UNAVAILABLE", nil)
		return nil, false
	}

	var user models.User
	if err := db.Select("id, email, account_status").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			UnauthorizedErrorResponse(c, "USER_NOT_FOUND", nil)
		} else {
			InternalServerErrorResponse(c, "USER_VALIDATION_FAILED", err)
		}
		return nil, false
	}

	// Validate user account status
	switch user.AccountStatus {
	case "active":
		// User is active, proceed
	case "inactive":
		HandleError(c, NewAccountInactiveError())
		return nil, false
	case "suspended":
		HandleError(c, NewAccountSuspendedError())
		return nil, false
	default:
		HandleError(c, NewForbiddenError("Invalid account status."))
		return nil, false
	}

	// Set validated user information in context
	c.Set("user_id", claims.UserID)
	c.Set("userID", claims.UserID) // Backward compatibility
	c.Set("email", claims.Email)
	c.Set("roles", claims.Roles)

	return claims, true
}
