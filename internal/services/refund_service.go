package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RefundService handles the full lifecycle of ticket refunds.
// Flow:
//   User: UserCancelTicket → pending refund
//   Admin: AdminCancelTicket → pending refund
//   Admin: ApproveRefund → auto-routes by payment gateway
//   Admin:   - Stripe  → calls gateway → processing (webhook → succeeded)
//   Admin:   - Konbini/manual → creates PaymentBill(type=refund) → processing
//              and becomes succeeded when the refund bill is fully paid
//   Admin: RejectRefund → rejected (terminal)

type RefundService struct {
	db                 *gorm.DB
	gwRegistry         *gateways.Registry
	sm                 *state.StateMachine[models.RefundStatus]
	refundCalc         *RefundCalculator
	refundQueue        *RefundQueueService
	emailOutboxService *EmailOutboxService // ✅ NEW: For sending refund status emails
}

func NewRefundService(
	db *gorm.DB,
	gw *gateways.Registry,
	sm *state.StateMachine[models.RefundStatus],
	refundCalc *RefundCalculator,
	refundQueue *RefundQueueService,
	emailOutboxService *EmailOutboxService, // ✅ NEW: Email service parameter
) *RefundService {
	return &RefundService{
		db:                 db,
		gwRegistry:         gw,
		sm:                 sm,
		refundCalc:         refundCalc,
		refundQueue:        refundQueue,
		emailOutboxService: emailOutboxService,
	}
}

// ─── Input types ────────────────────────────────────────────────────────────

// AdminCancelTicketRequest is the input for admin-initiated ticket cancellation.
type AdminCancelTicketRequest struct {
	TicketID     *uuid.UUID // one of these must be set
	TicketNumber string
	Reason       string
}

// ApproveRefundRequest contains optional manual transfer details for non-gateway refunds.
type ApproveRefundRequest struct {
	BankName          string `json:"bank_name,omitempty"`
	AccountHolderName string `json:"account_holder_name,omitempty"`
	AccountNumber     string `json:"account_number,omitempty"`
	RoutingNumber     string `json:"routing_number,omitempty"`
	Notes             string `json:"notes,omitempty"`
}

// ─── User flow ───────────────────────────────────────────────────────────────

// UserCancelTicket lets a logged-in user cancel one ticket at a time.
// It validates eligibility, marks the ticket as cancelled, and creates a
// pending Refund that an admin must approve or reject.
func (s *RefundService) UserCancelTicket(
	ctx context.Context,
	ticketID uuid.UUID,
	userID uuid.UUID,
	reason string,
) error {
	return s.createRefund(ctx, ticketID, userID, "user", reason, false)
}

// ─── Admin flow ──────────────────────────────────────────────────────────────

// AdminCancelTicket lets an admin cancel a ticket by ID or ticket number.
// It creates a pending Refund that still requires explicit approval.
func (s *RefundService) AdminCancelTicket(
	ctx context.Context,
	req AdminCancelTicketRequest,
	adminID uuid.UUID,
) error {
	// Resolve ticket
	var ticket models.Ticket
	if req.TicketID != nil {
		if err := s.db.WithContext(ctx).
			Preload("Event").
			First(&ticket, *req.TicketID).Error; err != nil {
			return utils.NewNotFoundError("ticket")
		}
	} else if req.TicketNumber != "" {
		if err := s.db.WithContext(ctx).
			Preload("Event").
			Where("ticket_number = ?", req.TicketNumber).
			First(&ticket).Error; err != nil {
			return utils.NewNotFoundError("ticket")
		}
	} else {
		return utils.NewValidationError("ticket_id or ticket_number is required", nil)
	}

	return s.createRefund(ctx, ticket.ID, adminID, "admin", req.Reason, true)
}

// ─── Admin approval/rejection ─────────────────────────────────────────────

