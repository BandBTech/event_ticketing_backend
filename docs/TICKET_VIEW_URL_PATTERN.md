# Ticket View URL Pattern

## Complete URL Format

```
{base_url}/tickets/view?token={jwt_token}
```

**Example:**

```
https://user.timroticket.com/tickets/view?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

---

## Architecture Overview

### 1. **Centralized URL Generation**

**Location:** `internal/services/ticket_service.go`

```go
// generateTicketViewURL generates a secure JWT-based view URL for a ticket
// Format: {base_url}/tickets/view?token={jwt_token}
// This is a centralized function to ensure consistency across guest and user flows
func (s *TicketService) generateTicketViewURL(ticket *models.Ticket) (string, error) {
	jwtService := utils.NewJWTService(s.jwtConfig)
	token, err := jwtService.GenerateTicketAccessToken(ticket)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT token: %w", err)
	}
	return fmt.Sprintf("%s/tickets/view?token=%s", s.getBaseURL(), token), nil
}
```

**Base URL Configuration:**

```go
func (s *TicketService) getBaseURL() string {
	return "https://user.timroticket.com"
}
```

---

### 2. **Email Integration**

Both guest and user emails use the **same pattern** with individual ticket URLs:

#### Guest Email Flow

**Location:** `internal/services/ticket_service.go` - `sendPaymentSuccessEmails()`

```go
// Generate ticket view URLs for each ticket
var ticketData []map[string]interface{}

for _, ticket := range tickets {
	// Generate secure view URL using centralized helper
	viewURL, err := s.generateTicketViewURL(&ticket)
	if err != nil {
		log.Printf("Failed to generate ticket view URL for ticket %s: %v", ticket.ID, err)
		continue
	}

	// Prepare ticket data for email template
	ticketData = append(ticketData, map[string]interface{}{
		"ticket_number": ticket.TicketNumber,
		"view_url":      viewURL,
	})
}
```

#### User Email Flow

**Location:** `internal/services/ticket_service.go` - `sendUserTicketConfirmationEmails()`

```go
// Generate ticket view URLs for each ticket
var ticketData []map[string]interface{}

for _, ticketPtr := range tickets {
	ticket := *ticketPtr // Dereference the pointer
	// Generate secure view URL using centralized helper
	viewURL, err := s.generateTicketViewURL(&ticket)
	if err != nil {
		log.Printf("Failed to generate ticket view URL for ticket %s: %v", ticket.ID, err)
		continue
	}

	// Prepare ticket data for email template
	ticketData = append(ticketData, map[string]interface{}{
		"ticket_number": ticket.TicketNumber,
		"view_url":      viewURL,
	})
}
```

#### Cash Payment Email Flow

**Location:** `internal/handlers/public_handler.go` - `prepareGuestOrderConfirmationData()`

```go
// Generate ticket data for email template
var ticketData []map[string]interface{}
for _, ticket := range tickets {
	// Generate secure view URL using JWT token
	// Format: {base_url}/tickets/view?token={jwt_token}
	jwtService := utils.NewJWTService(&h.config.JWT)
	token, err := jwtService.GenerateTicketAccessToken(ticket)
	if err != nil {
		log.Printf("Failed to generate JWT token for ticket %s: %v", ticket.ID, err)
		continue
	}

	viewURL := fmt.Sprintf("%s/tickets/view?token=%s", h.config.URLs.UserBaseURL, token)
	ticketData = append(ticketData, map[string]interface{}{
		"ticket_number": ticket.TicketNumber,
		"view_url":      viewURL,
	})
}
```

---

### 3. **Email Template Structure**

Both templates use **identical HTML structure**:

**Location:** `internal/templates/email/guest_order_confirmation.html` and `order_confirmation.html`

```html
<div class="ticket-list">
  <h3>Your Tickets</h3>
  {{range .tickets}}
  <div class="ticket-item">
    <div class="ticket-number">Ticket #{{.ticket_number}}</div>
    <p>
      Click the button below to view and download your ticket. Each ticket has a
      unique QR code for event entry.
    </p>
    <a href="{{.view_url}}" class="view-ticket-btn">View & Download Ticket</a>
  </div>
  {{end}}
