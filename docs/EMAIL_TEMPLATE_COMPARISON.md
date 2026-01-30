# Email Template Comparison - Guest vs User

## ✅ Templates Are Now Identical

Both `guest_order_confirmation.html` and `order_confirmation.html` now use the **same structure and field names**.

---

## Field Names (All Lowercase with Underscores)

### Email Data Structure

```go
emailData := map[string]interface{}{
    // Recipient info
    "guest_name": "...",        // For guests
    "user_name": "...",         // For users
    "guest_email": "...",
    "user_email": "...",

    // Event info
    "event_name": "...",
    "event_date": "...",
    "event_time": "...",
    "venue": "...",
    "organizer_name": "...",

    // Tickets
    "tickets": [
        {
            "ticket_number": "VIP-2026-0001",
            "view_url": "https://..."
        }
    ],
    "total_tickets": 2,
    "total_amount": 199.99,

    // Calendar
    "calendar_ics_url": "...",
    "google_calendar_url": "...",
    "calendar_filename": "...",

    // Other
    "base_url": "https://user.timroticket.com",
    "year": 2026
}
```

---

## Template Comparison

### Guest Template Header

```html
<div class="header">
  <h1>🎫 Your Tickets Are Ready!</h1>
  <p>Dear {{.guest_name}},</p>
</div>
```

### User Template Header

```html
<div class="header">
  <h1>🎫 Your Tickets Are Ready!</h1>
  <p>Thank you for your purchase, {{.user_name}}</p>
</div>
```

**Difference:** Only the greeting text and field name (`guest_name` vs `user_name`)

---

## Shared Sections (100% Identical)

### Event Details

```html
<div class="event-details">
  <h2>{{.event_name}}</h2>
  <p><strong>Date:</strong> {{.event_date}}</p>
  <p><strong>Time:</strong> {{.event_time}}</p>
  <p><strong>Venue:</strong> {{.venue}}</p>
  <p><strong>Organizer:</strong> {{.organizer_name}}</p>
</div>
```

### Order Summary

```html
<div class="total-summary">
  <h3>Order Summary</h3>
  <p><strong>Total Tickets:</strong> {{.total_tickets}}</p>
  <p class="total-amount">Total Amount: ${{printf "%.2f" .total_amount}}</p>
</div>
```

### Calendar Section

```html
<div class="calendar-section">
  <h3>📅 Add to Your Calendar</h3>
  <p>
    Never miss this event! Add it to your calendar and get automatic reminders.
  </p>
  <div class="button-group">
    <a
      href="{{.calendar_ics_url}}"
      class="calendar-btn"
      download="{{.calendar_filename}}"
    >
      📥 Add to Calendar (iOS/Android/Outlook)
    </a>
    <a href="{{.google_calendar_url}}" class="calendar-btn" target="_blank">
      📆 Add to Google Calendar
    </a>
  </div>
</div>
```

### Ticket List

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

### Important Note

```html
<div class="important-note">
  <h4>⚠️ Important Information</h4>
  <ul>
    <li>Keep this email safe - it contains links to access your tickets</li>
    <li>Each ticket link is secure and can only be accessed by you</li>
    <li>Present the QR code on your ticket at the event entrance</li>
    <li>Tickets are valid for one-time use only</li>
    <li>Arrive at the venue at least 30 minutes before the event starts</li>
  </ul>
</div>
```

### Footer

```html
<div class="footer">
  <p>Thank you for choosing Timro Tickets!</p>
  <p>If you have any questions, please contact our support team.</p>
  <p>&copy; {{.year}} Timro Tickets. All rights reserved.</p>
</div>
```

---

## CSS Styles (100% Identical)

Both templates use the exact same CSS classes:

- `.container`
- `.header`
- `.event-details`
- `.total-summary`
- `.calendar-section`
- `.ticket-list`
- `.ticket-item`
- `.ticket-number`
- `.view-ticket-btn`
- `.important-note`
- `.footer`

---

## What Was Fixed

### Before (Guest Template)

```html
<h2>{{.EventName}}</h2>
❌ Uppercase
<p>{{.EventDate}}</p>
❌ Uppercase
<p>{{.Venue}}</p>
❌ Uppercase
<p>{{.CalendarICSURL}}</p>
❌ Uppercase
<p>&copy; {{.CurrentYear}}</p>
❌ Wrong field name
<p>Dear Guest,</p>
❌ Not personalized
```

### After (Guest Template)

```html
<h2>{{.event_name}}</h2>
✅ Lowercase
<p>{{.event_date}}</p>
✅ Lowercase
<p>{{.venue}}</p>
✅ Lowercase
<p>{{.calendar_ics_url}}</p>
✅ Lowercase
<p>&copy; {{.year}}</p>
✅ Correct field
<p>Dear {{.guest_name}},</p>
✅ Personalized
```

---

## Email Flows Using These Templates

### 1. Guest Payment Gateway Flow

- **Function:** `sendPaymentSuccessEmails()` in `ticket_service.go`
- **Template:** `guest_order_confirmation.html`
- **Trigger:** After successful Stripe/PayPal/eSewa payment

### 2. Guest Cash Payment Flow

- **Function:** `prepareGuestOrderConfirmationData()` in `public_handler.go`
- **Template:** `guest_order_confirmation.html`
- **Trigger:** Immediate after cash purchase

### 3. User Logged-In Purchase Flow

- **Function:** `sendUserTicketConfirmationEmails()` in `ticket_service.go`
- **Template:** `order_confirmation.html`
- **Trigger:** After successful ticket purchase

---

## Benefits of Unified Structure

✅ **Consistency:** Same look and feel for all users  
✅ **Maintainability:** Fix in one place, applies to both  
✅ **Field names:** All lowercase with underscores  
✅ **No missing data:** All required fields present  
✅ **Personalization:** Uses actual names, not "Guest"

---

## Testing Checklist

- [ ] Guest cash payment → Email with correct name, event details, calendar buttons
- [ ] Guest gateway payment → Email with correct name, event details, calendar buttons
- [ ] User purchase → Email with correct name, event details, calendar buttons
- [ ] All ticket URLs work → Click "View & Download Ticket"
- [ ] Calendar downloads work → .ics file downloads
- [ ] Google Calendar link works → Opens Google Calendar
- [ ] Year in footer is correct → Shows 2026
- [ ] Multiple tickets → Each has own "View & Download" button
