# Guest Ticket Purchase Email Flow Comparison

## Summary

**Both cash and Stripe payment methods ARE sending confirmation emails to guests using the same service.**

---

## 1. CASH GUEST PURCHASE EMAIL FLOW

### Handler Method Location

- **File:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1527)
- **Function:** `handleCashGuestPurchase`
- **Lines:** 1527-1706

### Email Sending Logic

**Location:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1695-L1699)

```go
// Lines 1695-1699: Send confirmation emails for cash payments
if s.emailQueueService != nil {
	if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(guestUser.Email, allTickets); err != nil {
		log.Printf("Failed to queue confirmation email for guest %s: %v", guestUser.Email, err)
	}
}
```

### Sequence for Cash Payments

1. **Create/Find Guest User** (line 1559)
2. **Create Tickets** (lines 1610-1647)
3. **Create Transaction Record** (lines 1649-1667)
4. **Commit Transaction** (line 1678)
5. **Audit Logging** (lines 1680-1688)
6. **Queue Confirmation Emails** → `QueueGuestTicketConfirmationEmail()` (line 1697)
7. **Return** tickets, guest user, nil checkout session (line 1705)

**Key Point:** Emails are queued AFTER transaction is committed successfully ✓

---

## 2. STRIPE PAYMENT SUCCESS EMAIL FLOW

### Handler Method Location

- **File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L443)
- **Function:** `PaymentSuccessCallback` (payment/success endpoint)

### Service Method Location

- **File:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L2524)
- **Function:** `ProcessPaymentSuccess`
- **Lines:** 2524-2664

### Email Sending Logic

**Location:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L2638-L2662)

```go
// Lines 2638-2662: Send confirmation emails
if s.emailQueueService != nil {
	// Group tickets by user type for email sending
	userTickets := make(map[*models.User][]*models.Ticket)
	guestEmails := make(map[string][]*models.Ticket)

	for _, ticket := range allTickets {
		// Reload ticket with associations
		if err := s.db.Preload("User").Preload("GuestUser").Preload("Event").First(ticket, ticket.ID).Error; err != nil {
			continue // Skip if ticket not found
		}

		if ticket.User != nil {
			userTickets[ticket.User] = append(userTickets[ticket.User], ticket)
		} else if ticket.GuestUser != nil {
			guestEmails[ticket.GuestUser.Email] = append(guestEmails[ticket.GuestUser.Email], ticket)
		}
	}

	// Send emails
	for _, tickets := range userTickets {
		// Send order confirmation email (same as cash payments)
		s.sendUserTicketConfirmationEmails(tickets)
	}

	for email, tickets := range guestEmails {
		if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(email, tickets); err != nil {
			log.Printf("Failed to queue confirmation email for guest %s: %v", email, err)
		}
	}
}
```

### Sequence for Stripe Payments

1. **Find Checkout Session** (lines 2531-2536)
2. **Check if Already Processed** (lines 2538-2542)
3. **Check if Expired** (lines 2544-2548)
4. **Update Checkout Session Status** (lines 2550-2555)
5. **Collect All Tickets** (lines 2557-2618)
6. **Record Transaction** (lines 2629-2635)
7. **Commit Transaction** (lines 2637-2639)
8. **Queue Confirmation Emails** (lines 2638-2662):
   - **For Registered Users:** → `sendUserTicketConfirmationEmails()` (line 2654)
   - **For Guest Users:** → `QueueGuestTicketConfirmationEmail()` (line 2659)
9. **Return** nil (line 2664)

**Key Point:** Emails are queued AFTER transaction is committed successfully ✓

---

## 3. GUEST EMAIL QUEUING SERVICE

### Location

