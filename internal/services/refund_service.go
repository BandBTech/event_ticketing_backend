package services

// import (
// 	"context"
// 	"errors"
// 	"fmt"
// 	"log"
// 	"time"

// 	"event-ticketing-backend/internal/models"
// 	"event-ticketing-backend/pkg/config"
// 	"event-ticketing-backend/pkg/utils"

// 	"github.com/google/uuid"
// 	"gorm.io/gorm"
// )

// // RefundService handles all refund operations
// type RefundService struct {
// 	db                 *gorm.DB
// 	ticketService      *TicketService
// 	emailQueueService  *EmailQueueService
// 	emailOutboxService *EmailOutboxService
// 	cfg                *config.Config
// }

// // NewRefundService creates a new refund service instance
// func NewRefundService(db *gorm.DB, cfg *config.Config) *RefundService {
// 	return &RefundService{
// 		db:  db,
// 		cfg: cfg,
// 	}
// }

// // SetTicketService sets the ticket service dependency
// func (s *RefundService) SetTicketService(ticketService *TicketService) {
// 	s.ticketService = ticketService
// }

// // SetEmailQueueService sets the email queue service dependency
// func (s *RefundService) SetEmailQueueService(emailQueueService *EmailQueueService) {
// 	s.emailQueueService = emailQueueService
// }

// // SetEmailOutboxService sets the email outbox service dependency
// func (s *RefundService) SetEmailOutboxService(emailOutboxService *EmailOutboxService) {
// 	s.emailOutboxService = emailOutboxService
// }

// // RequestRefund creates a refund request for tickets
// func (s *RefundService) RequestRefund(userID *uuid.UUID, guestUserID *uuid.UUID, req models.RefundRequest) (*models.RefundResponse, error) {
// 	// Validate that user owns the tickets
// 	var tickets []models.Ticket
// 	query := s.db.Preload("Event").Preload("Tier").Preload("Transaction")

// 	if userID != nil {
// 		query = query.Where("user_id = ? AND id IN ?", *userID, req.TicketIDs)
// 	} else if guestUserID != nil {
// 		query = query.Where("guest_user_id = ? AND id IN ?", *guestUserID, req.TicketIDs)
// 	} else {
// 		return nil, utils.NewValidationError("Either user ID or guest user ID must be provided", nil)
// 	}

// 	if err := query.Find(&tickets).Error; err != nil {
// 		return nil, utils.NewDatabaseError("Failed to find tickets", err)
// 	}

// 	if len(tickets) == 0 {
// 		return nil, utils.NewNotFoundError("No valid tickets found for refund")
// 	}

// 	// Check that all tickets belong to the same transaction
// 	transactionID := tickets[0].TransactionID
// 	for _, ticket := range tickets {
// 		if ticket.TransactionID != transactionID {
// 			return nil, utils.NewValidationError("All tickets must belong to the same transaction", nil)
// 		}
// 	}

// 	// Check refund eligibility
// 	eligible, reason, err := s.CheckRefundEligibility(req.TicketIDs)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if !eligible {
// 		return nil, utils.NewValidationError("Refund not eligible: "+reason, nil)
// 	}

// 	// Calculate refund amount (sum of ticket amounts)
// 	var refundAmount float64
// 	for _, ticket := range tickets {
// 		refundAmount += ticket.TotalAmount
// 	}

// 	// Create refund request record
// 	refundRequest := &models.RefundRequest{
// 		TransactionID: *transactionID,
// 		UserID:        userID,
// 		GuestUserID:   guestUserID,
// 		TicketIDs:     req.TicketIDs,
// 		RefundAmount:  refundAmount,
// 		Currency:      tickets[0].Transaction.Currency,
// 		Status:        "pending",
// 		Reason:        req.Reason,
// 	}

// 	if err := s.db.Create(refundRequest).Error; err != nil {
// 		return nil, utils.NewDatabaseError("Failed to create refund request", err)
// 	}

// 	// Get event title for response
// 	eventTitle := ""
// 	if len(tickets) > 0 && tickets[0].Event != nil {
// 		eventTitle = tickets[0].Event.Title
// 	}

// 	response := &models.RefundResponse{
// 		ID:            refundRequest.ID,
// 		TransactionID: refundRequest.TransactionID,
// 		EventTitle:    eventTitle,
// 		RefundAmount:  refundRequest.RefundAmount,
// 		Currency:      refundRequest.Currency,
// 		Status:        refundRequest.Status,
// 		Reason:        refundRequest.Reason,
// 		CreatedAt:     refundRequest.CreatedAt,
// 		UpdatedAt:     refundRequest.UpdatedAt,
// 	}

// 	return response, nil
// }

// // GetUserRefunds returns paginated list of user's refund requests
// func (s *RefundService) GetUserRefunds(userID *uuid.UUID, guestUserID *uuid.UUID, page, limit int) ([]models.RefundResponse, int64, error) {
// 	var refundRequests []models.RefundRequest
// 	var total int64

// 	query := s.db.Model(&models.RefundRequest{}).
// 		Preload("Transaction.Event")

// 	if userID != nil {
// 		query = query.Where("user_id = ?", *userID)
// 	} else if guestUserID != nil {
// 		query = query.Where("guest_user_id = ?", *guestUserID)
// 	} else {
// 		return nil, 0, utils.NewValidationError("Either user ID or guest user ID must be provided", nil)
// 	}

// 	// Count total records
// 	if err := query.Count(&total).Error; err != nil {
// 		return nil, 0, utils.NewDatabaseError("Failed to count refund requests", err)
// 	}

// 	// Get paginated results
// 	offset := (page - 1) * limit
// 	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&refundRequests).Error; err != nil {
// 		return nil, 0, utils.NewDatabaseError("Failed to get refund requests", err)
// 	}

// 	// Convert to response format
// 	responses := make([]models.RefundResponse, 0, len(refundRequests))
// 	for _, refundRequest := range refundRequests {
// 		eventTitle := ""
// 		if refundRequest.Transaction != nil && refundRequest.Transaction.Event != nil {
// 			eventTitle = refundRequest.Transaction.Event.Title
// 		}

// 		response := models.RefundResponse{
// 			ID:            refundRequest.ID,
// 			TransactionID: refundRequest.TransactionID,
// 			EventTitle:    eventTitle,
// 			RefundAmount:  refundRequest.RefundAmount,
// 			Currency:      refundRequest.Currency,
// 			Status:        refundRequest.Status,
// 			Reason:        refundRequest.Reason,
// 			CreatedAt:     refundRequest.CreatedAt,
// 			UpdatedAt:     refundRequest.UpdatedAt,
// 		}
// 		responses = append(responses, response)
// 	}

// 	return responses, total, nil
// }

// // ProcessRefund processes a refund request (admin only)
// func (s *RefundService) ProcessRefund(refundRequestID uuid.UUID, adminID uuid.UUID, approve bool, adminNotes string) error {
// 	var refundRequest models.RefundRequest
// 	if err := s.db.Preload("Transaction").Preload("Transaction.Tickets").First(&refundRequest, refundRequestID).Error; err != nil {
// 		if err == gorm.ErrRecordNotFound {
// 			return utils.NewNotFoundError("Refund request not found")
// 		}
// 		return utils.NewDatabaseError("Failed to find refund request", err)
// 	}

// 	if refundRequest.Status != "pending" {
// 		return utils.NewValidationError("Refund request has already been processed", nil)
// 	}

// 	now := time.Now()
// 	refundRequest.UpdatedAt = now

// 	if approve {
// 		// Get payment intent ID from transaction
// 		var transaction models.Transaction
// 		if err := s.db.Select("payment_intent_id, payment_gateway").First(&transaction, refundRequest.TransactionID).Error; err != nil {
// 			return utils.NewDatabaseError("Failed to find transaction", err)
// 		}

// 		// Verify payment intent exists (transaction must have it)
// 		// PaymentIntentID is now required in Transaction model

// 		// Approve refund request - create actual refund record for gateway processing
// 		refund := &models.Refund{
// 			TransactionID:   refundRequest.TransactionID,
// 			PaymentIntentID: transaction.PaymentIntentID,        // Use directly from transaction
// 			PaymentGateway:  string(transaction.PaymentGateway), // Use from transaction
// 			GatewayRefundID: "",                                 // Will be set after gateway processing
// 			Amount:          refundRequest.RefundAmount,
// 			Currency:        refundRequest.Currency,
// 			Reason:          refundRequest.Reason,
// 			RefundType:      "customer_request",
// 			Status:          "processing",
// 			AffectedTicketIDs: func() []string {
// 				ids := make([]string, len(refundRequest.TicketIDs))
// 				for i, id := range refundRequest.TicketIDs {
// 					ids[i] = id.String()
// 				}
// 				return ids
// 			}(),
// 			TicketCount: len(refundRequest.TicketIDs),
// 			InitiatedBy: refundRequest.UserID, // User who requested refund
// 			ApprovedBy:  &adminID,
// 			Notes:       adminNotes,
// 			RequestedAt: &refundRequest.CreatedAt,
// 			ApprovedAt:  &now,
// 		}

// 		if err := s.db.Create(refund).Error; err != nil {
// 			return utils.NewDatabaseError("Failed to create refund record", err)
// 		}

// 		// Update refund request status
// 		refundRequest.Status = "approved"

// 		// TODO: Trigger actual gateway refund processing asynchronously
// 		// For now, we'll simulate completion
// 		refund.Status = "completed"
// 		refund.GatewayRefundID = string(refundRequest.Transaction.PaymentGateway) // Use gateway name
// 		refund.ProcessedAt = &now

// 		// Update ticket statuses to refunded
// 		if err := s.db.Model(&models.Ticket{}).Where("id IN ?", refundRequest.TicketIDs).
// 			Updates(map[string]interface{}{
// 				"status":     "refunded",
// 				"updated_at": now,
// 			}).Error; err != nil {
// 			return utils.NewDatabaseError("Failed to update ticket statuses", err)
// 		}

// 		// Update transaction status if all tickets are refunded
// 		var totalTickets int64
// 		var refundedTickets int64
// 		s.db.Model(&models.Ticket{}).Where("transaction_id = ?", refundRequest.TransactionID).Count(&totalTickets)
// 		s.db.Model(&models.Ticket{}).Where("transaction_id = ? AND status = 'refunded'", refundRequest.TransactionID).Count(&refundedTickets)

// 		if totalTickets == refundedTickets {
// 			if err := s.db.Model(&models.Transaction{}).Where("id = ?", refundRequest.TransactionID).
// 				Update("status", "refunded").Error; err != nil {
// 				return utils.NewDatabaseError("Failed to update transaction status", err)
// 			}

// 			// Log audit for transaction status update
// 			s.logAudit(context.Background(), "transaction_refunded", "transaction", refundRequest.TransactionID, nil, "system", nil, map[string]interface{}{
// 				"status":          "refunded",
// 				"total_refunded":  true,
// 				"refunded_amount": refund.Amount,
// 				"refund_id":       refund.ID,
// 			})
// 		}

// 		// Save the updated refund record
// 		if err := s.db.Save(refund).Error; err != nil {
// 			return utils.NewDatabaseError("Failed to update refund status", err)
// 		}

// 	} else {
// 		// Reject refund request
// 		refundRequest.Status = "rejected"

// 		// Restore ticket statuses to active since refund was rejected
// 		if err := s.db.Model(&models.Ticket{}).Where("id IN ?", refundRequest.TicketIDs).
// 			Updates(map[string]interface{}{
// 				"status":     "active",
// 				"updated_at": now,
// 			}).Error; err != nil {
// 			return utils.NewDatabaseError("Failed to restore ticket statuses", err)
// 		}

// 		// Restore tier inventory since tickets are active again
// 		for _, ticketID := range refundRequest.TicketIDs {
// 			var ticket models.Ticket
// 			if err := s.db.Select("tier_id").First(&ticket, ticketID).Error; err != nil {
// 				continue // Skip if ticket not found
// 			}
// 			if err := s.db.Model(&models.EventTier{}).
// 				Where("id = ?", ticket.TierID).
// 				Update("available", gorm.Expr("available - ?", 1)).Error; err != nil {
// 				// Log error but don't fail the entire operation
// 				log.Printf("Warning: Failed to restore tier inventory for ticket %s: %v", ticketID, err)
// 			}
// 		}
// 	}

