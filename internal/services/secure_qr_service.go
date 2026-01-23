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
	TicketNumber string  `json:"tn"`  // Ticket number
	EventID      string  `json:"eid"` // Event ID
	UserID       *string `json:"uid"` // User ID (optional for guest tickets)
	IssuedAt     int64   `json:"iat"` // Issued at timestamp
	ExpiresAt    int64   `json:"exp"` // Expiration timestamp
	CheckInCount int     `json:"cic"` // Current check-in count
	MaxCheckIns  int     `json:"mci"` // Maximum allowed check-ins
	Signature    string  `json:"sig"` // HMAC signature for verification
}

// GenerateSecureQR generates a secure QR code for a ticket
func (s *SecureQRService) GenerateSecureQR(ticket *models.IndividualTicket, event *models.Event, maxCheckIns int) (string, error) {
	// Get current check-in count
	checkInCount := 0
	if ticket.CheckInTime != nil {
		checkInCount = 1 // For now, simple check-in tracking
	}

	// Create secure data
	data := SecureQRData{
		TicketNumber: ticket.TicketNumber,
		EventID:      event.ID.String(),
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    event.EndDate.Unix(), // Valid until event ends
		CheckInCount: checkInCount,
		MaxCheckIns:  maxCheckIns,
	}

	// Add user ID if available
	if ticket.Ticket != nil && ticket.Ticket.UserID != nil {
		uid := ticket.Ticket.UserID.String()
		data.UserID = &uid
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
func (s *SecureQRService) GenerateSecureQRPayload(ticket *models.IndividualTicket, event *models.Event, maxCheckIns int) (string, error) {
	// Get current check-in count
	checkInCount := 0
	if ticket.CheckInTime != nil {
		checkInCount = 1
	}

	data := SecureQRData{
		TicketNumber: ticket.TicketNumber,
		EventID:      event.ID.String(),
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    event.EndDate.Unix(),
		CheckInCount: checkInCount,
		MaxCheckIns:  maxCheckIns,
	}

	if ticket.Ticket != nil && ticket.Ticket.UserID != nil {
		uid := ticket.Ticket.UserID.String()
		data.UserID = &uid
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

	// Check if maximum check-ins reached
	if data.CheckInCount >= data.MaxCheckIns {
		return nil, utils.NewBusinessLogicError("Maximum check-ins reached for this ticket.")
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

// CanCheckIn determines if a ticket can be checked in
func (s *SecureQRService) CanCheckIn(qrData *SecureQRData) bool {
	return qrData.CheckInCount < qrData.MaxCheckIns
}

// GetRemainingCheckIns returns how many check-ins are remaining
func (s *SecureQRService) GetRemainingCheckIns(qrData *SecureQRData) int {
	return qrData.MaxCheckIns - qrData.CheckInCount
}

// UpdateCheckInCount updates the check-in count in secure QR data
func (s *SecureQRService) UpdateCheckInCount(qrData *SecureQRData) *SecureQRData {
	updated := *qrData
	updated.CheckInCount++
	updated.IssuedAt = time.Now().Unix() // Update timestamp

	// Generate new signature
	signature, _ := s.generateSignature(updated)
	updated.Signature = signature

	return &updated
}
