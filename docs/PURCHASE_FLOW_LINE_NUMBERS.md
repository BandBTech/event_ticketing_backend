# Purchase Flow Quick Reference - Line Numbers

## HANDLERS

| Component                  | Logged-In User       | Guest User              | Status                       |
| -------------------------- | -------------------- | ----------------------- | ---------------------------- |
| **Handler Function**       | UserPurchaseTicket   | PurchaseTicketAsGuest   | ✅                           |
| **File**                   | ticket_handler.go    | public_handler.go       | -                            |
| **Lines**                  | 560-618              | 334-399                 | -                            |
| **Eligibility Validation** | Line 574             | Line 343                | ✅ Both validate all tiers   |
| **User Lookup**            | Line 580 (DB lookup) | Line 353-362 (defaults) | -                            |
| **Cash Check**             | Line 591             | Line 358                | ✅ Both exist                |
| **Gateway Service Call**   | Line 600             | Line 380                | ✅ Different service methods |

---

## SERVICE METHODS - GATEWAY PAYMENT

### Logged-In Service: InitiateUserPaymentGatewayPurchase

| Step                    | Lines     | Detail                                        |
| ----------------------- | --------- | --------------------------------------------- |
| Quantity validation     | 1869      | Check total ≤ 10                              |
| Tier locking            | 1873-1878 | Lock all tiers                                |
| Get event               | 1909      | Event for ticket number generation            |
| **TIER LOOP START**     | 1914      | `for _, tierSelection := range req.Tiers`     |
| Get tier with lock      | 1917-1920 | `Clauses(Locking{...})`                       |
| Check tier active       | 1923-1926 | Validation                                    |
| Check availability      | 1929-1932 | Quantity check                                |
| Set currency            | 1935-1937 | From first tier                               |
| **INNER QUANTITY LOOP** | 1941      | `for i := 0; i < tierSelection.Quantity; i++` |
| Generate ticket number  | 1943-1947 | Atomic sequence                               |
| Create ticket struct    | 1949-1956 | UserID: &userID, IsGuestPurchase: false       |
| **Preload Event**       | 1954-1958 | **INSIDE TRANSACTION** ⚠️                     |
| Append ticket           | 1962      | allTickets array                              |
| Add to total            | 1963      | totalAmount +=                                |
| **TIER LOOP END**       | -         | -                                             |
| Update inventory        | 1966-1973 | Atomic update: available--, sold++            |
| Create checkout session | 1990-1998 | Single session, store all ticket_ids          |
| Create PaymentIntent    | 2002-2056 | Full tracking record                          |
| Commit transaction      | 2060-2062 | Release locks                                 |
| Initialize gateway      | 2069+     | **AFTER COMMIT**                              |

### Guest Service: InitiatePaymentGatewayPurchase

| Step                    | Lines     | Detail                                                        |
| ----------------------- | --------- | ------------------------------------------------------------- |
| Quantity validation     | 2113      | Check total ≤ 6 **[DIFFERENT]**                               |
| Create/find guest       | 2127-2130 | createOrFindGuestUser                                         |
| Tier locking            | 2121-2126 | Lock all tiers                                                |
| Get event               | 2140      | Event for ticket number generation                            |
| **TIER LOOP START**     | 2146      | `for _, tierSelection := range req.Tiers`                     |
| Get tier with lock      | 2149-2152 | `Clauses(Locking{...})` **[IDENTICAL PATTERN]**               |
| Check tier active       | 2155-2158 | Validation **[IDENTICAL]**                                    |
| Check availability      | 2161-2164 | Quantity check **[IDENTICAL]**                                |
| Set currency            | 2167-2169 | From first tier **[IDENTICAL]**                               |
| **INNER QUANTITY LOOP** | 2173      | `for i := 0; i < tierSelection.Quantity; i++` **[IDENTICAL]** |
| Create ticket struct    | 2175-2181 | GuestUserID: &guestUser.ID, IsGuestPurchase: true             |
| Generate ticket number  | 2183-2188 | Atomic sequence **[IDENTICAL]**                               |
| Create ticket in DB     | 2190-2193 | **NO PRELOAD HERE** ⚠️                                        |
| Append ticket           | 2196      | allTickets array                                              |
| Add to total            | 2197      | totalAmount +=                                                |
| **TIER LOOP END**       | -         | -                                                             |
| Update inventory        | 2200-2207 | Atomic update **[IDENTICAL PATTERN]**                         |
| Create checkout session | 2224-2232 | Single session, store all ticket_ids **[IDENTICAL]**          |
| Create PaymentIntent    | 2237-2287 | Full tracking record **[IDENTICAL FIELDS]**                   |
| Commit transaction      | 2301-2303 | Release locks **[IDENTICAL]**                                 |
| Preload Event           | 2331      | **AFTER COMMIT** on allTickets[0] ⚠️                          |
| Initialize gateway      | 2313+     | **AFTER COMMIT**                                              |

