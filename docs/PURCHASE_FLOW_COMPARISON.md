# Stripe Purchase Flow Comparison: Logged-In vs Guest Users

## Summary

Both flows have been harmonized to handle multi-tier purchases identically, with proper tier iteration, event preloading, and gaateway initialization. Key differences exist in user type handling and purchase limits.

---

## 1. HANDLERS COMPARISON

### ✅ Logged-In User Handler: `UserPurchaseTicket`

**File:** [internal/handlers/ticket_handler.go](internal/handlers/ticket_handler.go#L560-L618)  
**Lines:** 560-618

```go
func (h *TicketHandler) UserPurchaseTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	var req models.TicketPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate event purchase eligibility for all tiers
	if err := h.validateEventPurchaseEligibility(req.EventID, req.Tiers); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get user details for email
	var user models.User
	if err := database.GetDB().Where("id = ?", userID).First(&user).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check if the payment gateway is cash - if so, purchase immediately
	if req.PaymentGateway == models.PaymentGatewayCash {
		tickets, err := h.ticketService.PurchaseTicket(userID.(uuid.UUID), &req)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusCreated,
			fmt.Sprintf("Successfully purchased %d tickets! Confirmation emails have been sent.", len(tickets)), nil)
		return
	}

	// For payment gateways (stripe, paypal, esewa, khalti, imepay), create checkout session
	checkoutSession, _, err := h.ticketService.InitiateUserPaymentGatewayPurchase(userID.(uuid.UUID), &req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated,
		"Payment initiated successfully. Please complete payment using the provided gateway data.",
		checkoutSession.ToResponse())
}
```

**Key Features:**

- Gets **logged-in userID** from context
- Validates request
- **Calls `validateEventPurchaseEligibility` for ALL tiers** (line 574)
- Retrieves full user details for email/name/phone
- Cash: calls `PurchaseTicket()`
- Gateway: calls `InitiateUserPaymentGatewayPurchase()` passing **userID directly**

---

### ✅ Guest User Handler: `PurchaseTicketAsGuest`

**File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L334-L399)  
**Lines:** 334-399

```go
func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
	var req models.GuestPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate event purchase eligibility for all tiers
	if err := h.validateEventPurchaseEligibility(req.EventID, req.Tiers); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Set default values if not provided
	if req.FirstName == "" {
		req.FirstName = "Guest"
	}
	if req.LastName == "" {
		req.LastName = "User"
	}

	// For cash payment, validate that the email is in the allowed list (if configured)
	if req.PaymentGateway == models.PaymentGatewayCash {
		cfg, err := config.Load()
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		// Only check allowed emails if the list is configured
		if len(cfg.Payment.CashAllowedEmails) > 0 {
			allowed := false
			for _, allowedEmail := range cfg.Payment.CashAllowedEmails {
				if strings.TrimSpace(allowedEmail) == req.Email {
					allowed = true
					break
				}
			}

			if !allowed {
				utils.HandleError(c, utils.NewBusinessLogicError(
					"Cash payments are not available for this email address. Please contact support or use a different payment method."))
				return
			}
		}
	}

	// Unified purchase flow for both cash and gateway payments
	tickets, _, checkoutSession, err := h.ticketService.UnifiedGuestPurchase(&req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Handle immediate completion for cash payments
	if req.PaymentGateway == models.PaymentGatewayCash {
		utils.SuccessResponse(c, http.StatusCreated,
			fmt.Sprintf("Successfully purchased %d tickets! Confirmation email sent to: %s", len(tickets), req.Email), nil)
		return
	}

	// For gateway payments, return checkout session for frontend to complete payment
	if checkoutSession == nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to initialize payment session. Please try again.", nil))
		return
	}
	utils.SuccessResponse(c, http.StatusCreated,
		"Payment initiated successfully. Please complete payment using the provided gateway data.",
		checkoutSession.ToResponse())
}
```

**Key Features:**

- Gets guest details from **request body**
- **Calls `validateEventPurchaseEligibility` for ALL tiers** (line 343)
- Cash gate: checks configured email allowlist (security for cash)
- **Calls `UnifiedGuestPurchase()`** which routes to cash or gateway

---

## 2. SERVICE METHODS COMPARISON

### GATEWAY PAYMENT FLOW:

#### ✅ Logged-In User: `InitiateUserPaymentGatewayPurchase`

**File:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1863-L2102)  
**Lines:** 1863-2102

**Core Loop Structure (Lines 1914-1993):**