// ApproveForGateway approves a pending refund for a gateway-driven payment.
// It calls the gateway API which moves the refund to "processing".
// The refund is completed when webhook processing confirms success/failure.
func (s *RefundService) ApproveForGateway(
	ctx context.Context,
	refundID uuid.UUID,
	adminID uuid.UUID,
) (*models.Refund, error) {
	var refund models.Refund

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&refund, refundID).Error; err != nil {
			return err
		}

		if refund.Status != models.RefundPending {
			return fmt.Errorf("refund is not in pending state (current: %s)", refund.Status)
		}

		if err := s.sm.Transition(refund.Status, models.RefundProcessing); err != nil {
			return err
		}

		now := time.Now()
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":      models.RefundProcessing,
			"approved_by": adminID,
			"approved_at": now,
		}).Error; err != nil {
			return err
		}

		// ✅ FIX: Mark ticket as CANCELED when refund is approved (not when pending)
		if refund.TicketID != uuid.Nil {
			if err := tx.Model(&models.Ticket{}).
				Where("id = ? AND status = ?", refund.TicketID, models.TicketActive).
				Update("status", models.TicketCanceled).Error; err != nil {
				return utils.NewDatabaseError("failed to cancel ticket", err)
			}
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, models.RefundPending, models.RefundProcessing, &adminID, "admin", "approved for stripe"); err != nil {
			return err
		}

		// Load transaction for provider charge ID
		var txn models.Transaction
		if err := tx.First(&txn, refund.TransactionID).Error; err != nil {
			return err
		}

		if txn.ProviderChargeID == "" {
			return fmt.Errorf("missing provider charge id on transaction")
		}

		gw, err := s.gwRegistry.Get(string(txn.PaymentGateway))
		if err != nil {
			return err
		}

		resp, err := gw.CreateRefund(ctx, &gateways.RefundRequest{
			GatewayChargeID: txn.ProviderChargeID,
			Amount:          refund.Amount,
			Currency:        refund.Currency,
			Reason:          refund.Reason,
			Metadata:        map[string]string{"refund_id": refund.ID.String()},
		})
		if err != nil {
			return fmt.Errorf("gateway refund call failed: %w", err)
		}

		if err := tx.Model(&refund).Update("provider_refund_id", resp.GatewayRefundID).Error; err != nil {
			return err
		}
		// enqueue a worker task to poll provider status as a safety net in case webhook is delayed
		if s.refundQueue != nil {
			// enqueue after transaction commits by scheduling outside tx
		}
		return LogPaymentAuditTx(
			tx,
			"refund_approved",
			"refund",
			refund.ID,
			&adminID,
			"admin",
			&refund.EventID,
			map[string]interface{}{
				"old_status":         models.RefundPending,
				"new_status":         models.RefundProcessing,
				"approval_method":    "stripe",
				"provider_refund_id": resp.GatewayRefundID,
			},
		)
	})
	// Enqueue refund processing task now (outside transaction) as a retry/poll mechanism
	if s.refundQueue != nil {
		_ = s.refundQueue.EnqueueRefundProcessing(refundID)
	}

	// ✅ NEW: Send email notification after successful approval
	if err == nil {
		if customerEmail := s.getCustomerEmailForRefund(ctx, &refund); customerEmail != "" {
			_ = s.sendRefundStatusEmail(ctx, &refund, customerEmail, models.RefundProcessing)
		}
	}

	return &refund, err
}

// ApproveForBillings approves a pending refund for manual/billing payments.
// It creates a refund bill and moves refund to processing.
// The refund is marked succeeded only after the bill is fully paid.
func (s *RefundService) ApproveForBillings(
	ctx context.Context,
	refundID uuid.UUID,
	adminID uuid.UUID,
	req ApproveRefundRequest,
) (*models.Refund, error) {
	var refund models.Refund

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&refund, refundID).Error; err != nil {
			return err
		}

		if refund.Status != models.RefundPending {
			return fmt.Errorf("refund is not in pending state (current: %s)", refund.Status)
		}

		if err := s.sm.Transition(refund.Status, models.RefundProcessing); err != nil {
			return err
		}

		// Resolve user info for bill notes
		var ticket models.Ticket
		if refund.TicketID != uuid.Nil {
			if err := tx.First(&ticket, refund.TicketID).Error; err != nil {
				return err
			}
		}
		var event models.Event
		if err := tx.Select("id, organizer_id").First(&event, refund.EventID).Error; err != nil {
			return err
		}

		userName, userEmail := s.resolveUserInfo(tx, ticket)

		now := time.Now()
		refundAmount := float64(refund.Amount)
		noteSuffix := req.Notes
		if noteSuffix != "" {
			noteSuffix = " | " + noteSuffix
		}
		noteParts := []string{
			fmt.Sprintf("Manual refund payout. User: %s <%s>", userName, userEmail),
		}
		if req.BankName != "" {
			noteParts = append(noteParts, "Bank: "+req.BankName)
		}
		if req.AccountHolderName != "" {
			noteParts = append(noteParts, "Account holder: "+req.AccountHolderName)
		}
		if req.AccountNumber != "" {
			noteParts = append(noteParts, "Account number: "+req.AccountNumber)
		}
		if req.RoutingNumber != "" {
			noteParts = append(noteParts, "Routing: "+req.RoutingNumber)
		}
		note := strings.Join(noteParts, " | ") + noteSuffix

		// Create billing record in payment_bills table
		bill := &models.PaymentBill{
			BillNumber:  s.generateBillNumber(),
			EventID:     &refund.EventID,
			OrganizerID: event.OrganizerID,
			CreatedByID: adminID,
			Currency:    refund.Currency,
			Amount:      refundAmount,
			PaidAmount:  0,
			Status:      models.PaymentBillPending,
			BillType:    models.BillTypeRefund,
			Notes:       note,
		}
		if err := tx.Create(bill).Error; err != nil {
			return err
		}

		// Move refund to processing and link bill.
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":         models.RefundProcessing,
			"approved_by":    adminID,
			"approved_at":    now,
			"refund_bill_id": bill.ID,
		}).Error; err != nil {
			return err
		}

		// ✅ FIX: Mark ticket as CANCELED when refund is approved (not when pending)
		if refund.TicketID != uuid.Nil {
			if err := tx.Model(&models.Ticket{}).
				Where("id = ? AND status = ?", refund.TicketID, models.TicketActive).
				Update("status", models.TicketCanceled).Error; err != nil {
				return utils.NewDatabaseError("failed to cancel ticket", err)
			}
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, models.RefundPending, models.RefundProcessing, &adminID, "admin", "approved for billing; awaiting bill payment"); err != nil {
			return err
		}

		return LogPaymentAuditTx(
			tx,
			"refund_approved",
			"refund",
			refund.ID,
			&adminID,
			"admin",
			&refund.EventID,
			map[string]interface{}{
				"old_status":      models.RefundPending,
				"new_status":      models.RefundProcessing,
				"approval_method": "billing",
				"payment_bill_id": bill.ID,
				"bill_type":       bill.BillType,
			},
		)
	})

	// ✅ NEW: Send email notification after successful approval
	if err == nil {
		if customerEmail := s.getCustomerEmailForRefund(ctx, &refund); customerEmail != "" {
			_ = s.sendRefundStatusEmail(ctx, &refund, customerEmail, models.RefundProcessing)
		}
	}

	return &refund, err
}