// 	return s.db.Save(&refundRequest).Error
// }

// // CheckRefundEligibility checks if tickets are eligible for refund
// // Returns eligibility status and reason if not eligible
// func (s *RefundService) CheckRefundEligibility(ticketIDs []uuid.UUID) (bool, string, error) {
// 	var tickets []models.Ticket
// 	if err := s.db.Where("id IN ?", ticketIDs).
// 		Preload("Event").
// 		Preload("Transaction").
// 		Find(&tickets).Error; err != nil {
// 		return false, "", fmt.Errorf("failed to fetch tickets: %w", err)
// 	}

// 	if len(tickets) != len(ticketIDs) {
// 		return false, "Some tickets not found", nil
// 	}

// 	// Check each ticket for refund eligibility
// 	for _, ticket := range tickets {
// 		// 1. Check ticket status
// 		if ticket.Status == "refunded" {
// 			return false, fmt.Sprintf("Ticket %s has already been refunded", ticket.TicketNumber), nil
// 		}
// 		if ticket.Status == "cancelled" {
// 			return false, fmt.Sprintf("Ticket %s is already cancelled", ticket.TicketNumber), nil
// 		}
// 		if ticket.Status == "pending_refund" {
// 			return false, fmt.Sprintf("Ticket %s already has a pending refund request", ticket.TicketNumber), nil
// 		}
// 		if ticket.Status == "used" || ticket.CheckInTime != nil {
// 			return false, fmt.Sprintf("Ticket %s has been checked in and cannot be refunded", ticket.TicketNumber), nil
// 		}

// 		// 2. Check event status
// 		if ticket.Event == nil {
// 			return false, "Event information not available", nil
// 		}
// 		if ticket.Event.IsCancelled || ticket.Event.Status == "cancelled" {
// 			return false, fmt.Sprintf("Cannot refund tickets for cancelled event: %s", ticket.Event.Title), nil
// 		}
// 		if ticket.Event.Status == "completed" {
// 			return false, fmt.Sprintf("Cannot refund tickets for completed event: %s", ticket.Event.Title), nil
// 		}

// 		// 3. Check event timing - no refunds within 24 hours of event start
// 		now := time.Now()
// 		timeUntilEvent := ticket.Event.StartDate.Sub(now)
// 		if timeUntilEvent < 24*time.Hour {
// 			return false, "Refunds not allowed within 24 hours of event start.", nil
// 		}

// 		// 4. Check purchase timing - no refunds within 1 hour of purchase
// 		timeSincePurchase := now.Sub(ticket.CreatedAt)
// 		if timeSincePurchase < 1*time.Hour {
// 			return false, "Refunds not allowed within 1 hour of purchase.", nil
// 		}

// 		// 5. Check event sales status
// 		if ticket.Event.SalesStatus == "stopped" {
// 			return false, fmt.Sprintf("Ticket sales have been stopped for event: %s", ticket.Event.Title), nil
// 		}
// 	}

// 	return true, "", nil
// }

// // CancelTicketWithRefund handles ticket cancellation and creates a refund request
// // Returns a map with refund status information
// func (s *RefundService) CancelTicketWithRefund(ticketID uuid.UUID, userID uuid.UUID, reason string) (map[string]interface{}, error) {
// 	// Get ticket with related data
// 	var ticket models.Ticket
// 	if err := s.db.Where("id = ?", ticketID).
// 		Preload("Event").
// 		Preload("Transaction").
// 		Preload("Tier").
// 		Find(&ticket).Error; err != nil {
// 		return nil, fmt.Errorf("failed to fetch ticket: %w", err)
// 	}

// 	// Begin transaction
// 	tx := s.db.Begin()

// 	// 1. Mark ticket as pending refund
// 	now := time.Now()
// 	if err := tx.Model(&ticket).Updates(map[string]interface{}{
// 		"status":     "pending_refund",
// 		"updated_at": now,
// 	}).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to mark ticket as pending refund: %w", err)
// 	}

// 	// 2. Restore tier inventory
// 	if err := tx.Model(&models.EventTier{}).
// 		Where("id = ?", ticket.TierID).
// 		Update("available", gorm.Expr("available + ?", 1)).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to restore tier inventory: %w", err)
// 	}

// 	// 3. Create refund request
// 	refundNumber := fmt.Sprintf("RF-%s-%d", ticket.TicketNumber, time.Now().Unix())
// 	affectedTicketIDs := []string{ticketID.String()}
// 	refund := models.Refund{
// 		RefundNumber:      refundNumber,
// 		TransactionID:     *ticket.TransactionID,
// 		PaymentIntentID:   ticket.Transaction.PaymentIntentID, // Get PaymentIntentID from Transaction
// 		PaymentGateway:    string(ticket.PaymentGateway),
// 		GatewayRefundID:   fmt.Sprintf("LOCAL-%d", time.Now().Unix()),
// 		Amount:            ticket.TotalAmount,
// 		Currency:          ticket.Event.Currency,
// 		Reason:            reason,
// 		RefundType:        "customer_request",
// 		Status:            "pending",
// 		AffectedTicketIDs: affectedTicketIDs,
// 		TicketCount:       1,
// 		InitiatedBy:       &userID,
// 		RequestedAt:       &now,
// 	}

// 	if err := tx.Create(&refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create refund: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit transaction: %w", err)
// 	}

// 	// Log refund creation
// 	s.logAudit(context.Background(), "refund_requested", "refund", refund.ID, &userID, "user", &ticket.EventID, map[string]interface{}{
// 		"refund_number":     refund.RefundNumber,
// 		"ticket_id":         ticketID.String(),
// 		"amount":            refund.Amount,
// 		"currency":          refund.Currency,
// 		"reason":            reason,
// 		"transaction_id":    refund.TransactionID.String(),
// 		"payment_intent_id": refund.PaymentIntentID.String(),
// 	})

// 	// Send refund created notification email
// 	go func() {
// 		if s.emailQueueService != nil {
// 			// Get user email for notification
// 			var userEmail string
// 			var userName string
// 			if userID != uuid.Nil {
// 				var user models.User
// 				if err := s.db.Where("id = ?", userID).First(&user).Error; err == nil {
// 					userEmail = user.Email
// 					userName = user.FirstName + " " + user.LastName
// 				}
// 			} else {
// 				// For guest users, we might need to get email from payment intent
// 				// This would require additional logic to fetch payment intent
// 				log.Printf("[REFUND] Guest user refund created, email notification skipped: %s", refundNumber)
// 				return
// 			}

// 			if userEmail != "" {
// 				// Queue refund created email using centralized system
// 				templateData := map[string]interface{}{
// 					"event_name":      ticket.Event.Title,
// 					"refund_amount":   ticket.TotalAmount,
// 					"currency":        ticket.Event.Currency,
// 					"ticket_count":    1, // Single ticket refund
// 					"refund_number":   refundNumber,
// 					"refund_reason":   reason,
// 					"refund_status":   "pending",
// 					"user_name":       userName,
// 					"recipient_email": userEmail,
// 				}

// 				subject := fmt.Sprintf("Refund Request Submitted - %s", refundNumber)
// 				if err := s.emailOutboxService.QueueEmail(context.Background(), models.EmailEventRefundProcessed, userEmail, subject, templateData, 2); err != nil {
// 					log.Printf("[REFUND] Warning: Failed to queue refund created email: %v", err)
// 				} else {
// 					log.Printf("[REFUND] Refund created email queued for %s", userEmail)
// 				}
// 			}
// 		}
// 	}()

// 	return map[string]interface{}{
// 		"refund_status": "pending",
// 		"refund_number": refundNumber,
// 		"ticket_id":     ticketID,
// 	}, nil
// }

// // RestoreRefundedTicketInventoryByIDs restores inventory and updates event availability for refunded tickets.
// func (s *RefundService) RestoreRefundedTicketInventoryByIDs(tx *gorm.DB, ticketIDs []uuid.UUID) error {
// 	if len(ticketIDs) == 0 {
// 		return nil
// 	}

// 	commitTx := false
// 	if tx == nil {
// 		tx = s.db.Begin()
// 		commitTx = true
// 	}

// 	defer func() {
// 		if r := recover(); r != nil {
// 			if commitTx {
// 				tx.Rollback()
// 			}
// 		}
// 	}()

// 	var tickets []models.Ticket
// 	if err := tx.Where("id IN ?", ticketIDs).Find(&tickets).Error; err != nil {
// 		if commitTx {
// 			tx.Rollback()
// 		}
// 		return fmt.Errorf("failed to load tickets for refund inventory restoration: %w", err)
// 	}
// 	if len(tickets) == 0 {
// 		if commitTx {
// 			tx.Rollback()
// 		}
// 		return fmt.Errorf("no tickets found for refund inventory restoration")
// 	}

// 	if err := tx.Model(&models.Ticket{}).Where("id IN ?", ticketIDs).
// 		Updates(map[string]interface{}{"status": "refunded", "updated_at": time.Now()}).Error; err != nil {
// 		if commitTx {
// 			tx.Rollback()
// 		}
// 		return fmt.Errorf("failed to update refunded ticket statuses: %w", err)
// 	}

// 	tierQuantities := make(map[uuid.UUID]int)
// 	eventQuantities := make(map[uuid.UUID]int)
// 	for _, ticket := range tickets {
// 		tierQuantities[ticket.TierID]++
// 		eventQuantities[ticket.EventID]++
// 	}

// 	for tierID, qty := range tierQuantities {
// 		if err := tx.Model(&models.EventTier{}).
// 			Where("id = ?", tierID).
// 			Updates(map[string]interface{}{
// 				"sold":      gorm.Expr("GREATEST(sold - ?, 0)", qty),
// 				"available": gorm.Expr("LEAST(available + ?, quantity)", qty),
// 			}).Error; err != nil {
// 			if commitTx {
// 				tx.Rollback()
// 			}
// 			return fmt.Errorf("failed to restore tier inventory for tier %s: %w", tierID, err)
// 		}
// 	}

// 	for eventID, qty := range eventQuantities {
// 		if err := tx.Model(&models.Event{}).
// 			Where("id = ?", eventID).
// 			Update("available", gorm.Expr("LEAST(available + ?, capacity)", qty)).Error; err != nil {
// 			if commitTx {
// 				tx.Rollback()
// 			}
// 			return fmt.Errorf("failed to restore event availability for event %s: %w", eventID, err)
// 		}
// 	}

// 	if commitTx {
// 		if err := tx.Commit().Error; err != nil {
// 			return fmt.Errorf("failed to commit refund inventory restoration: %w", err)
// 		}
// 	}

// 	return nil
// }

// // ProcessRefundedPayment processes a refunded payment from Stripe webhook
// // This cancels tickets and marks the transaction as refunded
// func (s *RefundService) ProcessRefundedPayment(checkoutToken string) error {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	// Find payment intent with this token
// 	var paymentIntent models.PaymentIntent
// 	if err := tx.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to find payment intent: %w", err)
// 	}

// 	// Update payment intent status to refunded
// 	paymentIntent.Status = "refunded"
// 	paymentIntent.UpdatedAt = time.Now()
// 	if err := tx.Save(&paymentIntent).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to update payment intent: %w", err)
// 	}

// 	// Collect all tickets for this payment intent
// 	var tickets []models.Ticket
// 	if err := tx.Where("checkout_token = ?", checkoutToken).Find(&tickets).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to find tickets: %w", err)
// 	}

// 	if len(tickets) == 0 {
// 		tx.Rollback()
// 		return utils.NewBusinessLogicError("No tickets found for payment intent")
// 	}

// 	// Extract ticket IDs for inventory restoration
// 	ticketIDs := make([]uuid.UUID, len(tickets))
// 	for i, ticket := range tickets {
// 		ticketIDs[i] = ticket.ID
// 	}

// 	if err := s.RestoreRefundedTicketInventoryByIDs(tx, ticketIDs); err != nil {
// 		tx.Rollback()
// 		return err
// 	}