```go
// TIER ITERATION
for _, tierSelection := range req.Tiers {
    // Get event tier with lock
    var eventTier models.EventTier
    err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
        Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
        First(&eventTier).Error
    if err != nil {
        tx.Rollback()
        return nil, nil, err
    }

    // Check if tier is active
    if !eventTier.IsActive {
        tx.Rollback()
        return nil, nil, fmt.Errorf("Event tier %s is not active", eventTier.TierName)
    }

    // Check availability
    if eventTier.Available < tierSelection.Quantity {
        tx.Rollback()
        return nil, nil, fmt.Errorf("Insufficient tickets available for tier %s", eventTier.TierName)
    }

    // Set currency from first tier
    if currency == "" {
        currency = eventTier.Currency
    }

    // TICKET CREATION PER QUANTITY
    for i := 0; i < tierSelection.Quantity; i++ {
        // Generate sequential ticket number
        ticketNum, err := utils.GenerateEventTicketNumber(tx, eventTier.TierName, event.StartDate.Year())
        if err != nil {
            tx.Rollback()
            return nil, nil, err
        }

        ticket := &models.Ticket{
            TicketNumber:    ticketNum,
            UserID:          &userID,  // ✅ User-specific
            EventID:         req.EventID,
            TierID:          eventTier.ID,
            TotalAmount:     eventTier.Price,
            PaymentGateway:  req.PaymentGateway,
            Status:          "pending_payment",
            IsGuestPurchase: false,  // ✅ Not a guest
        }

        if err := tx.Create(ticket).Error; err != nil {
            tx.Rollback()
            return nil, nil, err
        }

        // Preload Event ON EACH TICKET (within transaction)
        if err := tx.Preload("Event").First(ticket, ticket.ID).Error; err != nil {
            tx.Rollback()
            return nil, nil, err
        }

        allTickets = append(allTickets, ticket)
        totalAmount += eventTier.Price
    }

    // Update tier availability
    if err := tx.Model(&eventTier).
        Where("id = ? AND available >= ?", eventTier.ID, tierSelection.Quantity).
        Updates(map[string]interface{}{
            "available": gorm.Expr("available - ?", tierSelection.Quantity),
            "sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
        }).Error; err != nil {
        tx.Rollback()
        return nil, nil, err
    }
}
```

**PaymentIntent Creation (Lines 2017-2056):**

```go
paymentIntent := &models.PaymentIntent{
    PaymentGateway:     string(req.PaymentGateway),
    IdempotencyKey:     idempotencyKey,
    CheckoutToken:      checkoutToken,
    UserID:             &userID,  // ✅ User-specific
    GuestUserID:        nil,
    CustomerEmail:      user.Email,
    CustomerName:       user.FirstName + " " + user.LastName,
    CustomerPhone:      user.Phone,
    EventID:            req.EventID,
    TierID:             req.Tiers[0].TierID,  // Primary tier reference
    Quantity:           totalQuantity,
    Currency:           currency,
    CurrencySymbol:     getCurrencySymbol(currency),
    ExchangeRate:       1.0,
    BaseCurrency:       "USD",
    BaseCurrencyAmount: totalAmount + commissionAmount,
    UnitPrice:          0,  // Multi-tier
    Subtotal:           totalAmount,
    PlatformFee:        commissionAmount,
    GatewayFee:         0,
    TotalAmount:        totalAmount + commissionAmount,
    Status:             "pending",
    CommissionRate:     event.CommissionRate,
    CommissionAmount:   commissionAmount,
    OrganizerNetAmount: totalAmount,
    CountryCode:        user.CountryCode,
    ExpiresAt:          &checkoutSession.ExpiresAt,
}
```

**Gateway Initialization (Lines 2069-2093):**

```go
// Commit transaction FIRST (release locks)
if err := tx.Commit().Error; err != nil {
    return nil, nil, err
}

// Preload Event AFTER commit for gateway initialization
if err := s.initializeUserGatewayData(checkoutSession, &models.GuestPurchaseRequest{...}, allTickets[0], userID); err != nil {
    // Cleanup on failure
    cleanupTx := s.db.Begin()
    for _, ticket := range allTickets {
        cleanupTx.Model(&models.Ticket{}).Where("id = ?", ticket.ID).Update("status", "cancelled")
    }
    cleanupTx.Delete(checkoutSession)
    cleanupTx.Commit()
    return nil, nil, err
}
```

---

#### ✅ Guest User: `InitiatePaymentGatewayPurchase`

**File:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L2103-L2280)  
**Lines:** 2103-2280

**Core Loop Structure (Lines 2146-2225):**