// RejectRefund rejects a pending refund. The ticket remains cancelled and no
// money is returned to the user.
func (s *RefundService) RejectRefund(
	ctx context.Context,
	refundID uuid.UUID,
	adminID uuid.UUID,
	reason string,
) (*models.Refund, error) {
	var refund models.Refund

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&refund, refundID).Error; err != nil {
			return err
		}

		if refund.Status != models.RefundPending {
			return fmt.Errorf("refund is not in pending state (current: %s)", refund.Status)
		}

		if err := s.sm.Transition(refund.Status, models.RefundRejected); err != nil {
			return err
		}

		now := time.Now()
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":           models.RefundRejected,
			"rejected_by":      adminID,
			"rejected_at":      now,
			"rejection_reason": reason,
		}).Error; err != nil {
			return err
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, models.RefundPending, models.RefundRejected, &adminID, "admin", "refund rejected by admin"); err != nil {
			return err
		}

		// ✅ FIX: Revert ticket status to ACTIVE when refund is rejected
		// This allows user to either use the ticket or request a new refund
		if refund.TicketID != uuid.Nil {
			if err := tx.Model(&models.Ticket{}).
				Where("id = ?", refund.TicketID).
				Update("status", models.TicketActive).Error; err != nil {
				return utils.NewDatabaseError("failed to revert ticket status", err)
			}
		}

		return LogPaymentAuditTx(
			tx,
			"refund_rejected",
			"refund",
			refund.ID,
			&adminID,
			"admin",
			&refund.EventID,
			map[string]interface{}{
				"old_status":       models.RefundPending,
				"new_status":       models.RefundRejected,
				"rejection_reason": reason,
			},
		)
	})

	// ✅ NEW: Send email notification after rejection
	if err == nil {
		if customerEmail := s.getCustomerEmailForRefund(ctx, &refund); customerEmail != "" {
			_ = s.sendRefundStatusEmail(ctx, &refund, customerEmail, models.RefundRejected)
		}
	}

	return &refund, err
}

// RetryRefund enqueues a refund for re-processing without creating duplicates.
// Admins can call this when a refund is stuck in processing. It is idempotent
// and will not create duplicate provider refunds because the worker checks
// for existing provider_refund_id before calling the gateway.
func (s *RefundService) RetryRefund(ctx context.Context, refundID uuid.UUID, adminID uuid.UUID) error {
	var refund models.Refund

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, refundID).Error; err != nil {
			return err
		}

		// Do not retry terminal refunds
		switch refund.Status {
		case models.RefundSucceeded, models.RefundRejected, models.RefundCancelled:
			return fmt.Errorf("refund is in terminal state: %s", refund.Status)
		}

		// Increase retry count and set to processing so worker picks it up
		now := time.Now().UTC()
		if err := tx.Model(&refund).Updates(map[string]any{
			"updated_at":  now,
			"retry_count": gorm.Expr("retry_count + 1"),
		}).Error; err != nil {
			return err
		}
		return LogPaymentAuditTx(tx, "refund_retry_requested", "refund", refund.ID, &adminID, "admin", &refund.EventID, map[string]interface{}{"note": "admin retry requested"})
	}); err != nil {
		return err
	}

	if s.refundQueue == nil {
		return fmt.Errorf("refund queue is not configured")
	}

	if err := s.refundQueue.EnqueueRefundProcessing(refundID); err != nil {
		return err
	}

	return nil
}

// ─── Stripe webhook ──────────────────────────────────────────────────────────