// 	// Find and update transaction status to refunded
// 	var transaction models.Transaction
// 	if err := tx.Where("payment_intent_id = ?", paymentIntent.ID).First(&transaction).Error; err != nil {
// 		// Transaction may not exist yet if refund came before success webhook was processed
// 		log.Printf("Warning: Transaction not found for refunded payment intent %s\n", checkoutToken)
// 	} else {
// 		// NOTE: We don't update transaction status to "refunded" because:
// 		// 1. Financial reporting filters by status = 'completed' for revenue/ticket counts
// 		// 2. Refunds are tracked separately in the Refund model
// 		// 3. Changing status would break revenue analytics and commission calculations
// 		// Instead, refunds are handled separately in financial reports
// 		log.Printf("Transaction %s associated with refunded checkout session (keeping status as-is for financial reporting)\n", transaction.ID)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return fmt.Errorf("failed to commit transaction: %w", err)
// 	}

// 	return nil
// }

// // logAudit creates audit log entries for refund operations
// func (s *RefundService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
// 	// Implementation would go here - simplified for now
// 	log.Printf("Audit: %s on %s %s by %s", action, entityType, entityID, actorType)
// }

// // RequestRefund creates a refund request (from payment_service.go)
// func (s *RefundService) RequestRefund(ctx context.Context, paymentIntentID, userID uuid.UUID, reason string, ticketIDs []uuid.UUID) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var paymentIntent models.PaymentIntent
// 	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("payment intent not found: %w", err)
// 	}

// 	// Check authorization
// 	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Unauthorized.")
// 	}

// 	// Only succeeded payments can be refunded
// 	if paymentIntent.Status != "succeeded" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
// 	}

// 	// Validate refund conditions
// 	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not allowed: %w", err)
// 	}

// 	// Calculate refund amount based on tickets
// 	refundAmount := float64(paymentIntent.AmountTotal) * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity)) / 100

// 	// Find the transaction ID associated with this payment intent
// 	var transaction models.Transaction
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&transaction).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("transaction not found for payment intent: %w", err)
// 	}

// 	// Capture charge ID at creation time (don't rely on fetching PaymentIntent later)
// 	gatewayMetadata := map[string]interface{}{}