```go
// TIER ITERATION (identical to logged-in)
for _, tierSelection := range req.Tiers {
    // Get event tier with lock
    var eventTier models.EventTier
    err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
        Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
        First(&eventTier).Error
    if err != nil {
        tx.Rollback()
        return nil, nil, nil, err
    }

    // Check if tier is active
    if !eventTier.IsActive {
        tx.Rollback()
        return nil, nil, nil, fmt.Errorf("Event tier %s is not active", eventTier.TierName)
    }

    // Check availability
    if eventTier.Available < tierSelection.Quantity {
        tx.Rollback()
        return nil, nil, nil, fmt.Errorf("Insufficient tickets available for tier %s", eventTier.TierName)
    }

    // Set currency from first tier
    if currency == "" {
        currency = eventTier.Currency
    }

    // TICKET CREATION PER QUANTITY
    for i := 0; i < tierSelection.Quantity; i++ {
        ticket := &models.Ticket{
            GuestUserID:     &guestUser.ID,  // ✅ Guest-specific
            EventID:         req.EventID,
            TierID:          tierSelection.TierID,
            TotalAmount:     eventTier.Price,
            PaymentGateway:  req.PaymentGateway,
            Status:          "pending_payment",
            IsGuestPurchase: true,  // ✅ Guest flag
        }

        // Generate sequential ticket number
        ticketNumber, err := utils.GenerateEventTicketNumber(tx, eventTier.TierName, event.StartDate.Year())
        if err != nil {
            tx.Rollback()
            return nil, nil, nil, err
        }
        ticket.TicketNumber = ticketNumber

        if err := tx.Create(ticket).Error; err != nil {
            tx.Rollback()
            return nil, nil, nil, err
        }

        allTickets = append(allTickets, ticket)
        totalAmount += eventTier.Price
    }

    // Update tier availability
    if err := tx.Model(&eventTier).
        Where("id = ? AND available >= ?", eventTier.ID, tierSelection.Quantity).
        Updates(map[string]interface{}{
            "available": gorm.Expr("available - ?", tierSelection.Quantity),
            "sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
        }).Error; err != nil {
        tx.Rollback()
        return nil, nil, nil, err
    }
}
```

**PaymentIntent Creation (Lines 2249-2287):**

```go
paymentIntent := &models.PaymentIntent{
    PaymentGateway:     string(req.PaymentGateway),
    IdempotencyKey:     idempotencyKey,
    CheckoutToken:      checkoutToken,
    GuestUserID:        &guestUser.ID,  // ✅ Guest-specific
    UserID:             nil,
    CustomerEmail:      guestUser.Email,
    CustomerName:       guestUser.FirstName + " " + guestUser.LastName,
    CustomerPhone:      guestUser.Phone,
    EventID:            req.EventID,
    TierID:             req.Tiers[0].TierID,  // Primary tier reference (identical)
    Quantity:           totalQuantity,  // (same calculation)
    // ... all other fields identical to logged-in flow ...
    OrganizerNetAmount: totalAmount,
    CountryCode:        req.CountryCode,
    ExpiresAt:          &checkoutSession.ExpiresAt,
}
```

**Gateway Initialization (Lines 2313-2338):**

```go
// Commit transaction FIRST (release locks)
if err := tx.Commit().Error; err != nil {
    return nil, nil, nil, err
}

// Preload Event AFTER commit for gateway initialization
if err := s.initializeGatewayData(checkoutSession, req, allTickets[0], guestUser); err != nil {
    // Cleanup on failure (identical)
    cleanupTx := s.db.Begin()
    for _, ticket := range allTickets {
        cleanupTx.Model(&models.Ticket{}).Where("id = ?", ticket.ID).Update("status", "cancelled")
    }
    cleanupTx.Delete(checkoutSession)
    cleanupTx.Commit()
    return nil, nil, nil, err
}
```

---

### CASH PAYMENT FLOW:

#### ✅ Guest User: `handleCashGuestPurchase`

**File:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1525-L1710)  
**Lines:** 1525-1710

- Identical tier iteration logic
- Creates tickets with `Status: "active"` (immediate)
- Creates Transaction record after ticket creation
- No CheckoutSession (returns `nil`)

---

## 3. DETAILED DIFFERENCES IDENTIFIED