- **File:** [internal/services/email_queue_service.go](internal/services/email_queue_service.go#L290)
- **Function:** `QueueGuestTicketConfirmationEmail`
- **Lines:** 290-327

### Implementation

```go
// Lines 290-327
func (s *EmailQueueService) QueueGuestTicketConfirmationEmail(guestEmail string, tickets []*models.Ticket) error {
	for _, ticket := range tickets {
		// Generate secure QR code
		qrCodeBase64, err := s.secureQRService.GenerateSecureQR(ticket, ticket.Event)
		if err != nil {
			return utils.NewInternalServerError(fmt.Sprintf("Failed to generate secure QR code for ticket %s.", ticket.TicketNumber), err)
		}

		ticketData := map[string]interface{}{
			"Title":         "Your Event Ticket",
			"Message":       "Here is your event ticket. The QR code is unique and should be presented at the event entrance.",
			"RecipientName": "Valued Guest",
			"EventTitle":    ticket.Event.Title,
			"EventDate":     ticket.Event.StartDate.Format("January 2, 2006 at 3:04 PM"),
			"EventLocation": ticket.Event.Location,
			"TicketNumber":  ticket.TicketNumber,
			"QRCode":        qrCodeBase64,
			"TicketURL":     fmt.Sprintf("%s/ticket/%s", s.config.URLs.UserBaseURL, ticket.TicketNumber),
		}

		emailJob := &models.EmailJob{
			Type:         models.EmailTypeTicketConfirmation,
			To:           guestEmail,
			Subject:      fmt.Sprintf("Your Ticket - %s", ticket.Event.Title),
			TemplateFile: "guest_ticket.html",
			TemplateData: ticketData,
			Priority:     models.PriorityHigh,
			MaxRetries:   3,
		}

		emailJob.SetDefaults()

		if err := s.queueEmailJob(emailJob); err != nil {
			return err
		}
	}

	return nil
}
```

### What This Method Does

1. **Iterates through each ticket** provided
2. **Generates a secure QR code** for each ticket
3. **Builds email template data** with:
   - Event title, date, location
   - Ticket number
   - QR code (base64 encoded)
   - Ticket view URL
4. **Creates EmailJob record** with:
   - Type: `EmailTypeTicketConfirmation`
   - Template: `guest_ticket.html`
   - Priority: `PriorityHigh`
   - Max retries: 3
5. **Queues the email job** for async processing

---

## COMPARISON TABLE

| Aspect                          | Cash Payment                                           | Stripe Payment                                         | Difference         |
| ------------------------------- | ------------------------------------------------------ | ------------------------------------------------------ | ------------------ |
| **Handler**                     | `handleCashGuestPurchase`                              | `PaymentSuccessCallback` / `ProcessPaymentSuccess`     | Different handlers |
| **Email Service**               | `QueueGuestTicketConfirmationEmail()`                  | `QueueGuestTicketConfirmationEmail()`                  | **SAME SERVICE** ✓ |
| **Email Queuing Line (Cash)**   | [Line 1697](internal/services/ticket_service.go#L1697) | N/A                                                    | Cash only          |
| **Email Queuing Line (Stripe)** | N/A                                                    | [Line 2659](internal/services/ticket_service.go#L2659) | Stripe only        |
| **When Emails Sent**            | After tx commit                                        | After tx commit                                        | **SAME TIMING** ✓  |
| **Guest User Identification**   | From `guestUser.Email`                                 | From `ticket.GuestUser.Email`                          | Same result        |
| **Tickets Passed**              | `allTickets` (created)                                 | `allTickets` (collected)                               | Both ticket lists  |
| **Email Template**              | `guest_ticket.html`                                    | `guest_ticket.html`                                    | **IDENTICAL** ✓    |

---

## KEY FINDINGS

### ✅ CONFIRMED: Both Methods Send Emails

- **Cash:** Email queued at line 1697
- **Stripe:** Email queued at line 2659
- **Same service:** `QueueGuestTicketConfirmationEmail()`

### ✅ IDENTICAL EMAIL LOGIC

- Both use the same template: `guest_ticket.html`
- Both include QR codes, ticket numbers, event details
- Both have `MaxRetries: 3` and `Priority: High`

### ✅ PROPER TRANSACTION HANDLING

- Cash: Emails queued AFTER transaction committed (line 1678 → line 1697)
- Stripe: Emails queued AFTER transaction committed (line 2637 → line 2659)
- No race conditions in email queuing

### ⚠️ POTENTIAL ISSUE AREAS TO CHECK

1. **Email Queue Processing:**
   - Are the queued emails actually being processed by the worker?
   - Check [internal/workers/](internal/workers/) for email worker implementation
2. **Email Service Initialization:**
   - Verify `s.emailQueueService != nil` check passes during runtime
   - Both lines 1696 and 2638 have nil checks

3. **Database Preloading:**
   - Stripe version preloads associations (line 2643-2647)
   - Cash version uses populated `allTickets` from earlier in function
   - Ensure Event data is available when QueueGuestTicketConfirmationEmail runs

4. **Error Logging Only:**
   - Email failures are only logged, not propagated (lines 1697-1699, 2660-2662)
   - Failed emails won't cause transaction to fail or alert user

---

## CONCLUSION

**The email flows are functionally identical for both payment methods.** If guests are not receiving Stripe payment confirmation emails but are receiving cash payment emails, the issue is likely not in the email queuing logic itself, but rather in:

1. ✓ The ticket/event data preloading in `ProcessPaymentSuccess`
2. ✓ The email queue worker not processing Stripe payment emails
3. ✓ The email service configuration or credentials
4. ✓ Email provider (SMTP) issues
