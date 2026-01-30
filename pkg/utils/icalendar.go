package utils

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ICalendarEvent represents an event for iCalendar format
type ICalendarEvent struct {
	UID         string
	Summary     string
	Description string
	Location    string
	StartTime   time.Time
	EndTime     time.Time
	Organizer   string
	URL         string
}

// GenerateICS generates an iCalendar (.ics) file content
// This format is universally supported: iOS, Android, Windows, Mac, Linux
func GenerateICS(event ICalendarEvent) string {
	// Format times in UTC (iCalendar format: YYYYMMDDTHHMMSSZ)
	startUTC := event.StartTime.UTC().Format("20060102T150405Z")
	endUTC := event.EndTime.UTC().Format("20060102T150405Z")
	nowUTC := time.Now().UTC().Format("20060102T150405Z")

	// Generate unique UID if not provided
	if event.UID == "" {
		event.UID = uuid.New().String()
	}

	// Escape special characters in text fields
	summary := escapeICSText(event.Summary)
	description := escapeICSText(event.Description)
	location := escapeICSText(event.Location)

	// Build iCalendar content
	ics := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Timro Tickets//Event Ticketing System//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"X-WR-CALNAME:" + summary,
		"X-WR-TIMEZONE:UTC",
		"BEGIN:VEVENT",
		"UID:" + event.UID,
		"DTSTAMP:" + nowUTC,
		"DTSTART:" + startUTC,
		"DTEND:" + endUTC,
		"SUMMARY:" + summary,
		"DESCRIPTION:" + description,
		"LOCATION:" + location,
	}

	// Add organizer if provided
	if event.Organizer != "" {
		ics = append(ics, "ORGANIZER:CN="+escapeICSText(event.Organizer))
	}

	// Add URL if provided
	if event.URL != "" {
		ics = append(ics, "URL:"+event.URL)
	}

	// Add smart reminders based on how much time until event
	ics = append(ics, generateSmartReminders(event.StartTime, summary)...)

	ics = append(ics,
		"STATUS:CONFIRMED",
		"SEQUENCE:0",
		"END:VEVENT",
		"END:VCALENDAR",
	)

	// Join with CRLF (required by iCalendar spec)
	return strings.Join(ics, "\r\n")
}

// escapeICSText escapes special characters for iCalendar text fields
func escapeICSText(text string) string {
	// Escape backslashes first
	text = strings.ReplaceAll(text, "\\", "\\\\")
	// Escape semicolons
	text = strings.ReplaceAll(text, ";", "\\;")
	// Escape commas
	text = strings.ReplaceAll(text, ",", "\\,")
	// Escape newlines
	text = strings.ReplaceAll(text, "\n", "\\n")
	// Escape carriage returns
	text = strings.ReplaceAll(text, "\r", "")

	// Limit length to avoid issues (iCalendar recommends line folding at 75 chars)
	if len(text) > 500 {
		text = text[:497] + "..."
	}

	return text
}

// generateSmartReminders creates reminders based on time until event
// Ensures users always get at least one reminder, even for last-minute purchases
func generateSmartReminders(eventStart time.Time, summary string) []string {
	now := time.Now()
	timeUntilEvent := eventStart.Sub(now)

	var reminders []string

	// If event is more than 24 hours away: add both 24h and 2h reminders
	if timeUntilEvent > 24*time.Hour {
		reminders = append(reminders,
			"BEGIN:VALARM",
			"TRIGGER:-PT24H",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 24 hours",
			"END:VALARM",
			"BEGIN:VALARM",
			"TRIGGER:-PT2H",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 2 hours",
			"END:VALARM",
		)
	} else if timeUntilEvent > 2*time.Hour {
		// If event is 2-24 hours away: add 2h reminder only
		reminders = append(reminders,
			"BEGIN:VALARM",
			"TRIGGER:-PT2H",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 2 hours",
			"END:VALARM",
		)
	} else if timeUntilEvent > 30*time.Minute {
		// If event is 30min-2h away: add 30min reminder
		reminders = append(reminders,
			"BEGIN:VALARM",
			"TRIGGER:-PT30M",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 30 minutes",
			"END:VALARM",
		)
	} else if timeUntilEvent > 15*time.Minute {
		// If event is 15-30min away: add 15min reminder
		reminders = append(reminders,
			"BEGIN:VALARM",
			"TRIGGER:-PT15M",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 15 minutes",
			"END:VALARM",
		)
	} else if timeUntilEvent > 0 {
		// If event is less than 15min away but hasn't started: add 5min reminder
		reminders = append(reminders,
			"BEGIN:VALARM",
			"TRIGGER:-PT5M",
			"ACTION:DISPLAY",
			"DESCRIPTION:Reminder: "+summary+" starts in 5 minutes",
			"END:VALARM",
		)
	}
	// If event already started or passed, no reminders

	return reminders
}

// GenerateAddToCalendarURL generates a data URL for the .ics file
// This can be embedded directly in emails as a download link
func GenerateAddToCalendarURL(icsContent string) string {
	// Base64 encode for data URL (optional, but cleaner)
	// For simplicity, we'll use direct text encoding
	return "data:text/calendar;charset=utf8," + strings.ReplaceAll(icsContent, "\r\n", "%0D%0A")
}

// GenerateGoogleCalendarURL generates a Google Calendar URL
// Alternative method for Google Calendar users
func GenerateGoogleCalendarURL(event ICalendarEvent) string {
	baseURL := "https://calendar.google.com/calendar/render?action=TEMPLATE"

	// Format dates for Google (YYYYMMDDTHHMMSSZ)
	startUTC := event.StartTime.UTC().Format("20060102T150405Z")
	endUTC := event.EndTime.UTC().Format("20060102T150405Z")

	params := []string{
		"text=" + urlEncode(event.Summary),
		"dates=" + startUTC + "/" + endUTC,
		"details=" + urlEncode(event.Description),
		"location=" + urlEncode(event.Location),
	}

	if event.URL != "" {
		params = append(params, "url="+urlEncode(event.URL))
	}

	return baseURL + "&" + strings.Join(params, "&")
}

// urlEncode performs basic URL encoding
func urlEncode(s string) string {
	s = strings.ReplaceAll(s, " ", "+")
	s = strings.ReplaceAll(s, "\n", "%0A")
	s = strings.ReplaceAll(s, "&", "%26")
	s = strings.ReplaceAll(s, "=", "%3D")
	return s
}

// GetCalendarFilename generates a filename for the .ics file
func GetCalendarFilename(eventTitle string) string {
	// Sanitize filename
	filename := strings.ToLower(eventTitle)
	filename = strings.ReplaceAll(filename, " ", "-")
	// Remove special characters
	var sanitized strings.Builder
	for _, r := range filename {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sanitized.WriteRune(r)
		}
	}
	result := sanitized.String()
	if len(result) > 50 {
		result = result[:50]
	}
	if result == "" {
		result = "event"
	}
	return result + ".ics"
}

// FormatEventDescription creates a formatted description for calendar
func FormatEventDescription(eventTitle, ticketNumber, tierName string, quantity int) string {
	return fmt.Sprintf(
		"Event: %s\\n\\nTicket Details:\\nTicket Number: %s\\nTier: %s\\nQuantity: %d\\n\\n"+
			"Important: Please bring your ticket QR code for entry.\\n\\n"+
			"Powered by Timro Tickets",
		eventTitle, ticketNumber, tierName, quantity,
	)
}