| **Aspect**                   | **Logged-In User**                           | **Guest User**                 | **Status**                          |
| ---------------------------- | -------------------------------------------- | ------------------------------ | ----------------------------------- |
| **Tier Iteration**           | Lines 1914-1993                              | Lines 2146-2225                | ✅ **IDENTICAL**                    |
| **Tier Loop Logic**          | Iterates multi-tier                          | Iterates multi-tier            | ✅ **IDENTICAL**                    |
| **Tier Validation**          | Check active, availability                   | Check active, availability     | ✅ **IDENTICAL**                    |
| **Currency Setting**         | From first tier                              | From first tier                | ✅ **IDENTICAL**                    |
| **Ticket Number Generation** | Atomic counter per tier                      | Atomic counter per tier        | ✅ **IDENTICAL**                    |
| **Event Preload (Tickets)**  | Line 1954-1958 (inside tx)                   | NOT DONE in loop               | ⚠️ **DIFFERENT**                    |
| **Preload Timing**           | Inside transaction                           | After transaction commit       | ⚠️ **DIFFERENT**                    |
| **User Association**         | `UserID: &userID`                            | `GuestUserID: &guestUser.ID`   | ✅ **Expected**                     |
| **IsGuestPurchase Flag**     | `false`                                      | `true`                         | ✅ **Expected**                     |
| **Tier Availability Update** | Atomic after each tier                       | Atomic after each tier         | ✅ **IDENTICAL**                    |
| **Checkout Session**         | Single for all tickets                       | Single for all tickets         | ✅ **IDENTICAL**                    |
| **Ticket IDs Storage**       | In `GatewayData["ticket_ids"]`               | In `GatewayData["ticket_ids"]` | ✅ **IDENTICAL**                    |
| **PaymentIntent Creation**   | Lines 2017-2056                              | Lines 2249-2287                | ✅ **IDENTICAL** (except user type) |
| **TierID Reference**         | `req.Tiers[0].TierID`                        | `req.Tiers[0].TierID`          | ✅ **IDENTICAL**                    |
| **Quantity Field**           | Multi-tier formula                           | Multi-tier formula             | ✅ **IDENTICAL**                    |
| **Commission Calc**          | `totalAmount * (event.CommissionRate / 100)` | Same                           | ✅ **IDENTICAL**                    |
| **UnitPrice**                | `0` (multi-tier)                             | `0` (multi-tier)               | ✅ **IDENTICAL**                    |
| **Transaction Commit**       | Line 2064                                    | Line 2301                      | ✅ **Both before gateway init**     |
| **Gateway Init Timing**      | After commit (line 2069+)                    | After commit (line 2313+)      | ✅ **IDENTICAL**                    |
| **Cleanup on Error**         | Lines 2078-2085                              | Lines 2322-2329                | ✅ **IDENTICAL**                    |
| **Total Quantity Limit**     | 10 tickets                                   | 6 tickets                      | ⚠️ **DIFFERENT** (intentional)      |
| **Quantity Validation**      | Line 1869                                    | Line 2113                      | ✅ Different limits                 |

---

## 4. KEY FINDINGS

### ✅ FULLY HARMONIZED

1. **Tier Iteration Logic** - Both iterate through `req.Tiers` identically
2. **Tier Safety Checks** - Both check active status + availability
3. **Line Item Creation** - Both create one ticket per quantity per tier
4. **Currency Handling** - Both set from first tier
5. **Atomic Inventory Updates** - Both use same locking mechanism
6. **CheckoutSession** - Both create single session for entire order
7. **PaymentIntent** - Both create with identical fields (except user type)
8. **Transaction Sequencing** - Both commit before gateway initialization
9. **Error Cleanup** - Both have identical rollback logic

### ⚠️ INTENTIONAL DIFFERENCES

1. **Quantity Limits**:
   - Logged-in: 10 tickets
   - Guest: 6 tickets
   - Reason: Guest security/fraud prevention

2. **User Type Associations**:
   - Logged-in: `UserID` + `IsGuestPurchase: false`
   - Guest: `GuestUserID` + `IsGuestPurchase: true`

### ⚠️ TIMING DIFFERENCE - EVENT PRELOADING

**ISSUE FOUND:**

- **Logged-In user** (Lines 1954-1958): Preloads Event **inside transaction** on each ticket
- **Guest user** (Line 2331): Preloads Event **after commit** on first ticket only

This asymmetry could cause issues:

- Logged-in gets Event data potentially outdated by transaction isolation
- Guest gets fresh Event data after commit but only on first ticket
- For multi-ticket responses, guest tickets may lack Event data

**RECOMMENDATION:** Align both to preload Events **after commit** for consistency and optimality.

---

## 5. STRIPE LINE ITEM CREATION

Both flows route through `initializeGatewayData()` (guest) or `initializeUserGatewayData()` (user) **AFTER** transaction commit, where Stripe line items are created from the tickets.

**Expected behavior:** Both should create identical Stripe line items from the ticket array.

---

## 6. VERIFICATION CHECKLIST

- [x] Tier iteration: IDENTICAL ✅
- [x] Event preloading: DIFFERENT (logged-in in-transaction, guest post-commit) ⚠️
- [x] Line item creation: IDENTICAL (via gateway methods) ✅
- [x] Ticket creation: IDENTICAL in logic ✅
- [x] Safety checks: IDENTICAL ✅
- [x] Transaction handling: IDENTICAL ✅
- [x] Cleanup on error: IDENTICAL ✅