// ConfirmRefundWebhook is called when Stripe sends a "charge.refunded" event.
// It transitions the refund to succeeded and marks the ticket as refunded.
func (s *RefundService) ConfirmRefundWebhook(
	ctx context.Context,
	providerRefundID string,
) error {
	var refund models.Refund // Extract for email sending
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider_refund_id = ?", providerRefundID).
			First(&refund).Error; err != nil {
			return err
		}

		if refund.Status == models.RefundSucceeded {
			return nil // idempotent
		}

		if err := s.sm.Transition(refund.Status, models.RefundSucceeded); err != nil {
			return err
		}

		now := time.Now()
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":       models.RefundSucceeded,
			"processed_at": now,
		}).Error; err != nil {
			return err
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, refund.Status, models.RefundSucceeded, nil, "webhook", "refund completed via webhook"); err != nil {
			return err
		}

		if refund.TicketID != uuid.Nil {
			// Mark the ticket as refunded
			if err := tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(map[string]any{
				"status":        models.TicketRefunded,
				"refund_id":     refund.ID,
				"refunded_at":   now,
				"refund_amount": refund.Amount,
			}).Error; err != nil {
				return err
			}

			// Restore tier inventory
			var ticket models.Ticket
			if err := tx.First(&ticket, refund.TicketID).Error; err == nil {
				s.restoreInventory(tx, ticket.TierID)
			}
		} else {
			if err := tx.Model(&models.Ticket{}).
				Where("transaction_id = ? AND status <> ?", refund.TransactionID, models.TicketRefunded).
				Updates(map[string]any{
					"status":      models.TicketRefunded,
					"refund_id":   refund.ID,
					"refunded_at": now,
				}).Error; err != nil {
				return err
			}
		}

		if err := LogPaymentAuditTx(
			tx,
			"refund_succeeded",
			"refund",
			refund.ID,
			nil,
			"webhook",
			&refund.EventID,
			map[string]interface{}{
				"old_status": refund.Status,
				"new_status": models.RefundSucceeded,
			},
		); err != nil {
			return err
		}

		if refund.TicketID != uuid.Nil {
			return LogPaymentAuditTx(
				tx,
				"ticket_refunded",
				"ticket",
				refund.TicketID,
				nil,
				"webhook",
				&refund.EventID,
				map[string]interface{}{
					"refund_id":           refund.ID,
					"refund_amount_cents": refund.Amount,
				},
			)
		}
		return nil
	})

	// ✅ NEW: Send email notification after successful webhook confirmation
	if err == nil && refund.Status == models.RefundSucceeded {
		if customerEmail := s.getCustomerEmailForRefund(ctx, &refund); customerEmail != "" {
			_ = s.sendRefundStatusEmail(ctx, &refund, customerEmail, models.RefundSucceeded)
		}
	}

	return err
}

// ─── Query methods ────────────────────────────────────────────────────────────