---

## COMPARISON: Core Tier Handling

### Loop Comparison

```
LOGGED-IN (1914-1973)          GUEST (2146-2207)           DIFFERENCE
────────────────────          ─────────────────           ──────────
for _, tier := range           for _, tier := range       ✅ IDENTICAL
    get tier + lock                get tier + lock        ✅ IDENTICAL
    validate active               validate active        ✅ IDENTICAL
    check quantity                check quantity         ✅ IDENTICAL
    set currency                  set currency           ✅ IDENTICAL
    for i < quantity              for i < quantity       ✅ IDENTICAL
        gen ticket#                   gen ticket#        ✅ IDENTICAL
        create ticket               create ticket        ✅ IDENTICAL
        preload Event[*]            [NO PRELOAD]         ⚠️ DIFFERENT
        add to array                add to array         ✅ IDENTICAL
        add to total                add to total         ✅ IDENTICAL
    end for                         end for
    update inventory              update inventory       ✅ IDENTICAL
end for                         end for
```

---

## ASYMMETRY ALERT 🔴

### Event Preloading Timing

- **Logged-In User** (Line 1954-1958):
  ```go
  if err := tx.Preload("Event").First(ticket, ticket.ID).Error; err != nil {
      tx.Rollback()
      return nil, nil, err
  }
  ```
  **Within Transaction** - Called inside the loop for EVERY ticket
- **Guest User** (Line 2331):
  ```go
  if err := s.db.Preload("Event").First(&allTickets[0], allTickets[0].ID).Error; err != nil {
      return nil, nil, nil, fmt.Errorf("failed to preload event for gateway data: %w", err)
  }
  ```
  **After Transaction Commit** - Called only for FIRST ticket

**Impact:**

- Logged-in: Event data potentially isolated/stale during transaction
- Guest: Event data fresh but only preloaded for first ticket
- **Risk**: Multi-ticket guest responses may lack Event data association

**Recommendation**: Move logged-in preload to AFTER commit for consistency

---

## PaymentIntent Field Comparison

### Identical Fields (Both Use Same Values)

```
Field                          Logged-In                    Guest
────────────────────────────   ─────────────────           ──────────
PaymentGateway                 ✅ string(req.PaymentGateway)
IdempotencyKey                 ✅ fmt.Sprintf("payment_...")
CheckoutToken                  ✅ s.generateSecureToken()
EventID                        ✅ req.EventID
TierID                         ✅ req.Tiers[0].TierID
Quantity                       ✅ totalQuantity (sum of all)
Currency                       ✅ From first tier
UnitPrice                      ✅ 0 (multi-tier flag)
Subtotal                       ✅ totalAmount
PlatformFee                    ✅ totalAmount * commissionRate / 100
TotalAmount                    ✅ totalAmount + commissionAmount
Status                         ✅ "pending"
CommissionRate                 ✅ event.CommissionRate
CommissionAmount               ✅ totalAmount * commissionRate / 100
```

### Different Fields (Expected)

```
Field                          Logged-In                    Guest
────────────────────────────   ─────────────────           ──────────
UserID                         ✅ &userID                  ❌ nil
GuestUserID                    ❌ nil                      ✅ &guestUser.ID
CustomerEmail                  ✅ user.Email                  guestUser.Email
CustomerName                   ✅ user.FirstName + ...        guestUser.FirstName + ...
CustomerPhone                  ✅ user.Phone                  guestUser.Phone
CountryCode                    ✅ user.CountryCode            req.CountryCode
```

---

## Stripe Line Items

Both flows gateway initialization functions are called AFTER transaction commit:

- Logged-in: `initializeUserGatewayData()` (Line 2069+)
- Guest: `initializeGatewayData()` (Line 2313+)

Both create Stripe line items from the `allTickets` array with identical logic.

---

## Summary: ✅ VERIFIED

✅ Tier iteration logic is **IDENTICAL**  
✅ Tier validation is **IDENTICAL**  
✅ Ticket creation logic is **IDENTICAL**  
✅ Inventory updates are **IDENTICAL**  
✅ CheckoutSession handling is **IDENTICAL**  
✅ PaymentIntent fields are **IDENTICAL** (except user type)  
✅ Error cleanup is **IDENTICAL**  
✅ Gateway initialization timing is **IDENTICAL** (after commit)

⚠️ **ONE DIFFERENCE**: Event preload timing (logged-in: in-tx, guest: post-commit)  
✅ **INTENTIONAL**: Quantity limits differ (10 vs 6) for security
