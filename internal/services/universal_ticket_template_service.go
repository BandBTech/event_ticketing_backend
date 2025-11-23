package services

import (
	"bytes"
	"fmt"
	"log"
	"sync"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
)

// UniversalTicketTemplateService handles generation of universal ticket PDFs
type UniversalTicketTemplateService struct {
	fontMutex  sync.RWMutex
	fonts      map[string][]byte
	cache      map[string][]byte // Cache for generated tickets
	cacheMutex sync.RWMutex
}

// NewUniversalTicketTemplateService creates a new universal ticket template service
func NewUniversalTicketTemplateService() *UniversalTicketTemplateService {
	service := &UniversalTicketTemplateService{
		fonts: make(map[string][]byte),
		cache: make(map[string][]byte),
	}

	// Start cache cleanup goroutine
	go service.cacheCleanup()

	return service
}

// GenerateTicketPDF generates a universal ticket PDF for any event
func (s *UniversalTicketTemplateService) GenerateTicketPDF(ticket *models.IndividualTicket) ([]byte, error) {
	// Check cache first
	cacheKey := s.getCacheKey(ticket)
	s.cacheMutex.RLock()
	if cached, exists := s.cache[cacheKey]; exists {
		s.cacheMutex.RUnlock()
		return cached, nil
	}
	s.cacheMutex.RUnlock()

	// Generate PDF with portrait layout
	pdf := gofpdf.New("P", "mm", "A5", "") // A5 for portrait ticket size
	pdf.AddPage()

	// Set background color (white with subtle border)
	pdf.SetFillColor(255, 255, 255)
	pdf.Rect(0, 0, 148, 210, "F")

	// Add decorative border
	s.addDecorativeBorder(pdf)

	// Add header with logos
	s.addHeaderWithLogos(pdf, ticket)

	// Add event image
	s.addEventImage(pdf, ticket)

	// Add ticket title
	s.addTicketTitle(pdf)

	// Add QR code section
	s.addQRCodeSection(pdf, ticket)

	// Add ticket details below QR
	s.addTicketDetails(pdf, ticket)

	// Add footer with company info
	s.addCompanyFooter(pdf, ticket)

	// Generate PDF bytes
	var buf bytes.Buffer
	err := pdf.Output(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PDF: %w", err)
	}

	pdfData := buf.Bytes()

	// Cache the result
	s.cacheMutex.Lock()
	s.cache[cacheKey] = pdfData
	s.cacheMutex.Unlock()

	return pdfData, nil
}

// addDecorativeBorder adds a decorative border to the ticket
func (s *UniversalTicketTemplateService) addDecorativeBorder(pdf *gofpdf.Fpdf) {
	// Outer border
	pdf.SetDrawColor(37, 99, 235) // Blue border
	pdf.SetLineWidth(0.5)
	pdf.Rect(5, 5, 138, 200, "D")

	// Inner decorative border
	pdf.SetDrawColor(229, 231, 235) // Light gray
	pdf.SetLineWidth(0.2)
	pdf.Rect(8, 8, 132, 194, "D")
}

// addHeaderWithLogos adds the header section with company and organizer logos
func (s *UniversalTicketTemplateService) addHeaderWithLogos(pdf *gofpdf.Fpdf, ticket *models.IndividualTicket) {
	// Company logo placeholder (top left) - You would load actual logo here
	pdf.SetFont("Arial", "B", 12)
	pdf.SetTextColor(37, 99, 235)
	pdf.Text(10, 18, "TIMRO")

	// Organizer logo placeholder (top right) - You would load actual organizer logo here
	if ticket.Ticket != nil && ticket.Ticket.Event != nil && ticket.Ticket.Event.Organizer != nil {
		organizerName := ticket.Ticket.Event.Organizer.FirstName + " " + ticket.Ticket.Event.Organizer.LastName
		if len(organizerName) > 15 {
			organizerName = organizerName[:12] + "..."
		}
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(107, 114, 128)
		pdf.Text(110, 18, organizerName)
	}

	// Decorative line under header
	pdf.SetDrawColor(229, 231, 235)
	pdf.SetLineWidth(0.3)
	pdf.Line(10, 25, 138, 25)
}