// AdminGetAllRefundsList returns a paginated, filtered list of refunds.
func (s *RefundService) AdminGetAllRefundsList(
	ctx context.Context,
	status, search, refundType string,
	startDate, endDate *time.Time,
	transactionID *uuid.UUID,
	page, limit int,
	sortBy, sortOrder string,
) ([]models.Refund, int64, error) {
	var refunds []models.Refund
	var total int64

	query := s.db.WithContext(ctx).Model(&models.Refund{}).
		Preload("Event").
		Joins("LEFT JOIN users ON users.id::text = refunds.initiated_by::text").
		Joins("LEFT JOIN events ON events.id = refunds.event_id")

	if status != "" {
		query = query.Where("refunds.status = ?", status)
	}

	if refundType == "full" {
		query = query.Where("refunds.is_full_refund = ?", true)
	} else if refundType == "partial" {
		query = query.Where("refunds.is_full_refund = ?", false)
	}

	if search != "" {
		search = strings.TrimSpace(search)
		if search != "" {
			searchTerm := "%" + search + "%"
			query = query.Where(
				`(refunds.refund_number ILIKE ? 
				 OR CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, '')) ILIKE ?
				 OR events.title ILIKE ?)`,
				searchTerm, searchTerm, searchTerm,
			)
		}
	}

	if startDate != nil {
		query = query.Where("refunds.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("refunds.created_at <= ?", *endDate)
	}
	if transactionID != nil {
		query = query.Where("refunds.transaction_id = ?", *transactionID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit

	// Build order clause — handle event_title/event_name sort specially
	orderClause := fmt.Sprintf("refunds.%s %s", sortBy, sortOrder)
	if sortBy == "event_title" || sortBy == "event_name" {
		orderClause = fmt.Sprintf("events.title %s", sortOrder)
	}

	if err := query.Order(orderClause).
		Offset(offset).Limit(limit).Find(&refunds).Error; err != nil {
		return nil, 0, err
	}

	return refunds, total, nil
}

// AdminGetRefund returns a single refund by ID.
func (s *RefundService) AdminGetRefund(ctx context.Context, refundID uuid.UUID) (*models.Refund, error) {
	var refund models.Refund
	err := s.db.WithContext(ctx).Preload("Event").First(&refund, refundID).Error
	if err != nil {
		return nil, err
	}
	return &refund, nil
}

// AdminGetRefundStatusHistory returns the status-change history for a refund.
func (s *RefundService) AdminGetRefundStatusHistory(
	ctx context.Context,
	refundID uuid.UUID,
) ([]models.RefundStatusHistory, error) {
	var history []models.RefundStatusHistory
	err := s.db.WithContext(ctx).
		Where("refund_id = ?", refundID).
		Order("changed_at ASC").
		Find(&history).Error
	return history, err
}

// GetUserRefunds returns all refunds for a given user (regardless of who initiated them) with optional date filtering.
// Shows both user-initiated refunds and admin-initiated refunds (e.g., from event cancellations).
func (s *RefundService) GetUserRefunds(
	ctx context.Context,
	userID uuid.UUID,
	page, limit int,
	startDate, endDate *time.Time,
	transactionID *uuid.UUID,
) ([]models.Refund, int64, error) {
	var refunds []models.Refund
	var total int64

	// ✅ FIXED: Filter by UserID to show all refunds belonging to the user,
	// not just those initiated by the user. This includes:
	// - Refunds initiated by the user themselves (InitiatorType = 'user')
	// - Refunds created by admin (InitiatorType = 'admin') when event was cancelled
	query := s.db.WithContext(ctx).Model(&models.Refund{}).
		Preload("Event").
		Where("user_id = ?", userID)

	// ✅ NEW: Add date filtering support
	if startDate != nil {
		query = query.Where("refunds.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("refunds.created_at <= ?", *endDate)
	}

	// ✅ NEW: Add optional transaction_id filtering
	if transactionID != nil {
		query = query.Where("transaction_id = ?", *transactionID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&refunds).Error; err != nil {
		return nil, 0, err
	}

	return refunds, total, nil
}

// GetUserRefund returns details of a specific refund, verifying that it belongs to the requesting user.
// Shows both user-initiated refunds and admin-initiated refunds (e.g., from event cancellations).
func (s *RefundService) GetUserRefund(
	ctx context.Context,
	userID uuid.UUID,
	refundID uuid.UUID,
) (*models.Refund, error) {
	var refund models.Refund
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", refundID, userID).
		First(&refund).Error; err != nil {
		return nil, utils.NewNotFoundError("refund")
	}
	return &refund, nil
}

// GetUserRefundStatusHistory returns the status-change history for a refund,
// verifying that the refund belongs to the requesting user.
func (s *RefundService) GetUserRefundStatusHistory(
	ctx context.Context,
	userID uuid.UUID,
	refundID uuid.UUID,
) ([]models.RefundStatusHistory, error) {
	// Ownership check
	var refund models.Refund
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", refundID, userID).
		First(&refund).Error; err != nil {
		return nil, utils.NewNotFoundError("refund")
	}

	var history []models.RefundStatusHistory
	err := s.db.WithContext(ctx).
		Where("refund_id = ?", refundID).
		Order("changed_at ASC").
		Find(&history).Error
	return history, err
}

// ─── Internal helpers ────────────────────────────────────────────────────────

// createRefund is the shared logic for UserCancelTicket and AdminCancelTicket.
// ✅ OPTIMIZED: Proper transaction flow with all critical checks inside transaction,
// complete rollback on failure, race condition elimination.
func (s *RefundService) createRefund(
	ctx context.Context,
	ticketID uuid.UUID,
	initiatorID uuid.UUID,
	initiatorType string,
	reason string,
	isAdmin bool,
) error {
	// ─── Phase 1: Pre-transaction validation (read-only, no locks) ─────────────────────
	// Load ticket and event for eligibility checks
	var ticket models.Ticket
	if err := s.db.WithContext(ctx).
		Preload("Event").
		First(&ticket, ticketID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return utils.NewNotFoundError("ticket")
		}
		return utils.NewDatabaseError("Failed to load ticket", err)
	}

	// Ownership check for users (can only cancel their own tickets)
	if !isAdmin && ticket.ActorID != initiatorID {
		return utils.NewForbiddenError("ticket does not belong to this user")
	}

	// Time window eligibility for users (admins bypass this)
	now := time.Now()
	if !isAdmin {
		if err := s.refundCalc.ValidateCancellationRequest(&ticket, ticket.Event, initiatorID, isAdmin, now); err != nil {
			return utils.NewBusinessLogicError(err.Error())
		}
	}

	// Load transaction for amount calculation
	var txn models.Transaction
	if err := s.db.WithContext(ctx).First(&txn, ticket.TransactionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return utils.NewNotFoundError("transaction not found for ticket")
		}
		return utils.NewDatabaseError("Failed to load transaction", err)
	}

	// ✅ CRITICAL: Only succeeded transactions can be refunded
	if txn.Status != models.TransactionSucceeded {
		return utils.NewBusinessLogicError(fmt.Sprintf("Cannot refund transaction with status '%s' - only succeeded transactions can be refunded", txn.Status))
	}

	// Calculate per-ticket refund amount (no DB access)
	refundAmount, err := s.refundCalc.CalculateRefundForTicket(&txn)
	if err != nil {
		return utils.NewBusinessLogicError(fmt.Sprintf("failed to calculate refund amount: %v", err))
	}

	// ─── Phase 2: Atomic transaction (all critical checks + modifications) ────────────
	// This ensures complete rollback if ANY step fails
	var returnRefund models.Refund
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock ticket for atomic read-verify-modify
		var ticketInTx models.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&ticketInTx, ticketID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return utils.NewNotFoundError("ticket")
			}
			return utils.NewDatabaseError("Failed to lock ticket", err)
		}

		// ✅ FIX: Verify no check-ins (inside transaction, prevents race with concurrent check-in)
		var checkInCount int64
		if err := tx.Model(&models.TicketCheckIn{}).
			Where("ticket_id = ?", ticketID).
			Count(&checkInCount).Error; err != nil {
			return utils.NewDatabaseError("failed to verify ticket check-ins", err)
		}

		// ✅ FIX: Check for existing refund INSIDE transaction (prevents duplicate creation race condition)
		var existingRefund models.Refund
		refundFound := false
		if err := tx.
			Where("ticket_id = ?", ticketID).
			First(&existingRefund).Error; err == nil {
			refundFound = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewDatabaseError("failed to check existing refunds", err)
		}
		if err := s.refundCalc.ValidateCancellationRequest(&ticketInTx, ticket.Event, initiatorID, isAdmin, now); err != nil {
			return utils.NewBusinessLogicError(err.Error())
		}
		if err := s.refundCalc.ValidateCancellationState(ticketInTx.Status, checkInCount, &existingRefund, refundFound); err != nil {
			return utils.NewConflictError(err.Error())
		}

		// ✅ If refund exists in terminal state (rejected/cancelled), delete it to allow new refund creation
		if refundFound && (existingRefund.Status == models.RefundRejected || existingRefund.Status == models.RefundCancelled) {
			if err := tx.Delete(&existingRefund).Error; err != nil {
				return utils.NewDatabaseError("failed to clean up old rejected refund", err)
			}
		}

		// ✅ All checks passed. Mark ticket as CANCELED immediately so the UI
		// can reflect the cancellation right away, while the refund itself remains pending.
		if err := tx.Model(&models.Ticket{}).
			Where("id = ? AND status = ?", ticketID, models.TicketActive).
			Update("status", models.TicketPendingRefund).Error; err != nil {
			return utils.NewDatabaseError("failed to cancel ticket", err)
		}

		// Create refund record
		refund := &models.Refund{
			ID:              uuid.New(),
			RefundNumber:    fmt.Sprintf("REF-%s", uuid.New().String()[:8]),
			TicketID:        ticketID,
			TransactionID:   txn.ID,
			PaymentIntentID: txn.PaymentIntentID,
			EventID:         txn.EventID,
			Provider:        txn.PaymentGateway,
			PaymentProvider: txn.PaymentGateway,
			OrderID:         ticket.CheckoutToken,
			UserID:          &ticket.ActorID,
			Amount:          refundAmount,
			Currency:        txn.Currency,
			Reason:          reason,
			RefundType:      models.RefundTypeTicketRefund,
			InitiatedBy:     initiatorID,
			InitiatorType:   initiatorType,
			Status:          models.RefundPending,
			IsFullRefund:    true, // one ticket = full unit refund
		}

		if err := tx.Omit("provider_refund_id").Create(refund).Error; err != nil {
			return utils.NewDatabaseError("failed to create refund record", err)
		}

		returnRefund = *refund
		return nil
	})

	// If transaction failed, all changes rolled back automatically by GORM
	if err != nil {
		return err
	}

	// ─── Phase 3: Post-transaction async logging (non-blocking, safe to fail) ────────
	// These operations run in background and NEVER block the API response
	// Use background context so logging continues even if client disconnects

	// Log status history asynchronously
	go func() {
		if logErr := s.logRefundStatusHistory(
			s.db.WithContext(context.Background()),
			returnRefund.ID,
			"",
			models.RefundPending,
			&initiatorID,
			initiatorType,
			"refund requested",
		); logErr != nil {
			fmt.Printf("[RefundService] Failed to log refund status history: %v\n", logErr)
		}
	}()

	// Log ticket cancellation audit
	go func() {
		LogPaymentAuditAsync(
			s.db.WithContext(context.Background()),
			"ticket_cancelled_for_refund",
			"ticket",
			ticket.ID,
			&initiatorID,
			initiatorType,
			&ticket.EventID,
			map[string]interface{}{
				"old_status":    ticket.Status,
				"new_status":    models.TicketCanceled,
				"cancel_reason": reason,
				"is_admin":      isAdmin,
			},
		)
	}()

	// Log refund creation audit
	go func() {
		LogPaymentAuditAsync(
			s.db.WithContext(context.Background()),
			"refund_created",
			"refund",
			returnRefund.ID,
			&initiatorID,
			initiatorType,
			&returnRefund.EventID,
			map[string]interface{}{
				"refund_number":     returnRefund.RefundNumber,
				"status":            returnRefund.Status,
				"amount_cents":      returnRefund.Amount,
				"currency":          returnRefund.Currency,
				"transaction_id":    returnRefund.TransactionID,
				"payment_intent_id": returnRefund.PaymentIntentID,
				"reason":            returnRefund.Reason,
			},
		)
	}()

	// ✅ NEW: Send email notification when refund is first created (RefundPending status)
	go func() {
		bgCtx := context.Background()
		if customerEmail := s.getCustomerEmailForRefund(bgCtx, &returnRefund); customerEmail != "" {
			if logErr := s.sendRefundStatusEmail(bgCtx, &returnRefund, customerEmail, models.RefundPending); logErr != nil {
				fmt.Printf("[RefundService] Failed to send refund created email: %v\n", logErr)
			}
		}
	}()

	return nil
}