// 	// Get the charge ID from PaymentAttempt
// 	var paymentAttempt models.PaymentAttempt
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&paymentAttempt).Error; err == nil {
// 		if paymentAttempt.ProviderChargeID != "" {
// 			gatewayMetadata["stripe_charge_id"] = paymentAttempt.ProviderChargeID
// 		}
// 	} else {
// 		log.Printf("[REFUND] Warning: No payment attempt found for payment intent %s. Refund processing may fail.", paymentIntentID)
// 	}

// 	refund := &models.Refund{
// 		PaymentIntentID:  paymentIntentID,
// 		TransactionID:    transaction.ID,
// 		ProviderRefundID: "",
// 		Amount:           int64(refundAmount * 100),
// 		Currency:         paymentIntent.Currency,
// 		Reason:           reason,
// 		Status:           "pending",
// 		InitiatedBy:      &userID,
// 		GatewayMetadata:  gatewayMetadata,
// 		TicketCount:      len(ticketIDs),
// 		AffectedTicketIDs: func() []string {
// 			ids := make([]string, len(ticketIDs))
// 			for i, id := range ticketIDs {
// 				ids[i] = id.String()
// 			}
// 			return ids
// 		}(),
// 	}

// 	if err := tx.Create(refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create refund request: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit refund request: %w", err)
// 	}

// 	return refund, nil
// }

// // validateRefundConditions checks if a refund request meets all business requirements
// func (s *RefundService) validateRefundConditions(ctx context.Context, paymentIntentID uuid.UUID, ticketIDs []uuid.UUID) error {
// 	// Check if tickets exist and belong to the payment intent
// 	var tickets []models.Ticket
// 	if err := s.db.Where("id IN ? AND payment_intent_id = ?", ticketIDs, paymentIntentID).Find(&tickets).Error; err != nil {
// 		return fmt.Errorf("failed to validate tickets: %w", err)
// 	}

// 	if len(tickets) != len(ticketIDs) {
// 		return errors.New("some tickets not found or don't belong to this payment intent")
// 	}

// 	// Check ticket statuses - only active tickets can be refunded
// 	for _, ticket := range tickets {
// 		if ticket.Status != "active" {
// 			return fmt.Errorf("ticket %s has status %s and cannot be refunded", ticket.TicketNumber, ticket.Status)
// 		}
// 	}

// 	// Check if any tickets have already been refunded
// 	var existingRefunds []models.Refund
// 	if err := s.db.Where("payment_intent_id = ? AND status IN ?", paymentIntentID, []string{"pending", "processing", "completed"}).Find(&existingRefunds).Error; err != nil {
// 		return fmt.Errorf("failed to check existing refunds: %w", err)
// 	}

// 	// Check if any of the requested tickets are already being refunded
// 	for _, refund := range existingRefunds {
// 		for _, ticketID := range ticketIDs {
// 			for _, affectedID := range refund.AffectedTicketIDs {
// 				if affectedID == ticketID.String() {
// 					return fmt.Errorf("ticket %s is already part of an existing refund request", ticketID)
// 				}
// 			}
// 		}
// 	}

// 	return nil
// }

// // ApproveRefund approves and processes a refund through the payment gateway
// func (s *RefundService) ApproveRefund(ctx context.Context, refundID, adminID uuid.UUID) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var refund models.Refund
// 	if err := tx.Preload("PaymentIntent").First(&refund, refundID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not found: %w", err)
// 	}

// 	if refund.Status != "pending" {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund is not in pending status")
// 	}

// 	// Update refund status to processing
// 	now := time.Now()
// 	refund.Status = "processing"
// 	refund.ApprovedBy = &adminID
// 	refund.ApprovedAt = &now

// 	if err := tx.Save(&refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to update refund status: %w", err)
// 	}

// 	// Process the refund through the payment gateway
// 	if err := s.processGatewayRefund(ctx, &refund, &adminID); err != nil {
// 		// If gateway processing fails, mark as failed but don't rollback
// 		// This allows manual retry later
// 		refund.Status = "failed"
// 		refund.Notes = fmt.Sprintf("Gateway processing failed: %v", err)
// 		tx.Save(&refund)
// 		tx.Commit()
// 		return &refund, fmt.Errorf("gateway refund processing failed: %w", err)
// 	}

// 	// Update ticket statuses to refunded
// 	ticketIDs := make([]uuid.UUID, len(refund.AffectedTicketIDs))
// 	for i, idStr := range refund.AffectedTicketIDs {
// 		if id, err := uuid.Parse(idStr); err == nil {
// 			ticketIDs[i] = id
// 		}
// 	}

// 	if err := tx.Model(&models.Ticket{}).Where("id IN ?", ticketIDs).
// 		Updates(map[string]interface{}{
// 			"status":     "refunded",
// 			"updated_at": now,
// 		}).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to update ticket statuses: %w", err)
// 	}

// 	// Restore inventory
// 	if err := s.restoreRefundedInventory(tx, ticketIDs); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to restore inventory: %w", err)
// 	}

// 	// Mark refund as completed
// 	refund.Status = "completed"
// 	refund.ProcessedAt = &now

// 	if err := tx.Save(&refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to mark refund as completed: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit refund approval: %w", err)
// 	}

// 	// Send notification
// 	go s.notifyUserRefundCompleted(ctx, refund.PaymentIntent, &refund, "completed")

// 	return &refund, nil
// }

// // processGatewayRefund handles the actual gateway refund processing
// func (s *RefundService) processGatewayRefund(ctx context.Context, refund *models.Refund, adminID *uuid.UUID) error {
// 	// Simplified implementation - in real scenario would call Stripe API
// 	log.Printf("Processing gateway refund for refund ID: %s, amount: %d %s", refund.ID, refund.Amount, refund.Currency)

// 	// Simulate gateway processing
// 	time.Sleep(100 * time.Millisecond) // Simulate API call

// 	// Set gateway refund ID
// 	refund.ProviderRefundID = fmt.Sprintf("rf_%s", refund.ID.String()[:14])

// 	return nil
// }

// // restoreRefundedInventory restores ticket inventory when refunds are processed
// func (s *RefundService) restoreRefundedInventory(tx *gorm.DB, ticketIDs []uuid.UUID) error {
// 	var tickets []models.Ticket
// 	if err := tx.Where("id IN ?", ticketIDs).Find(&tickets).Error; err != nil {
// 		return fmt.Errorf("failed to load tickets for inventory restoration: %w", err)
// 	}

// 	tierQuantities := make(map[uuid.UUID]int)
// 	for _, ticket := range tickets {
// 		tierQuantities[ticket.TierID]++
// 	}

// 	for tierID, qty := range tierQuantities {
// 		if err := tx.Model(&models.EventTier{}).
// 			Where("id = ?", tierID).
// 			Updates(map[string]interface{}{
// 				"sold":      gorm.Expr("GREATEST(sold - ?, 0)", qty),
// 				"available": gorm.Expr("LEAST(available + ?, quantity)", qty),
// 			}).Error; err != nil {
// 			return fmt.Errorf("failed to restore tier inventory for tier %s: %w", tierID, err)
// 		}
// 	}

// 	return nil
// }

// // notifyUserRefundCompleted sends a notification to user about refund status
// func (s *RefundService) notifyUserRefundCompleted(ctx context.Context, pi *models.PaymentIntent, refund *models.Refund, status string) error {
// 	// Simplified notification implementation
// 	log.Printf("Refund %s status updated to %s for payment intent %s", refund.ID, status, pi.ID)
// 	return nil
// }

// RejectRefund rejects a refund request
// func (s *PaymentService) RejectRefund(ctx context.Context, refundID, adminID uuid.UUID, reason string) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var refund models.Refund
// 	if err := tx.First(&refund, refundID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not found: %w", err)
// 	}

// 	if refund.Status != "pending" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
// 	}

// 	refund.Status = "rejected"
// 	refund.RejectionReason = reason
// 	refund.ApprovedBy = &adminID
// 	now := time.Now()
// 	refund.ApprovedAt = &now

// 	if err := tx.Save(&refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to update refund: %w", err)
// 	}

// 	// Log status change
// 	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "pending", "rejected", &adminID, "admin", fmt.Sprintf("Refund rejected: %s", reason), nil); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to log status change: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit rejection: %w", err)
// 	}

// 	// Get event ID for audit logging
// 	var eventID *uuid.UUID
// 	var pi models.PaymentIntent
// 	if err := s.db.Select("event_id").Where("id = ?", refund.PaymentIntentID).First(&pi).Error; err == nil {
// 		eventID = &pi.EventID
// 	}

// 	changes := map[string]interface{}{}
// 	if eventID != nil {
// 		changes["event_id"] = eventID
// 	}
// 	changes["rejection_reason"] = reason
// 	changes["status_change"] = "pending -> rejected"

// 	s.logAudit(ctx, "refund_rejected", "refund", refund.ID, &adminID, "admin", eventID, changes)

// 	return &refund, nil
// }

// // AdminGetAllRefunds retrieves all refunds with filters
// func (s *PaymentService) AdminGetAllRefunds(ctx context.Context, status, search, refundType string, startDate, endDate *time.Time, page, limit int, sortBy, sortOrder string) ([]models.Refund, int64, error) {
// 	var refunds []models.Refund
// 	var total int64

// 	// Normalize sort order to lowercase to handle both lowercase and uppercase values from handler
// 	sortOrder = strings.ToLower(sortOrder)

// 	query := s.db.Model(&models.Refund{}).Preload("Transaction").Preload("Initiator").Preload("PaymentIntent").Preload("PaymentIntent.Event")

// 	// Apply status filter
// 	if status != "" {
// 		query = query.Where("refunds.status = ?", status)
// 	}

// 	// Apply refund type filter
// 	if refundType != "" {
// 		query = query.Where("refunds.refund_type = ?", refundType)
// 	}

// 	// Apply date filters
// 	if startDate != nil {
// 		query = query.Where("refunds.created_at >= ?", *startDate)
// 	}
// 	if endDate != nil {
// 		query = query.Where("refunds.created_at <= ?", *endDate)
// 	}

// 	// Apply search filter
// 	if search != "" {
// 		searchTerm := "%" + strings.ToLower(search) + "%"
// 		query = query.Joins("LEFT JOIN users u ON refunds.initiated_by = u.id").
// 			Where("LOWER(refunds.refund_number) LIKE ? OR LOWER(refunds.reason) LIKE ? OR LOWER(u.first_name || ' ' || u.last_name) LIKE ? OR LOWER(u.email) LIKE ? OR LOWER(refunds.transaction_id::text) LIKE ?",
// 				searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
// 	}

// 	// Always join with users table if sorting by initiated_by
// 	if sortBy == "initiated_by" {
// 		query = query.Joins("LEFT JOIN users u ON refunds.initiated_by = u.id")
// 	}

// 	// Count total records
// 	query.Count(&total)

// 	offset := (page - 1) * limit

// 	// Generate ORDER BY clause with special handling for initiated_by
// 	var orderByClause string
// 	if sortBy == "initiated_by" {
// 		// Sort by initiator's name (first_name + last_name)
// 		orderByClause = fmt.Sprintf("LOWER(COALESCE(TRIM(u.first_name || ' ' || u.last_name), '')) %s", sortOrder)
// 	} else if sortBy == "status" {
// 		// Use table prefix for status to be consistent and avoid potential ambiguity
// 		orderByClause = utils.GenerateOrderByClause("refunds.status", sortOrder)
// 	} else if sortBy == "refund_number" {
// 		// Use table prefix for refund_number
// 		orderByClause = utils.GenerateOrderByClause("refunds.refund_number", sortOrder)
// 	} else if sortBy == "amount" {
// 		// Amount is numeric, no LOWER needed
// 		orderByClause = "refunds.amount " + sortOrder
// 	} else if sortBy == "created_at" {
// 		// Use table prefix for created_at
// 		orderByClause = "refunds.created_at " + sortOrder
// 	} else {
// 		orderByClause = utils.GenerateOrderByClause(sortBy, sortOrder)
// 	}

// 	if err := query.Order(orderByClause).
// 		Offset(offset).
// 		Limit(limit).
// 		Find(&refunds).Error; err != nil {
// 		return nil, 0, fmt.Errorf("failed to retrieve refunds: %w", err)
// 	}

// 	return refunds, total, nil
// }

// // UserGetRefunds retrieves refunds for a specific user
// func (s *PaymentService) UserGetRefunds(ctx context.Context, userID uuid.UUID, status string, page, limit int, sortBy, sortOrder string) ([]models.Refund, int64, error) {
// 	var refunds []models.Refund
// 	var total int64

// 	// Normalize sort order to lowercase to handle both lowercase and uppercase values from handler
// 	sortOrder = strings.ToLower(sortOrder)

// 	query := s.db.Model(&models.Refund{}).
// 		Joins("JOIN payment_intents pi ON refunds.payment_intent_id = pi.id").
// 		Where("pi.user_id = ?", userID)

// 	if status != "" {
// 		query = query.Where("refunds.status = ?", status)
// 	}

// 	query.Count(&total)

// 	offset := (page - 1) * limit
// 	orderClause := "refunds." + sortBy + " " + sortOrder
// 	if err := query.Preload("Transaction").Preload("Initiator").Preload("PaymentIntent").Preload("PaymentIntent.Event").
// 		Order(orderClause).
// 		Offset(offset).
// 		Limit(limit).
// 		Find(&refunds).Error; err != nil {
// 		return nil, 0, fmt.Errorf("failed to retrieve user refunds: %w", err)
// 	}

// 	return refunds, total, nil
// }

// // convertRefundsToListResponse converts Refund models to RefundListResponse
// func (s *PaymentService) convertRefundsToListResponse(refunds []models.Refund) []models.RefundListResponse {
// 	responses := make([]models.RefundListResponse, len(refunds))
// 	for i, refund := range refunds {
// 		response := models.RefundListResponse{
// 			ID:            refund.ID,
// 			RefundNumber:  refund.RefundNumber,
// 			TransactionID: refund.TransactionID,
// 			Amount:        refund.Amount,
// 			Currency:      refund.Currency,
// 			Reason:        refund.Reason,
// 			RefundType:    refund.RefundType,
// 			Status:        refund.Status,
// 			TicketCount:   refund.TicketCount,
// 			RequestedAt:   refund.RequestedAt,
// 			CreatedAt:     refund.CreatedAt,
// 			UpdatedAt:     refund.UpdatedAt,
// 		}

// 		// Add initiated by info
// 		if refund.Initiator != nil {
// 			name := refund.Initiator.FirstName
// 			if refund.Initiator.LastName != "" {
// 				name += " " + refund.Initiator.LastName
// 			}
// 			response.InitiatedBy = &models.RefundUserInfo{
// 				ID:    refund.Initiator.ID,
// 				Name:  name,
// 				Email: refund.Initiator.Email,
// 			}
// 		}

// 		responses[i] = response
// 	}
// 	return responses
// }

// // UserGetRefundsList retrieves refunds for a specific user and returns RefundListResponse
// func (s *PaymentService) UserGetRefundsList(ctx context.Context, userID uuid.UUID, status string, page, limit int, sortBy, sortOrder string) ([]models.RefundListResponse, int64, error) {
// 	refunds, total, err := s.UserGetRefunds(ctx, userID, status, page, limit, sortBy, sortOrder)
// 	if err != nil {
// 		return nil, 0, err
// 	}
// 	return s.convertRefundsToListResponse(refunds), total, nil
// }

// // AdminGetAllRefundsList retrieves all refunds with filters and returns RefundListResponse
// func (s *PaymentService) AdminGetAllRefundsList(ctx context.Context, status, search, refundType string, startDate, endDate *time.Time, page, limit int, sortBy, sortOrder string) ([]models.RefundListResponse, int64, error) {
// 	refunds, total, err := s.AdminGetAllRefunds(ctx, status, search, refundType, startDate, endDate, page, limit, sortBy, sortOrder)
// 	if err != nil {
// 		return nil, 0, err
// 	}
// 	return s.convertRefundsToListResponse(refunds), total, nil
// }

// // convertRefundToDetailResponse converts a single Refund model to RefundDetailResponse
// func (s *PaymentService) convertRefundToDetailResponse(refund *models.Refund) models.RefundDetailResponse {
// 	response := models.RefundDetailResponse{
// 		ID:                refund.ID,
// 		RefundNumber:      refund.RefundNumber,
// 		Amount:            refund.Amount,
// 		Currency:          refund.Currency,
// 		Reason:            refund.Reason,
// 		RefundType:        refund.RefundType,
// 		Status:            refund.Status,
// 		AffectedTicketIDs: refund.AffectedTicketIDs,
// 		TicketCount:       refund.TicketCount,
// 		RequestedAt:       refund.RequestedAt,
// 		CreatedAt:         refund.CreatedAt,
// 		UpdatedAt:         refund.UpdatedAt,
// 	}

// 	// Add transaction info
// 	if refund.Transaction != nil {
// 		response.Transaction = models.RefundTransactionInfo{
// 			ID:        refund.Transaction.ID,
// 			Amount:    float64(refund.Transaction.Amount) / 100, // Convert cents to dollars
// 			Gateway:   string(refund.Transaction.PaymentGateway),
// 			Status:    refund.Transaction.Status,
// 			CreatedAt: refund.Transaction.CreatedAt,
// 		}
// 	}

// 	// Add event info
// 	if refund.PaymentIntent != nil && refund.PaymentIntent.Event != nil {
// 		response.Event = &models.RefundEventInfo{
// 			ID:          refund.PaymentIntent.Event.ID,
// 			Title:       refund.PaymentIntent.Event.Title,
// 			BannerImage: refund.PaymentIntent.Event.BannerImage,
// 		}
// 	}

// 	// Add organizer info
// 	if refund.PaymentIntent != nil && refund.PaymentIntent.Event != nil && refund.PaymentIntent.Event.Organizer != nil {
// 		organizer := refund.PaymentIntent.Event.Organizer
// 		var organizerName string

// 		// Use business_name from onboarding if available, otherwise first_name + last_name
// 		if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
// 			organizerName = organizer.OrganizerOnboarding.BusinessName
// 		} else {
// 			organizerName = organizer.FirstName
// 			if organizer.LastName != "" {
// 				organizerName += " " + organizer.LastName
// 			}
// 		}

// 		response.Organizer = &models.RefundOrganizerInfo{
// 			ID:   organizer.ID,
// 			Name: organizerName,
// 		}
// 	}

// 	// Add initiated by info
// 	if refund.Initiator != nil {
// 		name := refund.Initiator.FirstName
// 		if refund.Initiator.LastName != "" {
// 			name += " " + refund.Initiator.LastName
// 		}
// 		response.InitiatedBy = &models.RefundUserInfo{
// 			ID:    refund.Initiator.ID,
// 			Name:  name,
// 			Email: refund.Initiator.Email,
// 		}
// 	}

// 	return response
// }

// // AdminGetRefund retrieves a single refund by ID for admin
// func (s *PaymentService) AdminGetRefund(ctx context.Context, refundID uuid.UUID) (*models.RefundDetailResponse, error) {
// 	var refund models.Refund
// 	if err := s.db.Preload("Transaction").Preload("Initiator").
// 		Preload("PaymentIntent").Preload("PaymentIntent.Event").
// 		First(&refund, refundID).Error; err != nil {
// 		if err == gorm.ErrRecordNotFound {
// 			return nil, fmt.Errorf("refund not found")
// 		}
// 		return nil, fmt.Errorf("failed to retrieve refund: %w", err)
// 	}

// 	// Manually load organizer and onboarding if event exists
// 	if refund.PaymentIntent != nil && refund.PaymentIntent.Event != nil {
// 		var organizer models.User
// 		if err := s.db.Preload("OrganizerOnboarding").Where("id = ? AND deleted_at IS NULL", refund.PaymentIntent.Event.OrganizerID).First(&organizer).Error; err == nil {
// 			refund.PaymentIntent.Event.Organizer = &organizer
// 		}
// 	}

// 	response := s.convertRefundToDetailResponse(&refund)
// 	return &response, nil
// }

// // AdminGetRefundStatusHistory retrieves refund status history for any refund (admin access)
// func (s *PaymentService) AdminGetRefundStatusHistory(ctx context.Context, refundID uuid.UUID) ([]models.RefundStatusHistoryResponse, error) {
// 	var response []models.RefundStatusHistoryResponse

// 	// Check if the ID is a Refund ID
// 	var refund models.Refund
// 	if err := s.db.Where("id = ?", refundID).Preload("Transaction").Preload("PaymentIntent").First(&refund).Error; err == nil {
// 		// It's a Refund ID, get its status history
// 		var history []models.RefundStatusHistory
// 		if err := s.db.Where("refund_id = ?", refundID).
// 			Order("changed_at DESC").
// 			Preload("ChangedBy").
// 			Find(&history).Error; err != nil {
// 			return nil, fmt.Errorf("failed to fetch refund status history: %w", err)
// 		}

// 		response = make([]models.RefundStatusHistoryResponse, len(history))
// 		for i, h := range history {
// 			response[i] = models.RefundStatusHistoryResponse{
// 				ID:            h.ID,
// 				RefundID:      h.RefundID,
// 				OldStatus:     h.OldStatus,
// 				NewStatus:     h.NewStatus,
// 				ChangedByID:   h.ChangedByID,
// 				ChangedByType: h.ChangedByType,
// 				Remarks:       h.Remarks,
// 				Metadata:      h.Metadata,
// 				ChangedAt:     h.ChangedAt,
// 			}

// 			// Add changed by user info if available
// 			if h.ChangedBy != nil {
// 				response[i].ChangedBy = &models.UserSummary{
// 					ID:    h.ChangedBy.ID,
// 					Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
// 					Email: h.ChangedBy.Email,
// 				}
// 			}
// 		}
// 		return response, nil
// 	}

// 	// Check if the ID is a RefundRequest ID
// 	var refundRequest models.RefundRequest
// 	if err := s.db.Where("id = ?", refundID).
// 		Preload("Transaction").
// 		Preload("ProcessedBy").
// 		First(&refundRequest).Error; err == nil {
// 		// It's a RefundRequest ID, create synthetic history
// 		response = []models.RefundStatusHistoryResponse{}

// 		// Add creation entry
// 		response = append(response, models.RefundStatusHistoryResponse{
// 			ID:            uuid.New(), // Synthetic ID
// 			RefundID:      refundRequest.ID,
// 			OldStatus:     "",
// 			NewStatus:     "pending",
// 			ChangedByID:   refundRequest.UserID,
// 			ChangedByType: "user",
// 			Remarks:       "Refund request created",
// 			Metadata:      nil,
// 			ChangedAt:     refundRequest.CreatedAt,
// 		})

// 		// Add processing entry if processed
// 		if refundRequest.Status != "pending" {
// 			changedByType := "admin"
// 			var changedByID *uuid.UUID
// 			var remarks string

// 			if refundRequest.Status == "approved" {
// 				remarks = "Refund request approved"
// 				changedByID = refundRequest.ProcessedByID
// 			} else if refundRequest.Status == "rejected" {
// 				remarks = "Refund request rejected"
// 				changedByID = refundRequest.ProcessedByID
// 			}

// 			response = append(response, models.RefundStatusHistoryResponse{
// 				ID:            uuid.New(), // Synthetic ID
// 				RefundID:      refundRequest.ID,
// 				OldStatus:     "pending",
// 				NewStatus:     refundRequest.Status,
// 				ChangedByID:   changedByID,
// 				ChangedByType: changedByType,
// 				Remarks:       remarks,
// 				Metadata:      nil,
// 				ChangedAt:     *refundRequest.ProcessedAt,
// 			})
// 		}

// 		// If approved, also include the actual refund status history
// 		if refundRequest.Status == "approved" {
// 			// Find the associated refund
// 			var refund models.Refund
// 			if err := s.db.Where("transaction_id = ?", refundRequest.TransactionID).
// 				Where("status != 'failed'").
// 				Order("created_at DESC").
// 				First(&refund).Error; err == nil {
// 				var refundHistory []models.RefundStatusHistory
// 				if err := s.db.Where("refund_id = ?", refund.ID).
// 					Order("changed_at DESC").
// 					Preload("ChangedBy").
// 					Find(&refundHistory).Error; err == nil {
// 					for _, h := range refundHistory {
// 						response = append(response, models.RefundStatusHistoryResponse{
// 							ID:            h.ID,
// 							RefundID:      h.RefundID,
// 							OldStatus:     h.OldStatus,
// 							NewStatus:     h.NewStatus,
// 							ChangedByID:   h.ChangedByID,
// 							ChangedByType: h.ChangedByType,
// 							Remarks:       h.Remarks,
// 							Metadata:      h.Metadata,
// 							ChangedAt:     h.ChangedAt,
// 						})

// 						// Add changed by user info if available
// 						if h.ChangedBy != nil {
// 							response[len(response)-1].ChangedBy = &models.UserSummary{
// 								ID:    h.ChangedBy.ID,
// 								Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
// 								Email: h.ChangedBy.Email,
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}

