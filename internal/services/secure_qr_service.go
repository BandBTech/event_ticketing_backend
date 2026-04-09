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
	"github.com/skip2/go-qrcode"
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

// GenerateSecureQR generates a secure QR code for a ticket
func (s *SecureQRService) GenerateSecureQR(ticket *models.Ticket, event *models.Event) (string, error) {
	// Create secure data
	data := SecureQRData{
		TicketID:     ticket.ID.String(),
		EventID:      event.ID.String(),
		TicketStatus: string(ticket.Status),
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    event.EndDate.Unix(), // Valid until event ends
	}

	// Add user ID if available
	if ticket.UserID != nil {
		uid := ticket.UserID.String()
		data.UserID = &uid
	}

	// Add transaction ID if available
	if ticket.TransactionID != nil {
		data.TransactionID = ticket.TransactionID.String()
	}

	// Generate signature
	signature, err := s.generateSignature(data)
	if err != nil {
		return "", utils.NewInternalServerError("Failed to generate signature.", err)
	}
	data.Signature = signature

	// Marshal to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", utils.NewInternalServerError("Failed to marshal QR data.", err)
	}

	// Generate QR code PNG (base64) for email/pdf usage
	qrCode, err := qrcode.Encode(string(jsonData), qrcode.High, 256)
	if err != nil {
		return "", utils.NewInternalServerError("Failed to generate QR code.", err)
	}

	return base64.StdEncoding.EncodeToString(qrCode), nil
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
		ExpiresAt:    event.EndDate.Unix(),
	}

	if ticket.UserID != nil {
		uid := ticket.UserID.String()
		data.UserID = &uid
	}

	if ticket.TransactionID != nil {
		data.TransactionID = ticket.TransactionID.String()
	}

	signature, err := s.generateSignature(data)
	if err != nil {
		return "", utils.NewInternalServerError("Failed to generate signature.", err)
	}
	data.Signature = signature

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", utils.NewInternalServerError("Failed to marshal QR payload.", err)
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// ValidateSecureQR validates a scanned QR code
func (s *SecureQRService) ValidateSecureQR(qrData string, eventID uuid.UUID, scannerUserID uuid.UUID) (*SecureQRData, error) {
	// Decode base64
	jsonData, err := base64.StdEncoding.DecodeString(qrData)
	if err != nil {
		return nil, utils.NewBusinessLogicError("Invalid QR code format.")
	}

	// Unmarshal JSON
	var data SecureQRData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, utils.NewBusinessLogicError("Invalid QR code data.")
	}

	// Verify signature
	expectedSig, err := s.generateSignature(data)
	if err != nil {
		return nil, utils.NewInternalServerError("Signature generation failed.", err)
	}

	if !hmac.Equal([]byte(data.Signature), []byte(expectedSig)) {
		return nil, utils.NewBusinessLogicError("Invalid QR code signature.")
	}

	// Check expiration
	if time.Now().Unix() > data.ExpiresAt {
		return nil, utils.NewBusinessLogicError("QR code expired.")
	}

	// Check event ID
	if data.EventID != eventID.String() {
		return nil, utils.NewBusinessLogicError("QR code not valid for this event.")
	}

	// CRITICAL: Verify ticket status from QR matches expected status
	// This prevents use of QR codes generated before refunds or cancellations
	if data.TicketStatus == "refunded" {
		return nil, utils.NewBusinessLogicError("Ticket has been refunded and cannot be used.")
	}

	if data.TicketStatus == "cancelled" {
		return nil, utils.NewBusinessLogicError("Ticket has been cancelled and cannot be used.")
	}

	if data.TicketStatus == "used" {
		return nil, utils.NewBusinessLogicError("This QR code has already been used for check-in.")
	}

	// Verify ticket status is active
	if data.TicketStatus != "active" && data.TicketStatus != "pending_verification" {
		return nil, utils.NewBusinessLogicError("Ticket status is not valid for check-in.")
	}

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