// restoreInventory increments available count and decrements sold count for the tier after a refund.
func (s *RefundService) restoreInventory(tx *gorm.DB, tierID uuid.UUID) {
	tx.Exec(`UPDATE event_tiers SET available = available + 1, sold = GREATEST(sold - 1, 0) WHERE id = ?`, tierID)
}

// resolveUserInfo returns name/email from the ticket's actor (user or guest).
func (s *RefundService) resolveUserInfo(tx *gorm.DB, ticket models.Ticket) (string, string) {
	if ticket.ActorType == models.ActorUser {
		var u models.User
		if err := tx.First(&u, ticket.ActorID).Error; err == nil {
			return u.FirstName + " " + u.LastName, u.Email
		}
	} else {
		var g models.GuestUser
		if err := tx.First(&g, ticket.ActorID).Error; err == nil {
			return g.FirstName + " " + g.LastName, g.Email
		}
	}
	return "", ""
}

// generateBillNumber generates a short unique bill number for refund bills.
func (s *RefundService) generateBillNumber() string {
	return fmt.Sprintf("RFB-%s", uuid.New().String()[:8])
}

// uuidsToStrings converts uuid slice to string slice.
func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// logRefundStatusHistory
func (s *RefundService) logRefundStatusHistory(
	tx *gorm.DB,
	refundID uuid.UUID,
	fromStatus, toStatus models.RefundStatus,
	changedBy *uuid.UUID,
	changedByType string,
	note string,
) error {
	history := &models.RefundStatusHistory{
		ID:            uuid.New(),
		RefundID:      refundID,
		OldStatus:     fromStatus,
		NewStatus:     toStatus,
		ChangedAt:     time.Now(),
		ChangedByID:   changedBy,
		ChangedByType: changedByType,
		Remarks:       note,
	}
	return tx.Create(history).Error
}

