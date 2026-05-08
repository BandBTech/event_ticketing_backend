package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
)

// SecureQRService handles secure QR code generation and validation
type SecureQRService struct {
	config *config.Config
	secret string // Secret key for HMAC signing
}

// NewSecureQRService creates a new secure QR service
func NewSecureQRService(cfg *config.Config) *SecureQRService {
	return &SecureQRService{
		config: cfg,
		secret: cfg.App.SecretKey, // Use app secret for signing
	}
}

// SecureQRData represents the secure data encoded in QR codes
type SecureQRData struct {
	TicketID      string  `json:"tid"` // Ticket ID (UUID)
	EventID       string  `json:"eid"` // Event ID
	UserID        *string `json:"uid"` // User ID (optional for guest tickets)
	TransactionID string  `json:"txn"` // Transaction ID for payment verification
	TicketStatus  string  `json:"sts"` // Ticket status (active/used/refunded)
	IssuedAt      int64   `json:"iat"` // Issued at timestamp
	ExpiresAt     int64   `json:"exp"` // Expiration timestamp
	Signature     string  `json:"sig"` // HMAC signature for verification
}

// GenerateSecureQRPayload returns the base64-encoded JSON payload for the QR code
// (frontend can generate the QR image from this payload). This is a compact
// payload containing signed ticket data which the scanner can validate.
func (s *SecureQRService) GenerateSecureQRPayload(ticket *models.Ticket, event *models.Event) (string, error) {

	data := SecureQRData{
		TicketID:     ticket.ID.String(),
		EventID:      event.ID.String(),
		TicketStatus: string(ticket.Status),
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    event.EndDate.Add(24 * time.Hour).Unix(), // safer expiry buffer
	}

	// Generate signature FIRST
	signature, err := s.generateSignature(data)
	if err != nil {
		return "", utils.NewInternalServerError("failed to generate QR signature", err)
	}

	data.Signature = signature

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", utils.NewInternalServerError("failed to encode QR payload", err)
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// ValidateSecureQR validates a scanned QR code
func (s *SecureQRService) ValidateSecureQR(qrData string, eventID uuid.UUID, scannerUserID uuid.UUID) (*SecureQRData, error) {

	// 1. Decode base64
	jsonData, err := base64.StdEncoding.DecodeString(qrData)
	if err != nil {
		return nil, utils.NewBusinessLogicError("invalid QR format")
	}

	// 2. Parse JSON
	var data SecureQRData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, utils.NewBusinessLogicError("invalid QR payload")
	}

	// 3. Check expiry first (fast reject)
	if time.Now().Unix() > data.ExpiresAt {
		return nil, utils.NewBusinessLogicError("QR code expired")
	}

	// 4. Validate event match
	if data.EventID != eventID.String() {
		return nil, utils.NewBusinessLogicError("QR not valid for this event")
	}

	// 5. Recompute signature safely
	expectedSig, err := s.generateSignature(SecureQRData{
		TicketID:     data.TicketID,
		EventID:      data.EventID,
		TicketStatus: data.TicketStatus,
		IssuedAt:     data.IssuedAt,
		ExpiresAt:    data.ExpiresAt,
	})
	if err != nil {
		return nil, utils.NewInternalServerError("signature generation failed", err)
	}

	// 6. Constant-time comparison (IMPORTANT FIX)
	if !hmac.Equal([]byte(data.Signature), []byte(expectedSig)) {
		return nil, utils.NewBusinessLogicError("invalid QR signature")
	}

	// 7. Status validation (central rules)
	switch data.TicketStatus {
	case "refunded":
		return nil, utils.NewBusinessLogicError("ticket refunded")

	case "cancelled":
		return nil, utils.NewBusinessLogicError("ticket cancelled")

	case "used":
		return nil, utils.NewBusinessLogicError("ticket already used")

	case "active", "pending_verification":
		// allowed

	default:
		return nil, utils.NewBusinessLogicError("invalid ticket status")
	}

	// 8. OPTIONAL: track scanner usage (future audit/logging)
	_ = scannerUserID // keep for audit trail if needed later

	return &data, nil
}

// generateSignature creates an HMAC signature for the QR data
func (s *SecureQRService) generateSignature(data SecureQRData) (string, error) {
	// Create a copy without signature for signing
	dataCopy := data
	dataCopy.Signature = ""

	// Marshal to JSON
	jsonData, err := json.Marshal(dataCopy)
	if err != nil {
		return "", err
	}

	// Create HMAC
	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write(jsonData)
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature, nil
}