// 		return response, nil
// 	}

// 	// ID not found in either table
// 	return nil, fmt.Errorf("refund not found")
// }

// // AdminInitiateRefund allows admins to create refunds directly without user request
// func (s *PaymentService) AdminInitiateRefund(ctx context.Context, paymentIntentID, adminID uuid.UUID, amount float64, reason string, ticketIDs []uuid.UUID, refundType string) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var paymentIntent models.PaymentIntent
// 	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
// 		// Try with Unscoped if not found (might be soft-deleted)
// 		if err = tx.Unscoped().First(&paymentIntent, paymentIntentID).Error; err != nil {
// 			tx.Rollback()
// 			return nil, fmt.Errorf("payment intent not found: %w", err)
// 		}
// 	}

// 	// Only succeeded payments can be refunded
// 	if paymentIntent.Status != "succeeded" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
// 	}

// 	// Validate refund conditions
// 	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not allowed: %w", err)
// 	}

// 	// For admin refunds, use the provided amount or calculate based on tickets
// 	refundAmount := amount
// 	if refundAmount == 0 && len(ticketIDs) > 0 {
// 		refundAmount = float64(paymentIntent.AmountTotal) * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity)) / 100
// 	} else if refundAmount == 0 {
// 		refundAmount = float64(paymentIntent.AmountTotal) / 100 // Full refund if no tickets specified
// 	}

// 	// Validate refund amount doesn't exceed payment amount
// 	if refundAmount > float64(paymentIntent.AmountTotal)/100 {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Refund amount cannot exceed payment amount")
// 	}

// 	// Find the transaction ID associated with this payment intent
// 	var transaction models.Transaction
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&transaction).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("transaction not found for payment intent: %w", err)
// 	}

// 	// Capture charge ID at creation time (don't rely on fetching PaymentIntent later)
// 	gatewayMetadata := map[string]interface{}{}

