package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/png"
	"log"
	"strings"

	"event-ticketing-backend/internal/models"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/draw"
	"golang.org/x/image/font/gofont/goregular"
)

// TicketImageService handles generation of ticket images with QR codes and event details
type TicketImageService struct {
	font *truetype.Font
}

// NewTicketImageService creates a new ticket image service
func NewTicketImageService() *TicketImageService {
	// Load default font
	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		log.Printf("Warning: Failed to load default font, using fallback: %v", err)
		font = nil
	}

	return &TicketImageService{
		font: font,
	}
}

// GenerateTicketImage creates a ticket image with QR code and event details
func (s *TicketImageService) GenerateTicketImage(ticket *models.IndividualTicket) ([]byte, error) {
	// Image dimensions
	width := 800
	height := 400

	// Create a new RGBA image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill background with white
	white := color.RGBA{255, 255, 255, 255}
	stdDraw.Draw(img, img.Bounds(), &image.Uniform{white}, image.Point{}, stdDraw.Src)

	// Generate QR code
	qrCode, err := qrcode.Encode(ticket.QRCode, qrcode.Medium, 256)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Decode QR code image
	qrImg, _, err := image.Decode(bytes.NewReader(qrCode))
	if err != nil {
		return nil, fmt.Errorf("failed to decode QR code image: %w", err)
	}

	// Draw QR code on the right side
	qrSize := 200
	qrX := width - qrSize - 50
	qrY := (height - qrSize) / 2

	// Scale QR code to fit
	scaledQR := s.scaleImage(qrImg, qrSize, qrSize)
	stdDraw.Draw(img, image.Rect(qrX, qrY, qrX+qrSize, qrY+qrSize), scaledQR, image.Point{}, stdDraw.Over)

	// Draw ticket details on the left side
	s.drawTicketDetails(img, ticket, qrX-50)

	// Convert to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode image: %w", err)
	}

	return buf.Bytes(), nil
}

// drawTicketDetails draws the ticket information on the image
func (s *TicketImageService) drawTicketDetails(img *image.RGBA, ticket *models.IndividualTicket, maxX int) {
	if s.font == nil {
		return // Skip text drawing if font not available
	}

	height := img.Bounds().Dy() // Get height from image bounds

	ctx := freetype.NewContext()
	ctx.SetFont(s.font)
	ctx.SetFontSize(24)
	ctx.SetClip(img.Bounds())
	ctx.SetDst(img)
	ctx.SetSrc(image.Black)

	// Starting position
	x, y := 50, 80

	// Draw event title
	s.drawText(ctx, "TIMRO TICKETS", x, y, 28, image.NewUniform(color.RGBA{37, 99, 235, 255})) // Blue color
	y += 50

	// Draw event title
	eventTitle := ticket.Ticket.Event.Title
	if len(eventTitle) > 30 {
		eventTitle = eventTitle[:27] + "..."
	}
	s.drawText(ctx, eventTitle, x, y, 24, image.Black)
	y += 40

	// Draw event date and time
	eventDate := ticket.Ticket.Event.StartDate.Format("Jan 2, 2006 at 3:04 PM")
	s.drawText(ctx, eventDate, x, y, 18, image.NewUniform(color.RGBA{107, 114, 128, 255})) // Gray color
	y += 30

	// Draw location
	location := ticket.Ticket.Event.Location
	if len(location) > 35 {
		location = location[:32] + "..."
	}
	s.drawText(ctx, location, x, y, 18, image.NewUniform(color.RGBA{107, 114, 128, 255}))
	y += 30

	// Draw ticket number
	s.drawText(ctx, "Ticket #:", x, y, 16, image.NewUniform(color.RGBA{107, 114, 128, 255}))
	s.drawText(ctx, ticket.TicketNumber, x+100, y, 16, image.Black)
	y += 25

	// Draw ticket type/tier if available
	tierName := "Standard"
	if ticket.Ticket != nil && ticket.Ticket.Event != nil && ticket.Ticket.Event.Tiers != nil && len(ticket.Ticket.Event.Tiers) > 0 {
		tierName = ticket.Ticket.Event.Tiers[0].TierName
	}
	s.drawText(ctx, "Tier:", x, y, 16, image.NewUniform(color.RGBA{107, 114, 128, 255}))
	s.drawText(ctx, tierName, x+100, y, 16, image.Black)
	y += 25

	// Draw attendee name
	attendeeName := "Guest"
	if ticket.Ticket.User != nil {
		attendeeName = ticket.Ticket.User.FirstName + " " + ticket.Ticket.User.LastName
	} else if ticket.Ticket.GuestUser != nil {
		attendeeName = ticket.Ticket.GuestUser.FirstName + " " + ticket.Ticket.GuestUser.LastName
	}
	s.drawText(ctx, "Attendee:", x, y, 16, image.NewUniform(color.RGBA{107, 114, 128, 255}))
	s.drawText(ctx, attendeeName, x+100, y, 16, image.Black)

	// Draw instructions at bottom
	y = height - 60
	s.drawText(ctx, "Present this ticket at event entrance", x, y, 14, image.NewUniform(color.RGBA{107, 114, 128, 255}))
}

// drawText draws text on the image
func (s *TicketImageService) drawText(ctx *freetype.Context, text string, x, y int, size float64, color image.Image) error {
	ctx.SetFontSize(size)
	ctx.SetSrc(color)
	_, err := ctx.DrawString(text, freetype.Pt(x, y))
	return err
}

// scaleImage scales an image to the specified dimensions
func (s *TicketImageService) scaleImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// GenerateTicketImageBase64 generates a ticket image and returns it as base64 string
func (s *TicketImageService) GenerateTicketImageBase64(ticket *models.IndividualTicket) (string, error) {
	imageData, err := s.GenerateTicketImage(ticket)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(imageData), nil
}

// GenerateTicketAttachment creates an email attachment for a ticket
func (s *TicketImageService) GenerateTicketAttachment(ticket *models.IndividualTicket) (*models.EmailAttachment, error) {
	imageData, err := s.GenerateTicketImage(ticket)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("ticket-%s.png", strings.ReplaceAll(ticket.TicketNumber, "/", "-"))

	return &models.EmailAttachment{
		Filename:    filename,
		ContentType: "image/png",
		Data:        imageData,
		IsBase64:    false,
	}, nil
}