</div>
```

---

### 4. **API Endpoint Handler**

**Location:** `internal/handlers/public_handler.go` - `ViewTicket()`

**Route:** `GET /api/v1/public/tickets/view?token={jwt_token}`

```go
func (h *PublicHandler) ViewTicket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get all tickets for this order (same event, same user/guest)
	var tickets []models.Ticket
	query := h.db.Preload("Event").Preload("Event.Tiers").Preload("Event.Organizer")

	if claims.UserID != nil {
		query = query.Where("user_id = ? AND event_id = ?", *claims.UserID, claims.EventID)
	} else if claims.GuestUserID != nil {
		query = query.Where("guest_user_id = ? AND event_id = ?", *claims.GuestUserID, claims.EventID)
	}

	// ... rest of handler
}
```

---

## Key Features

### ✅ **Unified Pattern**

- Same URL format for both guest and user tickets
- Centralized helper function: `generateTicketViewURL()`
- Consistent email data structure

### ✅ **Security**

- JWT-based authentication
- Token contains: `ticket_id`, `event_id`, `user_id/guest_user_id`
- Tokens generated with same JWT service configuration

### ✅ **Email Data Structure**

```go
emailData := {
  "tickets": [
    {
      "ticket_number": "VIP-2026-0001",
      "view_url": "https://user.timroticket.com/tickets/view?token=eyJ..."
    },
    {
      "ticket_number": "VIP-2026-0002",
      "view_url": "https://user.timroticket.com/tickets/view?token=eyK..."
    }
  ],
  // ... other fields
}
```

---

## How to Modify in Future

### 1. **Change Base URL**

Update one place:

```go
// internal/services/ticket_service.go
func (s *TicketService) getBaseURL() string {
	return "https://your-new-domain.com" // Change here
}
```

Also update:

```go
// For cash payment handler
h.config.URLs.UserBaseURL // In config
```

### 2. **Change URL Format**

Update one place:

```go
// internal/services/ticket_service.go
func (s *TicketService) generateTicketViewURL(ticket *models.Ticket) (string, error) {
	jwtService := utils.NewJWTService(s.jwtConfig)
	token, err := jwtService.GenerateTicketAccessToken(ticket)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT token: %w", err)
	}
	// Change format here:
	return fmt.Sprintf("%s/view-ticket/%s", s.getBaseURL(), token), nil
}
```

### 3. **Add Query Parameters**

```go
return fmt.Sprintf("%s/tickets/view?token=%s&ref=email&utm_source=notification",
                   s.getBaseURL(), token), nil
```

### 4. **Change JWT Claims**

Update:

- `pkg/utils/jwt.go` - `GenerateTicketAccessToken()`
- `pkg/utils/jwt.go` - `ValidateTicketAccessToken()`

---

## Testing Checklist

- [ ] Guest cash payment → Email received → Click "View & Download Ticket"
- [ ] Guest gateway payment → Email received → Click "View & Download Ticket"
- [ ] User logged-in purchase → Email received → Click "View & Download Ticket"
- [ ] Multiple tickets in one order → Each ticket has unique URL
- [ ] JWT token validation works
- [ ] Expired events cannot be viewed
- [ ] Invalid tokens show error

---

## Related Files

```
internal/
├── services/
│   └── ticket_service.go
│       ├── generateTicketViewURL()        ← Centralized helper
│       ├── sendPaymentSuccessEmails()     ← Guest gateway
│       └── sendUserTicketConfirmationEmails() ← User purchase
├── handlers/
│   └── public_handler.go
│       ├── prepareGuestOrderConfirmationData() ← Guest cash
│       └── ViewTicket()                   ← API endpoint
├── templates/
│   └── email/
│       ├── guest_order_confirmation.html  ← Guest template
│       └── order_confirmation.html        ← User template
└── routes/
    └── routes.go
        └── GET /api/v1/public/tickets/view

pkg/utils/
└── jwt.go
    ├── GenerateTicketAccessToken()
    └── ValidateTicketAccessToken()
```

---

## Summary

**Complete URL:** `https://user.timroticket.com/tickets/view?token={jwt_token}`

**Pattern:** Both guest and user flows use identical code structure with centralized URL generation.

**To modify:** Update `generateTicketViewURL()` function in one place.