// addEventImage adds the event banner image in the center
func (s *UniversalTicketTemplateService) addEventImage(pdf *gofpdf.Fpdf, ticket *models.IndividualTicket) {
	// Event image placeholder - In a real implementation, you would load the actual event banner
	// For now, we'll add a placeholder rectangle
	pdf.SetFillColor(248, 250, 252)
	pdf.SetDrawColor(229, 231, 235)
	pdf.Rect(15, 35, 118, 60, "FD")

	// Placeholder text for event image
	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(107, 114, 128)
	pdf.Text(60, 65, "EVENT IMAGE")

	// Event title under image
	if ticket.Ticket != nil && ticket.Ticket.Event != nil {
		eventTitle := ticket.Ticket.Event.Title
		if len(eventTitle) > 25 {
			eventTitle = eventTitle[:22] + "..."
		}
		pdf.SetFont("Arial", "B", 11)
		pdf.SetTextColor(31, 41, 55)
		pdf.Text(74-float64(len(eventTitle))*0.8, 105, eventTitle)
	}
}

// addTicketTitle adds the "EVENT TICKET" title
func (s *UniversalTicketTemplateService) addTicketTitle(pdf *gofpdf.Fpdf) {
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(37, 99, 235)
	pdf.Text(50, 120, "EVENT TICKET")
}

// addQRCodeSection adds the QR code in the center
func (s *UniversalTicketTemplateService) addQRCodeSection(pdf *gofpdf.Fpdf, ticket *models.IndividualTicket) {
	// Generate QR code
	qrCode, err := qrcode.Encode(ticket.QRCode, qrcode.Medium, 256)
	if err != nil {
		log.Printf("Failed to generate QR code: %v", err)
		return
	}

	// Register QR code image
	qrImageName := "qrcode_" + ticket.TicketNumber
	pdf.RegisterImageOptionsReader(qrImageName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(qrCode))

	// Add QR code to PDF (centered)
	pdf.ImageOptions(qrImageName, 49, 130, 50, 50, false, gofpdf.ImageOptions{}, 0, "")

	// Add instruction text
	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(107, 114, 128)
	pdf.Text(45, 190, "Scan QR code at entrance")
}

// addTicketDetails adds ticket information below the QR code
func (s *UniversalTicketTemplateService) addTicketDetails(pdf *gofpdf.Fpdf, ticket *models.IndividualTicket) {
	y := 195.0

	// Ticket number
	pdf.SetFont("Arial", "B", 10)
	pdf.SetTextColor(31, 41, 55)
	pdf.Text(15, y, "Ticket #: "+ticket.TicketNumber)
	y += 8

	// Tier information
	tierName := "Standard"
	if ticket.Ticket != nil && ticket.Ticket.Event != nil {
		// Check if event has tiers and find the appropriate tier
		// For now, we'll use a default tier or check event pricing
		if ticket.Ticket.Event.Tiers != nil && len(ticket.Ticket.Event.Tiers) > 0 {
			// This is a simplified approach - in reality you'd match the ticket to a specific tier
			tierName = ticket.Ticket.Event.Tiers[0].TierName
		}
	}
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(31, 41, 55)
	pdf.Text(15, y, "Tier: "+tierName)
	y += 8

	// Venue
	if ticket.Ticket != nil && ticket.Ticket.Event != nil {
		venue := ticket.Ticket.Event.VenueName
		if len(venue) > 20 {
			venue = venue[:17] + "..."
		}
		pdf.Text(15, y, "Venue: "+venue)
		y += 8
	}

	// Date and time
	if ticket.Ticket != nil && ticket.Ticket.Event != nil {
		eventDate := ticket.Ticket.Event.StartDate.Format("Jan 2, 2006 3:04 PM")
		pdf.Text(15, y, "Date: "+eventDate)
	}
}

