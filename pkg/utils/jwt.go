package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims defines the claims in the JWT
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Roles  []string  `json:"roles"`
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
		return nil, fmt.Errorf("failed to create access token: %w", err)
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
		return nil, fmt.Errorf("failed to create refresh token: %w", err)
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
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.config.Secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Check if token is valid
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract the claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("failed to extract claims from token")
	}

	return claims, nil
}

// HashToken creates a secure hash of a token for database storage
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