// 	// Get the charge ID from PaymentAttempt
// 	var paymentAttempt models.PaymentAttempt
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&paymentAttempt).Error; err == nil {
// 		if paymentAttempt.ProviderChargeID != "" {
// 			gatewayMetadata["stripe_charge_id"] = paymentAttempt.ProviderChargeID
// 		}
// 	} else {
// 		log.Printf("[REFUND] Warning: No payment attempt found for payment intent %s. Refund processing may fail.", paymentIntentID)
// 	}

// 	refund := &models.Refund{
// 		PaymentIntentID:  paymentIntentID,
// 		TransactionID:    transaction.ID,       // Set the transaction ID
// 		Provider:         transaction.Provider, // Use provider from transaction
// 		ProviderRefundID: "",                   // Will be set when processed
// 		Amount:           int64(amount * 100),  // Convert to cents
// 		Currency:         paymentIntent.Currency,
// 		Reason:           reason,
// 		RefundType:       refundType,
// 		Status:           "pending", // Start as pending - will be approved by admin via ApproveRefund
// 		InitiatedBy:      &adminID,
// 		ProviderData:     gatewayMetadata, // Store charge ID for later processing
// 		AffectedTicketIDs: func() []string {
// 			ids := make([]string, len(ticketIDs))
// 			for i, id := range ticketIDs {
// 				ids[i] = id.String()
// 			}
// 			return ids
// 		}(),
// 		TicketCount: len(ticketIDs),
// 		RequestedAt: &time.Time{}, // Set to current time
// 	}

// 	now := time.Now()
// 	refund.RequestedAt = &now
// 	refund.ApprovedAt = &now

// 	if err := tx.Create(refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create admin refund: %w", err)
// 	}

// 	// Log initial status
// 	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "approved", &adminID, "admin", "Admin refund created and auto-approved", nil); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to log initial status change: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit admin refund: %w", err)
// 	}

// 	// Log audit with event_id
// 	s.logAudit(ctx, "refund_initiated_by_admin", "refund", refund.ID, &adminID, "admin", &paymentIntent.EventID, map[string]interface{}{})

// 	// Process the refund immediately since it's admin-approved
// 	if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
// 		// Update status to failed if processing fails
// 		refund.Status = "failed"
// 		s.db.Save(refund)
// 		return nil, fmt.Errorf("failed to process refund: %w", err)
// 	}

// 	return refund, nil
// }

// // AdminRefundFullTransaction allows admins to refund an entire transaction (all tickets)
// func (s *PaymentService) AdminRefundFullTransaction(ctx context.Context, transactionID, adminID uuid.UUID, reason string, refundType string) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var transaction models.Transaction
// 	if err := tx.Preload("PaymentIntent").First(&transaction, transactionID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("transaction not found: %w", err)
// 	}

// 	// Only completed transactions can be fully refunded
// 	if transaction.Status != "completed" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Only completed transactions can be fully refunded")
// 	}

// 	// Get all tickets for this transaction
// 	var tickets []models.Ticket
// 	if err := tx.Where("transaction_id = ?", transactionID).Find(&tickets).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to fetch transaction tickets: %w", err)
// 	}

// 	if len(tickets) == 0 {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("No tickets found for this transaction")
// 	}

// 	// Check if any tickets are already refunded
// 	var activeTickets []models.Ticket
// 	var affectedTicketIDs []uuid.UUID
// 	for _, ticket := range tickets {
// 		if ticket.Status != "refunded" {
// 			activeTickets = append(activeTickets, ticket)
// 			affectedTicketIDs = append(affectedTicketIDs, ticket.ID)
// 		}
// 	}

// 	if len(activeTickets) == 0 {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("All tickets in this transaction are already refunded")
// 	}

// 	// Calculate refund amount from active tickets
// 	refundAmount := 0.0
// 	for _, ticket := range activeTickets {
// 		refundAmount += ticket.TotalAmount
// 	}

// 	// Create the full transaction refund
// 	refund := &models.Refund{
// 		PaymentIntentID:  transaction.PaymentIntentID, // No longer a pointer
// 		TransactionID:    transactionID,
// 		Provider:         transaction.Provider,
// 		ProviderRefundID: "",                        // Will be set when processed
// 		Amount:           int64(refundAmount * 100), // Convert to cents
// 		Currency:         transaction.Currency,
// 		Reason:           reason,
// 		RefundType:       refundType,
// 		Status:           "approved", // Admin refunds are auto-approved
// 		InitiatedBy:      &adminID,
// 		ApprovedBy:       &adminID,
// 		AffectedTicketIDs: func() []string {
// 			ids := make([]string, len(affectedTicketIDs))
// 			for i, id := range affectedTicketIDs {
// 				ids[i] = id.String()
// 			}
// 			return ids
// 		}(),
// 		TicketCount:             len(affectedTicketIDs),
// 		IsFullTransactionRefund: true, // Mark as full transaction refund
// 		RequestedAt:             &time.Time{},
// 		ApprovedAt:              &time.Time{},
// 	}

// 	now := time.Now()
// 	refund.RequestedAt = &now
// 	refund.ApprovedAt = &now

// 	if err := tx.Create(refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create full transaction refund: %w", err)
// 	}

// 	// Log initial status
// 	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "approved", &adminID, "admin", "Full transaction refund created and auto-approved", nil); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to log initial status change: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit full transaction refund: %w", err)
// 	}

// 	// Log audit with event_id
// 	s.logAudit(ctx, "full_transaction_refund_initiated", "refund", refund.ID, &adminID, "admin", &transaction.PaymentIntent.EventID, map[string]interface{}{})

// 	// Process the refund immediately since it's admin-approved
// 	if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
// 		// Update status to failed if processing fails
// 		refund.Status = "failed"
// 		s.db.Save(refund)
// 		return nil, fmt.Errorf("failed to process full transaction refund: %w", err)
// 	}

// 	return refund, nil
// }

// // AdminRefundEventTickets refunds all eligible tickets for an event (event cancellation scenario)
// func (s *PaymentService) AdminRefundEventTickets(ctx context.Context, eventID, adminID uuid.UUID, reason string, refundType string) (map[string]interface{}, error) {
// 	// Get all eligible tickets for the event
// 	var tickets []models.Ticket
// 	if err := s.db.Where("event_id = ? AND status IN (?) AND check_in_time IS NULL", eventID, []string{"active", "confirmed"}).
// 		Preload("Transaction").
// 		Preload("Event").
// 		Find(&tickets).Error; err != nil {
// 		return nil, fmt.Errorf("failed to fetch event tickets: %w", err)
// 	}

// 	if len(tickets) == 0 {
// 		return map[string]interface{}{
// 			"message":          "No eligible tickets found for refund",
// 			"eligible_tickets": 0,
// 		}, nil
// 	}

// 	// Group tickets by transaction/payment_intent
// 	ticketsByTransaction := make(map[uuid.UUID][]models.Ticket)
// 	totalRefundAmount := 0.0
// 	eligibleTicketIDs := make([]uuid.UUID, 0, len(tickets))

// 	for _, ticket := range tickets {
// 		if ticket.TransactionID != nil {
// 			ticketsByTransaction[*ticket.TransactionID] = append(ticketsByTransaction[*ticket.TransactionID], ticket)
// 			totalRefundAmount += ticket.TotalAmount
// 			eligibleTicketIDs = append(eligibleTicketIDs, ticket.ID)
// 		}
// 	}

// 	// Create refunds for each transaction
// 	createdRefunds := 0
// 	totalRefundedAmount := 0.0

// 	for transactionID, transactionTickets := range ticketsByTransaction {
// 		// Calculate refund amount for this transaction
// 		transactionRefundAmount := 0.0
// 		transactionTicketIDs := make([]uuid.UUID, 0, len(transactionTickets))

// 		for _, ticket := range transactionTickets {
// 			transactionRefundAmount += ticket.TotalAmount
// 			transactionTicketIDs = append(transactionTicketIDs, ticket.ID)
// 		}

// 		// Get payment intent for this transaction
// 		var transaction models.Transaction
// 		if err := s.db.First(&transaction, transactionID).Error; err != nil {
// 			continue // Skip if transaction not found
// 		}

// 		// Create refund for this transaction
// 		refund := &models.Refund{
// 			PaymentIntentID:  transaction.PaymentIntentID, // No longer a pointer
// 			TransactionID:    transactionID,
// 			Provider:         transaction.Provider,                 // Use provider from transaction
// 			ProviderRefundID: "",                                   // Will be set when processed
// 			Amount:           int64(transactionRefundAmount * 100), // Convert to cents
// 			Currency:         transaction.Currency,
// 			Reason:           reason,
// 			RefundType:       refundType,
// 			Status:           "approved", // Admin refunds are auto-approved
// 			InitiatedBy:      &adminID,
// 			ApprovedBy:       &adminID,
// 			AffectedTicketIDs: func() []string {
// 				ids := make([]string, len(transactionTicketIDs))
// 				for i, id := range transactionTicketIDs {
// 					ids[i] = id.String()
// 				}
// 				return ids
// 			}(),
// 			TicketCount: len(transactionTicketIDs),
// 		}

// 		now := time.Now()
// 		refund.RequestedAt = &now
// 		refund.ApprovedAt = &now

// 		if err := s.db.Create(refund).Error; err != nil {
// 			return nil, fmt.Errorf("failed to create refund for transaction %s: %w", transactionID, err)
// 		}

// 		// Log initial status
// 		if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "", "approved", &adminID, "admin", "Event ticket refund created and auto-approved", nil); err != nil {
// 			log.Printf("[REFUND] Warning: Failed to log initial status change for event refund %s: %v", refund.ID, err)
// 		}

// 		// Process the refund immediately
// 		if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
// 			// Update status to failed if processing fails
// 			refund.Status = "failed"
// 			s.db.Save(refund)

// 			// Log status change for processing failure
// 			if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "approved", "failed", nil, "system", fmt.Sprintf("Event refund processing failed: %s", err.Error()), map[string]interface{}{
// 				"error": err.Error(),
// 			}); logErr != nil {
// 				log.Printf("[REFUND] Warning: Failed to log processing failure status change: %v", logErr)
// 			}

// 			return nil, fmt.Errorf("failed to process refund for transaction %s: %w", transactionID, err)
// 		}

// 		createdRefunds++
// 		totalRefundedAmount += transactionRefundAmount

// 		// Mark tickets as refunded
// 		for _, ticketID := range transactionTicketIDs {
// 			s.db.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "refunded")
// 		}

// 		// Send event cancellation email notification
// 		go func(txID uuid.UUID, txTickets []models.Ticket, refundAmt float64) {
// 			if err := s.sendEventCancellationEmail(ctx, txID, txTickets, refundAmt); err != nil {
// 				log.Printf("Failed to send event cancellation email for transaction %s: %v", txID, err)
// 			}
// 		}(transactionID, transactionTickets, transactionRefundAmount)
// 	}

// 	return map[string]interface{}{
// 		"message":                fmt.Sprintf("Successfully processed refunds for %d transactions", createdRefunds),
// 		"total_tickets_refunded": len(eligibleTicketIDs),
// 		"total_refund_amount":    totalRefundAmount,
// 		"total_refunded_amount":  totalRefundedAmount,
// 		"refunds_created":        createdRefunds,
// 	}, nil
// }

// // sendEventCancellationEmail sends event cancellation notification with refund details
// func (s *PaymentService) sendEventCancellationEmail(ctx context.Context, transactionID uuid.UUID, tickets []models.Ticket, refundAmount float64) error {
// 	if len(tickets) == 0 {
// 		return fmt.Errorf("no tickets provided for email notification")
// 	}

// 	// Get transaction details
// 	var transaction models.Transaction
// 	if err := s.db.First(&transaction, transactionID).Error; err != nil {
// 		return fmt.Errorf("failed to get transaction details: %w", err)
// 	}

// 	// Get user information
// 	var userEmail, userName string
// 	if tickets[0].UserID != nil {
// 		var user models.User
// 		if err := s.db.First(&user, *tickets[0].UserID).Error; err != nil {
// 			return fmt.Errorf("failed to get user details: %w", err)
// 		}
// 		userEmail = user.Email
// 		userName = user.FirstName + " " + user.LastName
// 	} else if tickets[0].GuestUserID != nil {
// 		var guestUser models.GuestUser
// 		if err := s.db.First(&guestUser, *tickets[0].GuestUserID).Error; err != nil {
// 			return fmt.Errorf("failed to get guest user details: %w", err)
// 		}
// 		userEmail = guestUser.Email
// 		userName = guestUser.FirstName + " " + guestUser.LastName
// 	} else {
// 		return fmt.Errorf("no user or guest user associated with tickets")
// 	}

// 	// Get event details
// 	event := tickets[0].Event
// 	organizerName := event.Organizer.FirstName + " " + event.Organizer.LastName

// 	// Format event date
// 	eventDate := ""
// 	if !event.StartDate.IsZero() {
// 		eventDate = event.StartDate.Format("January 2, 2006 at 3:04 PM")
// 	}

// 	// Prepare email template data
// 	templateData := map[string]interface{}{
// 		"user_name":      userName,
// 		"event_name":     event.Title,
// 		"organizer_name": organizerName,
// 		"refund_amount":  refundAmount,
// 		"currency":       transaction.Currency,
// 		"ticket_count":   len(tickets),
// 		"transaction_id": transactionID.String(),
// 		"event_date":     eventDate,
// 		"event_location": event.Location,
// 		"completed_at":   time.Now().Format("January 2, 2006 at 3:04 PM"),
// 	}

// 	// Queue the email
// 	subject := fmt.Sprintf("Event Cancelled - %s", event.Title)
// 	priority := 2 // High priority for event cancellations

// 	if err := s.emailOutboxService.QueueEmail(ctx, models.EmailEventEventCancellation, userEmail, subject, templateData, priority); err != nil {
// 		return fmt.Errorf("failed to queue event cancellation email: %w", err)
// 	}

// 	log.Printf("✓ Event cancellation email queued for %s: %s", userEmail, event.Title)
// 	return nil
// }

// ApproveRefund approves and processes a refund through the payment gateway
// func (s *PaymentService) ApproveRefund(ctx context.Context, refundID, adminID uuid.UUID) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var refund models.Refund
// 	// Preload PaymentIntent with all relationships
// 	if err := tx.Preload("PaymentIntent").First(&refund, refundID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not found: %w", err)
// 	}

// 	// Validate that PaymentIntent is loaded
// 	if refund.PaymentIntent == nil || refund.PaymentIntent.ID == uuid.Nil {
// 		// If PaymentIntent wasn't loaded, try to load it explicitly
// 		var pi models.PaymentIntent
// 		if err := tx.First(&pi, refund.PaymentIntentID).Error; err != nil {
// 			if err = tx.Unscoped().First(&pi, refund.PaymentIntentID).Error; err != nil {
// 				tx.Rollback()
// 				return nil, fmt.Errorf("payment intent not found for refund: %w", err)
// 			}
// 		}
// 		refund.PaymentIntent = &pi
// 	}

// 	if refund.Status != "pending" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
// 	}

// 	// Update status to processing
// 	oldStatus := refund.Status
// 	refund.Status = "processing"
// 	refund.ApprovedBy = &adminID
// 	now := time.Now()
// 	refund.ApprovedAt = &now

// 	if err := tx.Save(&refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to update refund status to processing: %w", err)
// 	}

// 	// Log status change
// 	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, oldStatus, "processing", &adminID, "admin", "Refund approved and moved to processing", nil); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to log status change: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit status update: %w", err)
// 	}

// 	// Process refund through payment gateway asynchronously
// 	go func() {
// 		if err := s.processGatewayRefund(context.Background(), &refund, &adminID); err != nil {
// 			log.Printf("[REFUND] Failed to process gateway refund %s: %v", refund.ID, err)
// 			// Update refund status to failed
// 			s.db.Model(&refund).Updates(map[string]interface{}{
// 				"status":    "failed",
// 				"failed_at": time.Now(),
// 				"gateway_response": map[string]interface{}{
// 					"error": err.Error(),
// 				},
// 			})

// 			// Log status change for gateway processing failure
// 			if logErr := s.LogRefundStatusChange(context.Background(), nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Gateway refund processing failed: %s", err.Error()), map[string]interface{}{
// 				"error": err.Error(),
// 			}); logErr != nil {
// 				log.Printf("[REFUND] Warning: Failed to log gateway failure status change: %v", logErr)
// 			}
// 		}
// 	}()

// 	// Send refund approved notification email
// 	go func() {
// 		// Use the already-loaded PaymentIntent
// 		if refund.PaymentIntent != nil {
// 			if err := s.notifyUserRefundCompleted(context.Background(), refund.PaymentIntent, &refund, "processing"); err != nil {
// 				log.Printf("[REFUND] Warning: Failed to notify user about refund approval: %v", err)
// 			}
// 		} else {
// 			log.Printf("[REFUND] Warning: PaymentIntent not available for refund notification")
// 		}
// 	}()

// 	// Get event ID for audit logging from the loaded PaymentIntent
// 	var eventID *uuid.UUID
// 	if refund.PaymentIntent != nil && refund.PaymentIntent.ID != uuid.Nil {
// 		eventID = &refund.PaymentIntent.EventID
// 	}

// 	s.logAudit(ctx, "refund_approved", "refund", refund.ID, &adminID, "admin", eventID, map[string]interface{}{})

// 	// Return refund with processing status
// 	return &refund, nil
// }

// // processGatewayRefund handles the actual gateway refund processing
// func (s *PaymentService) processGatewayRefund(ctx context.Context, refund *models.Refund, adminID *uuid.UUID) error {
// 	// Only process for Stripe payments
// 	if refund.PaymentGateway != "stripe" {
// 		// For non-Stripe payments, mark as succeeded without gateway processing
// 		s.db.Model(refund).Updates(map[string]interface{}{
// 			"status":       "succeeded",
// 			"processed_at": time.Now(),
// 		})

// 		// Log status change for non-Stripe refund
// 		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "succeeded", adminID, "admin", "Refund processed for non-Stripe payment", nil); logErr != nil {
// 			log.Printf("[REFUND] Warning: Failed to log non-Stripe refund status change: %v", logErr)
// 		}

// 		// Restore refunded ticket inventory and update event availability immediately for non-Stripe refunds
// 		if s.ticketService != nil {
// 			refundedTicketIDs := parseTicketIDsFromGatewayData(refund.AffectedTicketIDs)
// 			if len(refundedTicketIDs) > 0 {
// 				if err := s.ticketService.RestoreRefundedTicketInventoryByIDs(nil, refundedTicketIDs); err != nil {
// 					log.Printf("[REFUND] Warning: Failed to restore inventory for non-Stripe refund %s: %v", refund.ID, err)
// 				}
// 			}
// 		}

// 		return nil
// 	}

// 	// Get PaymentIntent to retrieve the charge ID
// 	// Try to find the payment intent, including soft-deleted records
// 	var chargeID string

// 	// First, try to use stored charge ID from ProviderData (captured at refund creation time)
// 	if refund.ProviderData != nil {
// 		if storedChargeID, exists := refund.ProviderData["stripe_charge_id"]; exists {
// 			if chargeIDStr, ok := storedChargeID.(string); ok && chargeIDStr != "" {
// 				chargeID = chargeIDStr
// 				log.Printf("[REFUND] Using stored charge ID from refund metadata: %s", chargeID)
// 			}
// 		}
// 	}

// 	// If no stored charge ID, try to fetch from PaymentIntent
// 	if chargeID == "" {
// 		var paymentIntent models.PaymentIntent
// 		if err := s.db.First(&paymentIntent, refund.PaymentIntentID).Error; err != nil {
// 			// If not found in regular query, try with Unscoped (includes soft-deleted records)
// 			if err := s.db.Unscoped().First(&paymentIntent, refund.PaymentIntentID).Error; err != nil {
// 				// Payment intent not found - this is a critical error
// 				// Could be: wrong payment_intent_id, transaction incomplete, or data mismatch
// 				errMsg := fmt.Sprintf("Payment intent %s not found in system. The payment may not have been completed successfully.", refund.PaymentIntentID)
// 				log.Printf("[REFUND] ERROR: %s", errMsg)
// 				s.db.Model(refund).Updates(map[string]interface{}{
// 					"status":         "failed",
// 					"failed_at":      time.Now(),
// 					"failure_reason": errMsg,
// 					"gateway_response": map[string]interface{}{
// 						"error":         "payment_intent_not_found",
// 						"error_details": errMsg,
// 					},
// 				})

// 				// Log status change for payment intent not found
// 				if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", adminID, "admin", fmt.Sprintf("Refund failed: %s", errMsg), map[string]interface{}{
// 					"error": "payment_intent_not_found",
// 				}); logErr != nil {
// 					log.Printf("[REFUND] Warning: Failed to log payment intent not found status change: %v", logErr)
// 				}

// 				return fmt.Errorf("%s", errMsg)
// 			}
// 			log.Printf("[REFUND] Warning: Payment intent %s was soft-deleted, using it for refund processing", paymentIntent.ID)
// 		}

// 		// Extract charge ID from PaymentAttempt
// 		var paymentAttempt models.PaymentAttempt
// 		if err := s.db.Where("payment_intent_id = ?", refund.PaymentIntentID).First(&paymentAttempt).Error; err == nil {
// 			if paymentAttempt.ProviderChargeID != "" {
// 				chargeID = paymentAttempt.ProviderChargeID
// 				log.Printf("[REFUND] Using charge ID from payment attempt: %s", chargeID)
// 			}
// 		} else {
// 			log.Printf("[REFUND] Warning: No payment attempt found for payment intent %s", refund.PaymentIntentID)
// 		}
// 	}

// 	// Validate charge ID exists (required for Stripe refunds)
// 	if chargeID == "" {
// 		errMsg := fmt.Sprintf("Refund cannot be processed: Stripe charge ID missing or empty. Payment may be incomplete or in pending state. Payment Intent ID: %s", refund.PaymentIntentID)
// 		log.Printf("[REFUND] ERROR: %s", errMsg)
// 		s.db.Model(refund).Updates(map[string]interface{}{
// 			"status":         "failed",
// 			"failed_at":      time.Now(),
// 			"failure_reason": errMsg,
// 			"gateway_response": map[string]interface{}{
// 				"error":         "missing_charge_id",
// 				"error_details": errMsg,
// 			},
// 		})
// 		return fmt.Errorf("%s", errMsg)
// 	}

// 	// Call Stripe gateway to create refund
// 	gatewayRefundReq := &gateways.RefundRequest{
// 		ChargeID: chargeID, // Pass the Stripe charge ID (ch_xxx)
// 		Amount:   refund.Amount,
// 		Currency: refund.Currency,
// 		Reason:   s.mapRefundReasonToStripe(refund.Reason, refund.RefundType), // Map to Stripe's valid reasons
// 		Metadata: map[string]string{
// 			"refund_id":     refund.ID.String(),
// 			"refund_number": refund.RefundNumber,
// 		},
// 	}

// 	// Get the gateway implementation
// 	gateway := s.getPaymentGateway("stripe")
// 	if gateway == nil {
// 		return fmt.Errorf("stripe gateway not configured")
// 	}

// 	// Create refund on Stripe
// 	gatewayResponse, err := gateway.CreateRefund(ctx, gatewayRefundReq)
// 	if err != nil {
// 		// Determine user-friendly and admin-friendly error messages
// 		userMsg := "The refund could not be processed by the payment gateway. Please contact support."
// 		adminMsg := err.Error()

// 		if strings.Contains(err.Error(), "amount") {
// 			userMsg = "Refund amount is invalid or exceeds the original payment amount."
// 		} else if strings.Contains(err.Error(), "charge") {
// 			userMsg = "Payment charge information is not available for refunding."
// 		} else if strings.Contains(err.Error(), "already") {
// 			userMsg = "This payment has already been refunded."
// 		} else if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "network") {
// 			userMsg = "Payment gateway is temporarily unavailable. Please try again in a few minutes."
// 		}

// 		// Mark refund as failed and send notification
// 		failedAt := time.Now()
// 		s.db.Model(refund).Updates(map[string]interface{}{
// 			"status":         "failed",
// 			"failed_at":      failedAt,
// 			"failure_reason": userMsg,
// 			"gateway_response": map[string]interface{}{
// 				"error":               adminMsg,
// 				"user_facing_message": userMsg,
// 				"admin_debug_message": adminMsg,
// 				"failed_at":           failedAt,
// 				"charge_id":           chargeID,
// 			},
// 		})

// 		// Log status change for gateway refund failure
// 		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", adminID, "admin", fmt.Sprintf("Gateway refund failed: %s", userMsg), map[string]interface{}{
// 			"error":     adminMsg,
// 			"charge_id": chargeID,
// 		}); logErr != nil {
// 			log.Printf("[REFUND] Warning: Failed to log gateway refund failure status change: %v", logErr)
// 		}

// 		// Log status change
// 		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Refund failed: %s", adminMsg), map[string]interface{}{
// 			"error":               adminMsg,
// 			"user_facing_message": userMsg,
// 			"charge_id":           chargeID,
// 		}); logErr != nil {
// 			log.Printf("[REFUND] Warning: Failed to log status change: %v", logErr)
// 		}

// 		// Audit log for failed refund
// 		var auditPI models.PaymentIntent
// 		if err := s.db.First(&auditPI, refund.PaymentIntentID).Error; err != nil {
// 			s.db.Unscoped().First(&auditPI, refund.PaymentIntentID)
// 		}
// 		s.logAudit(ctx, "refund_failed", "refund", refund.ID, adminID, "admin", &auditPI.EventID, map[string]interface{}{
// 			"error":               adminMsg,
// 			"user_facing_message": userMsg,
// 			"failed_at":           failedAt,
// 			"charge_id":           chargeID,
// 		})

// 		// Send failed refund notification
// 		go func() {
// 			// Try to load payment intent for notification
// 			var pi models.PaymentIntent
// 			if loadErr := s.db.First(&pi, refund.PaymentIntentID).Error; loadErr != nil {
// 				// Try with Unscoped if not found
// 				if loadErr = s.db.Unscoped().First(&pi, refund.PaymentIntentID).Error; loadErr != nil {
// 					log.Printf("[REFUND] Warning: Could not load payment intent for refund notification: %v", loadErr)
// 					return
// 				}
// 			}
// 			if err := s.notifyUserRefundCompleted(context.Background(), &pi, refund, "failed"); err != nil {
// 				log.Printf("[REFUND] Warning: Failed to notify user about refund failure: %v", err)
// 			}
// 		}()

// 		return fmt.Errorf("stripe refund creation failed: %w", err)
// 	}

// 	// Update refund with gateway response - set to processing, wait for webhook confirmation
// 	refund.GatewayRefundID = gatewayResponse.GatewayRefundID
// 	refund.Status = "processing" // Wait for webhook confirmation instead of immediately marking as succeeded
// 	now := time.Now()
// 	refund.ProcessedAt = &now

// 	// Store the full gateway response
// 	gatewayData := map[string]interface{}{
// 		"gateway_refund_id": gatewayResponse.GatewayRefundID,
// 		"gateway_status":    gatewayResponse.Status,
// 		"amount":            gatewayResponse.Amount,
// 		"currency":          gatewayResponse.Currency,
// 		"created_at":        gatewayResponse.CreatedAt,
// 		"awaiting_webhook":  true, // Flag to indicate we're waiting for webhook confirmation
// 	}

// 	if err := s.db.Model(refund).Updates(map[string]interface{}{
// 		"gateway_refund_id": refund.GatewayRefundID,
// 		"status":            "processing",
// 		"processed_at":      refund.ProcessedAt,
// 		"gateway_response":  gatewayData,
// 	}).Error; err != nil {
// 		return fmt.Errorf("failed to update refund with gateway response: %w", err)
// 	}

// 	// Log status change - still processing, awaiting webhook
// 	if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "processing", nil, "system", "Refund initiated via payment gateway, awaiting webhook confirmation", map[string]interface{}{
// 		"gateway_refund_id": gatewayResponse.GatewayRefundID,
// 		"amount":            gatewayResponse.Amount,
// 		"awaiting_webhook":  true,
// 	}); err != nil {
// 		log.Printf("[REFUND] Warning: Failed to log status change: %v", err)
// 	}

// 	// Audit log for successful refund
// 	var succeedPI models.PaymentIntent
// 	if err := s.db.First(&succeedPI, refund.PaymentIntentID).Error; err != nil {
// 		s.db.Unscoped().First(&succeedPI, refund.PaymentIntentID)
// 	}
// 	s.logAudit(ctx, "refund_succeeded", "refund", refund.ID, adminID, "admin", &succeedPI.EventID, map[string]interface{}{
// 		"gateway_refund_id": gatewayResponse.GatewayRefundID,
// 		"amount":            gatewayResponse.Amount,
// 		"currency":          gatewayResponse.Currency,
// 	})

// 	log.Printf("[REFUND] Refund %s processed successfully. Stripe Refund ID: %s", refund.RefundNumber, gatewayResponse.GatewayRefundID)

// 	// Update payment intent status to refunded/partially_refunded
// 	var pi models.PaymentIntent
// 	if err := s.db.First(&pi, refund.PaymentIntentID).Error; err != nil {
// 		// Try with Unscoped if not found
// 		if err = s.db.Unscoped().First(&pi, refund.PaymentIntentID).Error; err != nil {
// 			log.Printf("[REFUND] Warning: Could not load payment intent to update status: %v", err)
// 		} else if err := s.updatePaymentIntentRefundStatus(&pi, refund); err != nil {
// 			log.Printf("[REFUND] Warning: Failed to update payment intent status: %v", err)
// 		}
// 	} else if err := s.updatePaymentIntentRefundStatus(&pi, refund); err != nil {
// 		log.Printf("[REFUND] Warning: Failed to update payment intent status: %v", err)
// 	}

// 	// Notify user about refund success
// 	go func() {
// 		var notifyPi models.PaymentIntent
// 		if err := s.db.First(&notifyPi, refund.PaymentIntentID).Error; err != nil {
// 			// Try with Unscoped
// 			if err = s.db.Unscoped().First(&notifyPi, refund.PaymentIntentID).Error; err != nil {
// 				log.Printf("[REFUND] Warning: Could not load payment intent for refund notification: %v", err)
// 				return
// 			}
// 		}
// 		if err := s.notifyUserRefundCompleted(context.Background(), &notifyPi, refund, "succeeded"); err != nil {
// 			log.Printf("[REFUND] Warning: Failed to notify user: %v", err)
// 		}
// 	}()

// 	return nil
// }

// // RetryFailedRefund retries a failed refund
// func (s *PaymentService) RetryFailedRefund(ctx context.Context, refundID uuid.UUID, adminID uuid.UUID) (*models.Refund, error) {
// 	var refund models.Refund
// 	if err := s.db.First(&refund, refundID).Error; err != nil {
// 		return nil, fmt.Errorf("refund not found: %w", err)
// 	}

// 	if refund.Status != "failed" {
// 		return nil, utils.NewBusinessLogicError("Only failed refunds can be retried")
// 	}

// 	// Ensure we have the charge ID from GatewayMetadata
// 	var chargeID string
// 	if refund.ProviderData != nil {
// 		if storedChargeID, exists := refund.ProviderData["stripe_charge_id"]; exists {
// 			if chargeIDStr, ok := storedChargeID.(string); ok && chargeIDStr != "" {
// 				chargeID = chargeIDStr
// 				log.Printf("[REFUND] Retry: Using stored charge ID from refund metadata: %s", chargeID)
// 			}
// 		}
// 	}

// 	if chargeID == "" {
// 		return nil, utils.NewBusinessLogicError("Cannot retry refund: Stripe charge ID not found. The original payment may not have been completed successfully.")
// 	}

// 	// Check retry attempts (store in gateway_response)
// 	var retryCount int = 0
// 	if refund.ProviderData != nil {
// 		if count, ok := refund.ProviderData["retry_count"].(float64); ok {
// 			retryCount = int(count)
// 		}
// 	}

// 	// Max 3 retry attempts
// 	if retryCount >= 3 {
// 		return nil, utils.NewBusinessLogicError("Maximum retry attempts (3) exceeded. Please contact support.")
// 	}

// 	// Mark as processing and retry
// 	if err := s.db.Model(&refund).Update("status", "processing").Error; err != nil {
// 		return nil, fmt.Errorf("failed to update refund status: %w", err)
// 	}

// 	// Log status change
// 	if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "failed", "processing", &adminID, "admin", fmt.Sprintf("Refund retry initiated (attempt %d)", retryCount+1), nil); err != nil {
// 		log.Printf("[REFUND] Warning: Failed to log status change: %v", err)
// 	}

// 	// Reload refund to get fresh data from database (especially GatewayMetadata with stored charge_id)
// 	if err := s.db.First(&refund, refundID).Error; err != nil {
// 		log.Printf("[REFUND] Warning: Failed to reload refund after status update: %v", err)
// 		// Continue anyway with in-memory refund, which should still have GatewayMetadata
// 	}

// 	// Process gateway refund asynchronously
// 	go func() {
// 		if err := s.processGatewayRefund(context.Background(), &refund, &adminID); err != nil {
// 			log.Printf("[REFUND] Retry failed for refund %s (attempt %d): %v", refund.ID, retryCount+1, err)
// 			// Update retry count
// 			newRetryCount := retryCount + 1
// 			response := refund.ProviderData
// 			if response == nil {
// 				response = make(map[string]interface{})
// 			}
// 			response["retry_count"] = newRetryCount
// 			response["last_retry_error"] = err.Error()
// 			response["last_retry_at"] = time.Now()

// 			s.db.Model(&refund).Updates(map[string]interface{}{
// 				"status":           "failed",
// 				"gateway_response": response,
// 				"failed_at":        time.Now(),
// 			})

// 			// Log status change for retry failure
// 			if logErr := s.LogRefundStatusChange(context.Background(), nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Refund retry failed (attempt %d): %s", newRetryCount, err.Error()), map[string]interface{}{
// 				"retry_attempt": newRetryCount,
// 				"error":         err.Error(),
// 			}); logErr != nil {
// 				log.Printf("[REFUND] Warning: Failed to log retry failure status change: %v", logErr)
// 			}

// 			// Notify admin about retry failure
// 			s.notifyAdminRefundFailed(context.Background(), &refund, newRetryCount)
// 		}
// 	}()

// 	return &refund, nil
// }

// // notifyUserRefundCompleted sends a notification to user about refund status
// func (s *PaymentService) notifyUserRefundCompleted(ctx context.Context, pi *models.PaymentIntent, refund *models.Refund, status string) error {
// 	// Get recipient email - works for both logged-in users and guests
// 	var recipientEmail string
// 	var userName string

// 	if pi.UserID != nil {
// 		// Logged-in user
// 		var user models.User
// 		if err := s.db.Where("id = ?", pi.UserID).First(&user).Error; err != nil {
// 			log.Printf("[REFUND] Warning: Could not find user %s for refund notification: %v", pi.UserID, err)
// 			return nil
// 		}
// 		recipientEmail = user.Email
// 		userName = user.FirstName + " " + user.LastName
// 	} else if pi.GuestUserID != nil {
// 		// Guest user - get email from GuestUser
// 		var guestUser models.GuestUser
// 		if err := s.db.Where("id = ?", pi.GuestUserID).First(&guestUser).Error; err != nil {
// 			log.Printf("[REFUND] Warning: Could not find guest user %s for refund notification: %v", pi.GuestUserID, err)
// 			return nil
// 		}
// 		recipientEmail = guestUser.Email
// 		userName = guestUser.FirstName + " " + guestUser.LastName
// 		if userName == "" {
// 			userName = "Valued Customer" // Default name for guests
// 		}
// 	} else {
// 		log.Printf("[REFUND] Warning: No user or guest user ID available for refund notification (payment_intent: %s)", pi.ID)
// 		return nil
// 	}

// 	// Get event title
// 	var event models.Event
// 	if err := s.db.Where("id = ?", pi.EventID).First(&event).Error; err != nil {
// 		log.Printf("[REFUND] Warning: Could not find event for refund notification: %v", err)
// 		return nil
// 	}

// 	// Prepare email subject and data based on status
// 	var subject string
// 	var priority int

// 	switch status {
// 	case "pending":
// 		subject = fmt.Sprintf("Refund Request Submitted - %s", refund.RefundNumber)
// 		priority = 2 // High priority
// 	case "processing":
// 		subject = fmt.Sprintf("Refund Approved - Processing Started - %s", refund.RefundNumber)
// 		priority = 2 // High priority
// 	case "succeeded":
// 		subject = fmt.Sprintf("Refund Completed Successfully - %s", refund.RefundNumber)
// 		priority = 2 // High priority
// 	case "failed":
// 		subject = fmt.Sprintf("Refund Processing Failed - %s", refund.RefundNumber)
// 		priority = 3 // Urgent priority
// 	default:
// 		log.Printf("[REFUND] Unknown refund status for email: %s", status)
// 		return nil
// 	}

// 	// Prepare template data for the centralized email system
// 	templateData := map[string]interface{}{
// 		"event_name":      event.Title,
// 		"refund_amount":   refund.Amount,
// 		"currency":        refund.Currency,
// 		"ticket_count":    1, // Default to 1 for single refund
// 		"refund_number":   refund.RefundNumber,
// 		"refund_reason":   refund.Reason,
// 		"refund_status":   status,
// 		"user_name":       userName,
// 		"recipient_email": recipientEmail,
// 	}

// 	// Add status-specific data
// 	if status == "succeeded" && refund.ProcessedAt != nil {
// 		templateData["completed_at"] = refund.ProcessedAt.Format("January 2, 2006 at 3:04 PM UTC")
// 	}
// 	if status == "failed" {
// 		templateData["error_message"] = "Processing error occurred during refund"
// 	}

// 	// Queue email using centralized system
// 	if err := s.emailOutboxService.QueueEmail(ctx, models.EmailEventRefundProcessed, recipientEmail, subject, templateData, priority); err != nil {
// 		log.Printf("[REFUND] Warning: Failed to queue refund email for %s: %v", recipientEmail, err)
// 		return err
// 	}

// 	log.Printf("[REFUND] Email notification queued for %s, refund %s, status: %s", recipientEmail, refund.ID, status)
// 	return nil
// }

// // notifyAdminRefundFailed notifies admin about refund retry failure
// func (s *PaymentService) notifyAdminRefundFailed(ctx context.Context, refund *models.Refund, retryCount int) {
// 	log.Printf("[REFUND] Alert: Refund %s failed on retry attempt %d. Manual intervention may be needed.", refund.ID, retryCount)
// }

// // BulkApproveRefunds approves multiple refunds at once (for event cancellations)
// func (s *PaymentService) BulkApproveRefunds(ctx context.Context, refundIDs []uuid.UUID, adminID uuid.UUID) (map[string]interface{}, error) {
// 	if len(refundIDs) == 0 {
// 		return nil, utils.NewBusinessLogicError("No refunds provided")
// 	}

// 	if len(refundIDs) > 500 {
// 		return nil, utils.NewBusinessLogicError("Maximum 500 refunds can be processed at once")
// 	}

// 	// Validate all refunds exist and are pending
// 	var refunds []models.Refund
// 	if err := s.db.Where("id IN ? AND status = ?", refundIDs, "pending").Find(&refunds).Error; err != nil {
// 		return nil, fmt.Errorf("failed to fetch refunds: %w", err)
// 	}

// 	if len(refunds) != len(refundIDs) {
// 		return nil, utils.NewBusinessLogicError("Some refunds not found or not in pending status")
// 	}

// 	// Start approval process for each refund
// 	successCount := 0
// 	var errors []string

// 	for _, refund := range refunds {
// 		if _, err := s.ApproveRefund(ctx, refund.ID, adminID); err != nil {
// 			errorMessage := fmt.Sprintf("Refund %s: %v", refund.RefundNumber, err)
// 			errors = append(errors, errorMessage)
// 		} else {
// 			successCount++
// 		}
// 	}

// 	return map[string]interface{}{
// 		"total":         len(refundIDs),
// 		"approved":      successCount,
// 		"failed":        len(errors),
// 		"error_details": errors,
// 	}, nil
// }

// // GetRefundAnalytics returns refund analytics and statistics
// func (s *PaymentService) GetRefundAnalytics(ctx context.Context, startDate, endDate time.Time) (map[string]interface{}, error) {
// 	var totalRefunds, successfulRefunds, failedRefunds, pendingRefunds int64
// 	var totalAmount, successfulAmount float64

// 	// Get total refunds count
// 	if err := s.db.Model(&models.Refund{}).
// 		Where("created_at BETWEEN ? AND ?", startDate, endDate).
// 		Count(&totalRefunds).Error; err != nil {
// 		return nil, err
// 	}

// 	// Get successful refunds
// 	if err := s.db.Model(&models.Refund{}).
// 		Where("status = ? AND created_at BETWEEN ? AND ?", "succeeded", startDate, endDate).
// 		Count(&successfulRefunds).Error; err != nil {
// 		return nil, err
// 	}

// 	// Get successful amount
// 	s.db.Model(&models.Refund{}).
// 		Where("status = ? AND created_at BETWEEN ? AND ?", "succeeded", startDate, endDate).
// 		Select("COALESCE(SUM(amount), 0)").
// 		Row().
// 		Scan(&successfulAmount)

// 	// Get failed refunds
// 	if err := s.db.Model(&models.Refund{}).
// 		Where("status = ? AND created_at BETWEEN ? AND ?", "failed", startDate, endDate).
// 		Count(&failedRefunds).Error; err != nil {
// 		return nil, err
// 	}

// 	// Get pending refunds
// 	if err := s.db.Model(&models.Refund{}).
// 		Where("status = ? AND created_at BETWEEN ? AND ?", "pending", startDate, endDate).
// 		Count(&pendingRefunds).Error; err != nil {
// 		return nil, err
// 	}

// 	// Get total amount
// 	s.db.Model(&models.Refund{}).
// 		Where("created_at BETWEEN ? AND ?", startDate, endDate).
// 		Select("COALESCE(SUM(amount), 0)").
// 		Row().
// 		Scan(&totalAmount)

// 	// Calculate success rate
// 	successRate := float64(0)
// 	if totalRefunds > 0 {
// 		successRate = (float64(successfulRefunds) / float64(totalRefunds)) * 100
// 	}

// 	// Get refunds by type
// 	type RefundTypeStats struct {
// 		RefundType  string
// 		Count       int64
// 		TotalAmount float64
// 	}
// 	var refundsByType []RefundTypeStats
// 	if err := s.db.Model(&models.Refund{}).
// 		Select("refund_type, COUNT(*) as count, COALESCE(SUM(amount), 0) as total_amount").
// 		Where("created_at BETWEEN ? AND ?", startDate, endDate).
// 		Group("refund_type").
// 		Scan(&refundsByType).Error; err != nil {
// 		log.Printf("[ANALYTICS] Warning: Failed to get refunds by type: %v", err)
// 	}

// 	return map[string]interface{}{
// 		"period":               map[string]time.Time{"start_date": startDate, "end_date": endDate},
// 		"total_refunds":        totalRefunds,
// 		"successful_refunds":   successfulRefunds,
// 		"failed_refunds":       failedRefunds,
// 		"pending_refunds":      pendingRefunds,
// 		"total_amount":         totalAmount,
// 		"successful_amount":    successfulAmount,
// 		"success_rate_percent": successRate,
// 		"refunds_by_type":      refundsByType,
// 	}, nil
// }

// // updatePaymentIntentRefundStatus updates the payment intent status based on refund
// func (s *PaymentService) updatePaymentIntentRefundStatus(pi *models.PaymentIntent, refund *models.Refund) error {
// 	// Check if this is a full refund
// 	if math.Abs(float64(refund.Amount-pi.AmountTotal)) < 1 { // Within 1 cent
// 		return s.db.Model(pi).Update("status", "refunded").Error
// 	}
// 	// Otherwise mark as partially refunded
// 	return s.db.Model(pi).Update("status", "partially_refunded").Error
// }