// addCompanyFooter adds company information and contact details at the bottom
func (s *UniversalTicketTemplateService) addCompanyFooter(pdf *gofpdf.Fpdf, ticket *models.IndividualTicket) {
	// Company logo placeholder at bottom
	pdf.SetFont("Arial", "B", 10)
	pdf.SetTextColor(37, 99, 235)
	pdf.Text(10, 240, "TIMRO TICKETS")

	// Contact information
	pdf.SetFont("Arial", "", 7)
	pdf.SetTextColor(107, 114, 128)
	pdf.Text(10, 248, "www.timrotickets.com | support@timrotickets.com")
	pdf.Text(10, 254, "+977-1234567890")

	// Terms and conditions
	pdf.Text(10, 262, "Terms: Non-transferable, non-refundable. Valid ID required.")

	// Generated timestamp
	pdf.SetFont("Arial", "", 6)
	pdf.Text(10, 268, "Generated: "+time.Now().Format("Jan 2, 2006 3:04 PM"))

	// Decorative bottom border
	pdf.SetDrawColor(229, 231, 235)
	pdf.SetLineWidth(0.3)
	pdf.Line(10, 275, 138, 275)
}

// GenerateTicketAttachment creates an email attachment for a ticket PDF
func (s *UniversalTicketTemplateService) GenerateTicketAttachment(ticket *models.IndividualTicket) (*models.EmailAttachment, error) {
	pdfData, err := s.GenerateTicketPDF(ticket)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("ticket-%s.pdf", ticket.TicketNumber)

	return &models.EmailAttachment{
		Filename:    filename,
		ContentType: "application/pdf",
		Data:        pdfData,
		IsBase64:    false,
	}, nil
}

// getCacheKey generates a cache key for the ticket
func (s *UniversalTicketTemplateService) getCacheKey(ticket *models.IndividualTicket) string {
	return fmt.Sprintf("%s_%s_%d",
		ticket.TicketNumber,
		ticket.UpdatedAt.Format("20060102150405"),
		len(ticket.QRCode))
}

// cacheCleanup periodically cleans up old cached items
func (s *UniversalTicketTemplateService) cacheCleanup() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cacheMutex.Lock()
		// Clear cache every 30 minutes to ensure fresh content
		s.cache = make(map[string][]byte)
		s.cacheMutex.Unlock()
	}
}

// PreloadFonts preloads fonts for better performance
func (s *UniversalTicketTemplateService) PreloadFonts() error {
	s.fontMutex.Lock()
	defer s.fontMutex.Unlock()

	// Fonts are handled by gofpdf internally, but we can add custom fonts here if needed
	return nil
}

// GenerateBulkTickets generates multiple tickets concurrently for better performance
func (s *UniversalTicketTemplateService) GenerateBulkTickets(tickets []*models.IndividualTicket) (map[string][]byte, error) {
	results := make(map[string][]byte)
	resultsMutex := sync.Mutex{}

	// Use worker pool for concurrent generation
	workerCount := 5 // Adjust based on system capabilities
	if len(tickets) < workerCount {
		workerCount = len(tickets)
	}

	jobs := make(chan *models.IndividualTicket, len(tickets))
	resultsChan := make(chan struct {
		ticketNumber string
		data         []byte
		err          error
	}, len(tickets))

	// Start workers
	for i := 0; i < workerCount; i++ {
		go func() {
			for ticket := range jobs {
				pdfData, err := s.GenerateTicketPDF(ticket)
				resultsChan <- struct {
					ticketNumber string
					data         []byte
					err          error
				}{
					ticketNumber: ticket.TicketNumber,
					data:         pdfData,
					err:          err,
				}
			}
		}()
	}

	// Send jobs
	go func() {
		for _, ticket := range tickets {
			jobs <- ticket
		}
		close(jobs)
	}()

	// Collect results
	for i := 0; i < len(tickets); i++ {
		result := <-resultsChan
		if result.err != nil {
			return nil, fmt.Errorf("failed to generate ticket %s: %w", result.ticketNumber, result.err)
		}
		resultsMutex.Lock()
		results[result.ticketNumber] = result.data
		resultsMutex.Unlock()
	}

	return results, nil
}