// ✅ sendRefundStatusEmail sends email notification for refund status changes
// Common template for all statuses with 3-4 business day messaging
func (s *RefundService) sendRefundStatusEmail(ctx context.Context, refund *models.Refund, userEmail string, toStatus models.RefundStatus) error {
	if s.emailOutboxService == nil || userEmail == "" {
		return nil // Skip if service or email not available
	}

	var event models.Event
	if err := s.db.WithContext(ctx).Where("id = ?", refund.EventID).First(&event).Error; err != nil {
		return nil // Don't fail the refund process if we can't get event details
	}

	var subject, statusMessage string
	switch toStatus {
	case models.RefundPending:
		subject = "Refund Request Received"
		statusMessage = "Your refund request has been received and is pending review by our team."

	case models.RefundProcessing:
		subject = "Refund Approved & Processing"
		statusMessage = "Your refund has been approved and is now being processed. It typically takes 3-4 business days for the amount to appear in your bank account or original payment method."

	case models.RefundSucceeded:
		subject = "Refund Successfully Completed"
		statusMessage = "Your refund has been successfully completed. The amount should appear in your bank account or original payment method within 3-4 business days if not already reflected."

	case models.RefundRejected:
		subject = "Refund Request Rejected"
		statusMessage = "Your refund request has been reviewed and rejected. Please contact support for more information about the decision."

	case models.RefundFailed:
		subject = "Refund Processing Failed"
		statusMessage = "Unfortunately, there was an issue processing your refund. Our team is investigating this and will follow up with you shortly."

	case models.RefundCancelled:
		subject = "Refund Request Cancelled"
		statusMessage = "Your refund request has been cancelled. If you have any questions, please contact our support team."

	default:
		return nil // Unknown status
	}

	templateData := map[string]interface{}{
		"customer_email": userEmail,
		"event_name":     event.Title,
		"refund_amount":  refund.Amount,
		"currency":       refund.Currency,
		"status_message": statusMessage,
		"refund_reason":  refund.Reason,
		"refund_id":      refund.RefundNumber,
		"business_days":  "3-4", // Common messaging for all statuses
	}

	return s.emailOutboxService.QueueEmail(
		ctx,
		models.EmailEventRefundStatusUpdate, // Use new email event type constant
		userEmail,
		subject,
		templateData,
		0, // HIGH PRIORITY: Process immediately (not delayed)
	)
}

// ✅ getCustomerEmailForRefund resolves customer email from multiple sources for refund notifications
func (s *RefundService) getCustomerEmailForRefund(ctx context.Context, refund *models.Refund) string {
	// First, try to get email from PaymentIntent
	var paymentIntent models.PaymentIntent
	if err := s.db.WithContext(ctx).Select("customer_email").First(&paymentIntent, refund.PaymentIntentID).Error; err == nil {
		if paymentIntent.CustomerEmail != "" {
			return paymentIntent.CustomerEmail
		}
	}

	// Second, try to get email from User (if refund has user_id)
	if refund.UserID != nil {
		var user models.User
		if err := s.db.WithContext(ctx).Select("email").First(&user, *refund.UserID).Error; err == nil && user.Email != "" {
			return user.Email
		}
	}

	// Third, try to get email from Ticket's actor (User or GuestUser)
	if refund.TicketID != uuid.Nil {
		var ticket models.Ticket
		if err := s.db.WithContext(ctx).Select("actor_id", "actor_type").First(&ticket, refund.TicketID).Error; err == nil {
			if ticket.ActorType == models.ActorUser {
				var user models.User
				if err := s.db.WithContext(ctx).Select("email").First(&user, ticket.ActorID).Error; err == nil && user.Email != "" {
					return user.Email
				}
			} else if ticket.ActorType == models.ActorGuest {
				var guest models.GuestUser
				if err := s.db.WithContext(ctx).Select("email").First(&guest, ticket.ActorID).Error; err == nil && guest.Email != "" {
					return guest.Email
				}
			}
		}
	}

	return "" // Unable to find customer email
}

// ApproveRefund approves a pending refund and auto-detects the flow by payment gateway.
func (s *RefundService) ApproveRefund(
	ctx context.Context,
	refundID uuid.UUID,
	adminID uuid.UUID,
	req ApproveRefundRequest,
) (*models.Refund, error) {
	var refund models.Refund
	if err := s.db.WithContext(ctx).First(&refund, refundID).Error; err != nil {
		return nil, err
	}

	var txn models.Transaction
	if err := s.db.WithContext(ctx).Select("payment_gateway").First(&txn, refund.TransactionID).Error; err != nil {
		return nil, err
	}

	switch txn.PaymentGateway {
	case models.PaymentGatewayStripe:
		return s.ApproveForGateway(ctx, refundID, adminID)
	case models.PaymentGatewayKonbini:
		return s.ApproveForBillings(ctx, refundID, adminID, req)
	default:
		return s.ApproveForGateway(ctx, refundID, adminID)
	}
}

// ✅ NEW: ApproveRejectedRefund allows admin to change a rejected refund back to approved.
// This enables admins to reconsider and approve previously rejected refunds.
func (s *RefundService) ApproveRejectedRefund(
	ctx context.Context,
	refundID uuid.UUID,
	adminID uuid.UUID,
	req ApproveRefundRequest,
) (*models.Refund, error) {
	var refund models.Refund

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&refund, refundID).Error; err != nil {
			return err
		}

		// ✅ FIX: Allow changing from RefundRejected back to processing
		if refund.Status != models.RefundRejected {
			return fmt.Errorf("refund is not in rejected state (current: %s)", refund.Status)
		}

		if err := s.sm.Transition(refund.Status, models.RefundProcessing); err != nil {
			return err
		}

		now := time.Now()
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":      models.RefundProcessing,
			"approved_by": adminID,
			"approved_at": now,
		}).Error; err != nil {
			return err
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, models.RefundRejected, models.RefundProcessing, &adminID, "admin", "re-approved from rejected state"); err != nil {
			return err
		}

		// Mark ticket as CANCELED when re-approved
		if refund.TicketID != uuid.Nil {
			if err := tx.Model(&models.Ticket{}).
				Where("id = ? AND status = ?", refund.TicketID, models.TicketActive).
				Update("status", models.TicketCanceled).Error; err != nil {
				return utils.NewDatabaseError("failed to cancel ticket", err)
			}
		}

		// Load transaction for provider charge ID
		var txn models.Transaction
		if err := tx.First(&txn, refund.TransactionID).Error; err != nil {
			return err
		}

		if txn.ProviderChargeID == "" {
			return fmt.Errorf("missing provider charge id on transaction")
		}

		gw, err := s.gwRegistry.Get(string(txn.PaymentGateway))
		if err != nil {
			return err
		}

		resp, err := gw.CreateRefund(ctx, &gateways.RefundRequest{
			GatewayChargeID: txn.ProviderChargeID,
			Amount:          refund.Amount,
			Currency:        refund.Currency,
			Reason:          refund.Reason,
			Metadata:        map[string]string{"refund_id": refund.ID.String()},
		})
		if err != nil {
			return fmt.Errorf("gateway refund call failed: %w", err)
		}

		if err := tx.Model(&refund).Update("provider_refund_id", resp.GatewayRefundID).Error; err != nil {
			return err
		}

		return LogPaymentAuditTx(
			tx,
			"refund_re_approved",
			"refund",
			refund.ID,
			&adminID,
			"admin",
			&refund.EventID,
			map[string]interface{}{
				"old_status":         models.RefundRejected,
				"new_status":         models.RefundProcessing,
				"provider_refund_id": resp.GatewayRefundID,
			},
		)
	})

	if s.refundQueue != nil {
		_ = s.refundQueue.EnqueueRefundProcessing(refundID)
	}

	// ✅ NEW: Send email notification after successful re-approval
	if err == nil {
		if customerEmail := s.getCustomerEmailForRefund(ctx, &refund); customerEmail != "" {
			_ = s.sendRefundStatusEmail(ctx, &refund, customerEmail, models.RefundProcessing)
		}
	}

	return &refund, err
}
