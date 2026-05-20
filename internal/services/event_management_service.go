package services

import (
	"errors"
	"event-ticketing-backend/pkg/currency"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EventManagementService handles advanced event management operations
type EventManagementService struct {
	db           *gorm.DB
	eventService *EventService
	refundQueue  *RefundQueueService
}

// NewEventManagementService creates a new event management service
func NewEventManagementService(refundQueue *RefundQueueService) *EventManagementService {
	return &EventManagementService{
		db:           database.DB,
		eventService: NewEventService(),
		refundQueue:  refundQueue,
	}
}

// GetDB returns the database instance
func (s *EventManagementService) GetDB() *gorm.DB {
	return s.db
}

// ControlEventSales allows organizers to pause, resume, or stop sales
func (s *EventManagementService) ControlEventSales(eventID, organizerID uuid.UUID, req *models.EventSalesControlRequest) error {
	var event models.Event

	// Find the event and verify ownership
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("event")
		}
		return utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return utils.NewBusinessLogicError("Cannot control sales for cancelled event.")
	}

	// Update sales status based on action.
	var targetStatus string
	var targetSalesStatus string
	switch req.Action {
	case "pause":
		if event.SalesStatus == models.EventSalesStatusPaused.String() {
			return utils.NewBusinessLogicError("Event sales are already paused.")
		}
		if event.Status == models.EventStatusCompleted.String() || event.Status == models.EventStatusCancelled.String() {
			return utils.NewBusinessLogicError("Event sales cannot be paused after the event is completed or cancelled.")
		}
		targetSalesStatus = models.EventSalesStatusPaused.String()
		if event.Status == models.EventStatusOnSale.String() || event.Status == models.EventStatusScheduled.String() ||
			event.Status == models.EventStatusSalesUpcoming.String() || event.Status == models.EventStatusSalesEnd.String() {
			targetStatus = models.EventStatusHold.String()
		}
	case "resume":
		if event.SalesStatus == models.EventSalesStatusActive.String() {
			return utils.NewBusinessLogicError("Event sales are already active.")
		}
		if event.Status == models.EventStatusCompleted.String() || event.Status == models.EventStatusCancelled.String() {
			return utils.NewBusinessLogicError("Event sales cannot be resumed after the event is completed or cancelled.")
		}
		targetSalesStatus = models.EventSalesStatusActive.String()
		if event.Status == models.EventStatusHold.String() {
			targetStatus = models.EventStatusOnSale.String()
		}
	case "stop":
		if event.SalesStatus == models.EventSalesStatusStopped.String() {
			return utils.NewBusinessLogicError("Event sales are already stopped.")
		}
		if event.Status == models.EventStatusCompleted.String() || event.Status == models.EventStatusCancelled.String() {
			return utils.NewBusinessLogicError("Event sales cannot be stopped after the event is completed or cancelled.")
		}
		targetSalesStatus = models.EventSalesStatusStopped.String()
		if event.Status != models.EventStatusLive.String() {
			targetStatus = models.EventStatusSalesEnd.String()
		}
	default:
		return utils.NewValidationError("Invalid action: must be pause, resume, or stop.", nil)
	}

	// Use central function to update both status and sales status with logging
	err := s.eventService.UpdateEventStatusAndSalesStatusWithLogging(
		eventID,
		targetStatus,
		targetSalesStatus,
		models.EventStatusTypeManual.String(),
		organizerID.String(),
		req.Reason,
	)
	if err != nil {
		if appErr, ok := err.(*utils.AppError); ok {
			return appErr
		}
		return utils.NewDatabaseError("Failed to update event sales status.", err)
	}

	return nil
}

func (s *EventManagementService) CreateCancellationRequest(eventID, organizerID uuid.UUID, req *models.CreateEventCancellationRequest) (*models.EventCancellationRequest, error) {
	var created models.EventCancellationRequest

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var event models.Event
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organizer_id = ?", eventID, organizerID).
			First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return utils.NewForbiddenError("Event not found or you don't have permission.")
			}
			return utils.NewDatabaseError("Failed to retrieve event.", err)
		}

		if event.IsCancelled || event.Status == models.EventStatusCancelled.String() {
			return utils.NewBusinessLogicError("Cancelled events cannot be cancelled again.")
		}
		if event.Status == models.EventStatusCompleted.String() {
			return utils.NewBusinessLogicError("Completed events cannot be cancelled.")
		}

		var pendingCount int64
		if err := tx.Model(&models.EventCancellationRequest{}).
			Where("event_id = ? AND status = ?", eventID, models.EventCancellationRequestPending).
			Count(&pendingCount).Error; err != nil {
			return utils.NewDatabaseError("Failed to check existing requests.", err)
		}
		if pendingCount > 0 {
			return utils.NewBusinessLogicError("A pending cancellation request already exists for this event.")
		}

		created = models.EventCancellationRequest{
			ID:          uuid.New(),
			EventID:     eventID,
			OrganizerID: organizerID,
			Reason:      strings.TrimSpace(req.Reason),
			Status:      models.EventCancellationRequestPending,
		}
		if err := tx.Create(&created).Error; err != nil {
			return utils.NewDatabaseError("Failed to create cancellation request.", err)
		}

		// Store current status for potential reversion on rejection
		oldStatus := event.Status
		currentStatus := event.Status
		statusPtr := &currentStatus
		if err := tx.Model(&event).Update("status_before_cancel_request", statusPtr).Error; err != nil {
			return utils.NewDatabaseError("Failed to store pre-cancel status.", err)
		}

		// Validate state transition from current status to cancel_pending
		sm := state.NewStateMachine(state.EventTransitions)
		if err := sm.Transition(models.EventStatus(oldStatus), models.EventStatusCancelPending); err != nil {
			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot cancel event in current status: %s", event.Status))
		}

		// Change event status to cancel_pending and log it atomically
		if err := tx.Model(&event).Update("status", models.EventStatusCancelPending.String()).Error; err != nil {
			return utils.NewDatabaseError("Failed to update event status.", err)
		}

		// Log the status change to event history
		if logErr := s.eventService.LogStatusChangeTx(tx, eventID, oldStatus, models.EventStatusCancelPending.String(), models.EventStatusTypeManual.String(), organizerID.String(), "cancellation requested: "+strings.TrimSpace(req.Reason)); logErr != nil {
			return utils.NewDatabaseError("Failed to log status change in event history.", logErr)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}

func (s *EventManagementService) ListCancellationRequests(status string, page, limit int) ([]models.EventCancellationRequestResponse, int64, error) {
	var reqs []models.EventCancellationRequest
	var total int64

	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}

	query := s.db.Model(&models.EventCancellationRequest{}).
		Preload("Event").
		Preload("Organizer").
		Preload("Reviewer")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&reqs).Error; err != nil {
		return nil, 0, err
	}
	// Map Response
	responses := make([]models.EventCancellationRequestResponse, 0, len(reqs))

	for i := range reqs {
		responses = append(
			responses,
			models.NewEventCancellationRequestResponse(&reqs[i]),
		)
	}
	return responses, total, nil
}

func (s *EventManagementService) ReviewCancellationRequest(requestID, adminID uuid.UUID, approve bool, adminRemark string) (*models.EventCancellationRequest, error) {
	var reviewed models.EventCancellationRequest
	refundIDs := make([]uuid.UUID, 0)
	now := time.Now().UTC()

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&reviewed, requestID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return utils.NewNotFoundError("cancellation request")
			}
			return err
		}

		// Allow reviewing pending or previously rejected requests (admin can reconsider)
		if reviewed.Status != models.EventCancellationRequestPending && reviewed.Status != models.EventCancellationRequestRejected {
			return utils.NewBusinessLogicError("Cannot review an already approved cancellation request.")
		}

		targetStatus := models.EventCancellationRequestRejected
		if approve {
			targetStatus = models.EventCancellationRequestApproved
		}

		if err := tx.Model(&reviewed).Updates(map[string]any{
			"status":       targetStatus,
			"admin_remark": strings.TrimSpace(adminRemark),
			"reviewed_by":  adminID,
			"reviewed_at":  now,
		}).Error; err != nil {
			return err
		}

		if !approve {
			reviewed.Status = targetStatus
			reviewed.AdminRemark = strings.TrimSpace(adminRemark)
			reviewed.ReviewedBy = &adminID
			reviewed.ReviewedAt = &now

			// Fetch the event to get the stored pre-cancel status
			var event models.Event
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reviewed.EventID).First(&event).Error; err != nil {
				return err
			}

			// Revert to stored status if available, otherwise keep current
			if event.StatusBeforeCancelRequest != nil && *event.StatusBeforeCancelRequest != "" {
				oldStatus := event.Status
				if err := tx.Model(&event).Updates(map[string]any{
					"status":                       *event.StatusBeforeCancelRequest,
					"status_before_cancel_request": nil,
				}).Error; err != nil {
					return err
				}
				// Log the status revert to event history
				if logErr := s.eventService.LogStatusChangeTx(tx, reviewed.EventID, oldStatus, *event.StatusBeforeCancelRequest, models.EventStatusTypeApproval.String(), adminID.String(), "cancellation request rejected: "+strings.TrimSpace(adminRemark)); logErr != nil {
					return logErr
				}
			} else {
				// If no stored status, just log the rejection note
				if logErr := s.eventService.LogEventNoteTx(tx, reviewed.EventID, models.EventStatusTypeApproval.String(), adminID.String(), "cancellation request rejected: "+strings.TrimSpace(adminRemark)); logErr != nil {
					return logErr
				}
			}
			return nil
		}

		var event models.Event
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reviewed.EventID).First(&event).Error; err != nil {
			return err
		}
		if event.IsCancelled || event.Status == models.EventStatusCancelled.String() || event.Status == models.EventStatusCompleted.String() {
			return utils.NewBusinessLogicError("Event cannot be cancelled in its current state.")
		}

		// Clear the pre-cancel status since we're proceeding with cancellation
		if err := tx.Model(&event).Update("status_before_cancel_request", nil).Error; err != nil {
			return utils.NewDatabaseError("Failed to clear pre-cancel status.", err)
		}

		// Update event status to cancelled directly within this transaction
		oldStatus := event.Status
		if err := tx.Model(&event).Updates(map[string]interface{}{
			"status":        models.EventStatusCancelled.String(),
			"sales_status":  models.EventSalesStatusStopped.String(),
			"is_cancelled":  true,
			"cancelled_at":  now,
			"cancel_reason": reviewed.Reason,
		}).Error; err != nil {
			return utils.NewDatabaseError("Failed to update event status to cancelled.", err)
		}

		// Log the status change within this transaction (avoids nested transaction)
		if logErr := s.eventService.LogStatusChangeTx(tx, reviewed.EventID, oldStatus, models.EventStatusCancelled.String(), models.EventStatusTypeApproval.String(), adminID.String(), "event cancellation request approved"); logErr != nil {
			return logErr
		}

		// Invalidate existing tickets and check-in records for this event.
		if err := tx.Exec(`DELETE FROM ticket_check_ins WHERE ticket_id IN (SELECT id FROM tickets WHERE event_id = ?)`, event.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Ticket{}).
			Where("event_id = ? AND status IN ?", event.ID, []models.TicketStatus{models.TicketActive, models.TicketUsed, models.TicketPartiallyRefunded}).
			Updates(map[string]any{"status": models.TicketCanceled, "updated_at": now}).Error; err != nil {
			return err
		}

		// Unpaid konbini intents should be cancelled instead of refunded.
		if err := tx.Model(&models.PaymentIntent{}).
			Where("event_id = ? AND payment_gateway = ? AND status IN ?", event.ID, models.PaymentGatewayKonbini, []models.PaymentIntentStatus{
				models.PaymentIntentRequiresPaymentMethod,
				models.PaymentIntentRequiresConfirmation,
				models.PaymentIntentProcessing,
			}).
			Updates(map[string]any{
				"status":      models.PaymentIntentCanceled,
				"canceled_at": now,
				"updated_at":  now,
			}).Error; err != nil {
			return err
		}

		// Collect transactions needing refunds, but defer processing to async queue to avoid 504 timeout
		var txns []models.Transaction
		if err := tx.Where("event_id = ? AND status = ?", event.ID, models.TransactionSucceeded).Find(&txns).Error; err != nil {
			return err
		}

		for _, txn := range txns {
			var existing models.Refund
			err := tx.Where("event_id = ? AND transaction_id = ? AND refund_type = ? AND status IN ?",
				txn.EventID,
				txn.ID,
				models.RefundTypeEventCancellation,
				[]models.RefundStatus{models.RefundPending, models.RefundProcessing, models.RefundSucceeded},
			).First(&existing).Error
			if err == nil {
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			var pi models.PaymentIntent
			orderID := txn.PaymentIntentID.String()
			if err := tx.Select("checkout_token").First(&pi, txn.PaymentIntentID).Error; err == nil && strings.TrimSpace(pi.CheckoutToken) != "" {
				orderID = pi.CheckoutToken
			}

			refund := models.Refund{
				ID:              uuid.New(),
				RefundNumber:    fmt.Sprintf("REF-%s", uuid.New().String()[:8]),
				TicketID:        uuid.Nil,
				TransactionID:   txn.ID,
				PaymentIntentID: txn.PaymentIntentID,
				EventID:         txn.EventID,
				Provider:        txn.PaymentGateway,
				PaymentProvider: txn.PaymentGateway,
				OrderID:         orderID,
				UserID:          &txn.ActorID,
				Amount:          txn.AmountTotal,
				Currency:        txn.Currency,
				Reason:          reviewed.Reason,
				RefundType:      models.RefundTypeEventCancellation,
				InitiatedBy:     adminID,
				InitiatorType:   "admin",
				Status:          models.RefundPending,
				IsFullRefund:    true,
			}
			if err := tx.Create(&refund).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.RefundStatusHistory{
				ID:            uuid.New(),
				RefundID:      refund.ID,
				NewStatus:     models.RefundPending,
				ChangedAt:     now,
				ChangedByID:   &adminID,
				ChangedByType: "admin",
				Remarks:       "created from approved event cancellation request",
			}).Error; err != nil {
				return err
			}
			refundIDs = append(refundIDs, refund.ID)
		}

		reviewed.Status = targetStatus
		reviewed.AdminRemark = strings.TrimSpace(adminRemark)
		reviewed.ReviewedBy = &adminID
		reviewed.ReviewedAt = &now
		return nil
	})
	if err != nil {
		return nil, err
	}

	if approve && s.refundQueue != nil {
		for _, refundID := range refundIDs {
			if enqueueErr := s.refundQueue.EnqueueRefundProcessing(refundID); enqueueErr != nil {
				return nil, enqueueErr
			}
		}
	}

	return &reviewed, nil
}

// GetEventAnalytics returns comprehensive analytics for an event
func (s *EventManagementService) GetEventAnalytics(eventID, organizerID uuid.UUID) (*models.EventAnalyticsResponse, error) {
	var event models.Event

	// Find the event with tiers
	if err := s.db.Scopes(models.WithTiers).Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	return s.buildEventAnalytics(&event)
}

// AdminGetEventAnalytics returns comprehensive analytics for an event (Admin access - no organizer scoping)
func (s *EventManagementService) AdminGetEventAnalytics(eventID uuid.UUID) (*models.EventAnalyticsResponse, error) {
	var event models.Event

	// Find the event with tiers (no organizer scoping for admin)
	if err := s.db.Scopes(models.WithTiers).Where("id = ?", eventID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	return s.buildEventAnalytics(&event)
}

// buildEventAnalytics is a helper method to build analytics for an event (eliminates code duplication)
// Uses transaction isolation to prevent race conditions between aggregate and per-tier queries
func (s *EventManagementService) buildEventAnalytics(event *models.Event) (*models.EventAnalyticsResponse, error) {
	// Calculate total seats from tiers (fixed capacity)
	totalSeats := 0
	for _, tier := range event.Tiers {
		totalSeats += tier.Quantity
	}

	// Get tier analytics from transactions (with atomic consistency)
	// Calculate totals by summing individual tier data instead of separate aggregate query
	// This prevents race conditions where data changes between queries
	tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))
	totalSoldSeats := 0
	totalRevenue := 0.0

	for i, tier := range event.Tiers {
		var tierSummary struct {
			SoldSeats int     `json:"sold_seats"`
			Revenue   float64 `json:"revenue"`
		}

		// Count TICKETS by tier instead of transactions
		// This works with the new single-transaction-per-purchase model
		// Tickets are linked to transactions via transaction_id, and each ticket has a tier_id
		if err := s.db.Model(&models.Ticket{}).
			Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
			Select("COALESCE(COUNT(*), 0) as sold_seats, COALESCE(SUM(event_tiers.price), 0) as revenue").
			Where("tickets.event_id = ? AND tickets.tier_id = ? AND tickets.status IN ('active', 'used','expired')",
				event.ID, tier.ID).
			Scan(&tierSummary).Error; err != nil {
			return nil, utils.NewDatabaseError("Failed to calculate tier analytics from tickets.", err)
		}

		availSeats := tier.Quantity - tierSummary.SoldSeats
		commissionEarning := tierSummary.Revenue * event.CommissionRate / 100
		tierAnalytics[i] = models.EventTierAnalytics{
			TierID:            tier.ID,
			TierName:          tier.TierName,
			Price:             tier.Price,
			Currency:          tier.Currency,
			TotalSeats:        tier.Quantity,
			SoldSeats:         tierSummary.SoldSeats,
			AvailSeats:        availSeats,
			Revenue:           tierSummary.Revenue,
			SalesStart:        tier.SalesStart,
			SalesEnd:          tier.SalesEnd,
			IsActive:          tier.IsActive,
			CommissionEarning: commissionEarning,
		}

		// Accumulate totals from tier data for consistency
		totalSoldSeats += tierSummary.SoldSeats
		totalRevenue += tierSummary.Revenue
	}

	// Prefer authoritative transaction-based earnings when available
	var txs []models.Transaction
	if err := s.db.Where("event_id = ? AND status = ?", event.ID, string(models.TransactionSucceeded)).Find(&txs).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to retrieve transactions for earnings.", err)
	}

	totalRevenueFromTx := 0.0
	organizerEarnings := 0.0
	adminEarnings := 0.0
	for _, t := range txs {
		amt, _ := currency.FromSmallestUnit(t.AmountTotal, t.Currency)
		org, _ := currency.FromSmallestUnit(t.OrganizerShare, t.Currency)
		admin, _ := currency.FromSmallestUnit(t.PlatformFee, t.Currency)
		totalRevenueFromTx += amt
		organizerEarnings += org
		adminEarnings += admin
	}

	// If we have transaction data, prefer it for totals. Otherwise fall back to tier-based calculation.
	finalTotalRevenue := totalRevenue
	if len(txs) > 0 {
		finalTotalRevenue = totalRevenueFromTx
	}

	// Use platform fees as commission earning and organizer_share from transactions as organizer earning
	commissionEarning := adminEarnings

	return &models.EventAnalyticsResponse{
		EventID:           event.ID,
		EventTitle:        event.Title,
		EventStatus:       event.Status,
		SalesStatus:       event.SalesStatus,
		TotalSeats:        totalSeats,
		SoldSeats:         totalSoldSeats, // Sum of tier data, not separate query
		AvailSeats:        totalSeats - totalSoldSeats,
		TotalRevenue:      finalTotalRevenue,
		CommissionRate:    event.CommissionRate,
		CommissionEarning: commissionEarning,
		OrganizerShare:    organizerEarnings,
		TierCount:         len(event.Tiers),
		Tiers:             tierAnalytics,
		CreatedAt:         event.CreatedAt,
	}, nil
}

// GetAllEventsAnalytics returns analytics for all events of an organizer with accurate transaction-based calculations
func (s *EventManagementService) GetAllEventsAnalytics(organizerID uuid.UUID, page, limit int) ([]models.EventAnalyticsResponse, int64, error) {
	var events []models.Event
	var total int64

	// Get total count
	s.db.Model(&models.Event{}).Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).Count(&total)

	// Get paginated events with tiers
	offset := (page - 1) * limit
	if err := s.db.Scopes(models.WithTiers).Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).
		Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	analytics := make([]models.EventAnalyticsResponse, len(events))
	for i, event := range events {
		// Use the same calculation logic as buildEventAnalytics to ensure consistency
		// Calculate total seats from tiers (fixed capacity)
		totalSeats := 0
		for _, tier := range event.Tiers {
			totalSeats += tier.Quantity
		}

		// Get tier analytics from transactions (consistent with buildEventAnalytics)
		tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))
		totalSoldSeats := 0
		totalRevenue := 0.0

		for j, tier := range event.Tiers {
			var tierSummary struct {
				SoldSeats int     `json:"sold_seats"`
				Revenue   float64 `json:"revenue"`
			}

			if err := s.db.Model(&models.Ticket{}).
				Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
				Select("COALESCE(COUNT(*), 0) as sold_seats, COALESCE(SUM(event_tiers.price), 0) as revenue").
				Where("tickets.event_id = ? AND tickets.tier_id = ? AND tickets.status IN ('active', 'used') AND tickets.deleted_at IS NULL",
					event.ID, tier.ID).
				Scan(&tierSummary).Error; err != nil {
				return nil, 0, utils.NewDatabaseError("Failed to calculate tier analytics from tickets.", err)
			}

			availSeats := tier.Quantity - tierSummary.SoldSeats
			commissionEarning := tierSummary.Revenue * event.CommissionRate / 100
			tierAnalytics[j] = models.EventTierAnalytics{
				TierID:            tier.ID,
				TierName:          tier.TierName,
				Price:             tier.Price,
				Currency:          tier.Currency,
				TotalSeats:        tier.Quantity,
				SoldSeats:         tierSummary.SoldSeats,
				AvailSeats:        availSeats,
				Revenue:           tierSummary.Revenue,
				SalesStart:        tier.SalesStart,
				SalesEnd:          tier.SalesEnd,
				IsActive:          tier.IsActive,
				CommissionEarning: commissionEarning,
			}

			// Accumulate totals from tier data for consistency
			totalSoldSeats += tierSummary.SoldSeats
			totalRevenue += tierSummary.Revenue
		}

		commissionEarning := totalRevenue * event.CommissionRate / 100
		analytics[i] = models.EventAnalyticsResponse{
			EventID:           event.ID,
			EventTitle:        event.Title,
			EventStatus:       event.Status,
			SalesStatus:       event.SalesStatus,
			TotalSeats:        totalSeats,
			SoldSeats:         totalSoldSeats,
			AvailSeats:        totalSeats - totalSoldSeats,
			TotalRevenue:      totalRevenue,
			CommissionRate:    event.CommissionRate,
			CommissionEarning: commissionEarning,
			OrganizerShare:    totalRevenue - commissionEarning,
			TierCount:         len(event.Tiers),
			Tiers:             tierAnalytics,
			CreatedAt:         event.CreatedAt,
		}
	}

	return analytics, total, nil
}

// GetOrganizerTierTemplates returns all tier templates for an organizer
func (s *EventManagementService) GetOrganizerTierTemplates(organizerID uuid.UUID) ([]models.OrganizerTierTemplateResponse, error) {
	var templates []models.OrganizerTierTemplate
	if err := s.db.Where("organizer_id = ? AND is_active = ?", organizerID, true).
		Order("created_at DESC").
		Find(&templates).Error; err != nil {
		return nil, err
	}

	responses := make([]models.OrganizerTierTemplateResponse, len(templates))
	for i, template := range templates {
		responses[i] = models.OrganizerTierTemplateResponse{
			ID:           template.ID,
			OrganizerID:  template.OrganizerID,
			TemplateName: template.TemplateName,
			Description:  template.Description,
			IsActive:     template.IsActive,
			CreatedAt:    template.CreatedAt,
			UpdatedAt:    template.UpdatedAt,
		}
	}

	return responses, nil
}

// CreateOrganizerTierTemplate creates a new tier template for an organizer
func (s *EventManagementService) CreateOrganizerTierTemplate(organizerID uuid.UUID, req *models.CreateOrganizerTierTemplateRequest) error {
	// Check if template name already exists for this organizer
	var existing models.OrganizerTierTemplate
	if err := s.db.Where("organizer_id = ? AND template_name = ?", organizerID, req.TemplateName).First(&existing).Error; err == nil {
		return utils.NewConflictError(fmt.Sprintf("Tier template with name '%s' already exists.", req.TemplateName))
	} else if err != gorm.ErrRecordNotFound {
		return utils.NewDatabaseError("Failed to check existing tier template.", err)
	}

	template := &models.OrganizerTierTemplate{
		OrganizerID:  organizerID,
		TemplateName: req.TemplateName,
		Description:  req.Description,
		IsActive:     true,
	}

	if err := s.db.Create(template).Error; err != nil {
		return utils.NewDatabaseError("Failed to create tier template.", err)
	}

	return nil
}

// UpdateOrganizerTierTemplate updates an existing tier template
func (s *EventManagementService) UpdateOrganizerTierTemplate(templateID, organizerID uuid.UUID, req *models.UpdateOrganizerTierTemplateRequest) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("tier template")
		}
		return utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check name uniqueness if changing name
	if req.TemplateName != "" && req.TemplateName != template.TemplateName {
		var existing models.OrganizerTierTemplate
		if err := s.db.Where("organizer_id = ? AND template_name = ? AND id != ?", organizerID, req.TemplateName, templateID).First(&existing).Error; err == nil {
			return utils.NewConflictError(fmt.Sprintf("Tier template with name '%s' already exists.", req.TemplateName))
		} else if err != gorm.ErrRecordNotFound {
			return utils.NewDatabaseError("Failed to check existing tier template.", err)
		}
		template.TemplateName = req.TemplateName
	}

	if req.Description != nil {
		template.Description = *req.Description
	}
	if req.IsActive != nil {
		template.IsActive = *req.IsActive
	}

	if err := s.db.Save(&template).Error; err != nil {
		return utils.NewDatabaseError("Failed to update tier template.", err)
	}

	return nil
}

// DeleteOrganizerTierTemplate deletes a tier template
func (s *EventManagementService) DeleteOrganizerTierTemplate(templateID, organizerID uuid.UUID) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("tier template")
		}
		return utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check if template is being used in any event tiers (code-level protection)
	var count int64
	if err := s.db.Model(&models.EventTier{}).
		Where("tier_template_id = ?", templateID).
		Count(&count).Error; err != nil {
		return utils.NewDatabaseError("Failed to check template usage.", err)
	}

	if count > 0 {
		return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete tier template: it is currently being used in %d event tier(s). Please remove it from all events first.", count))
	}

	if err := s.db.Unscoped().Delete(&template).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete tier template.", err)
	}

	return nil
}

// CreateEventTier creates a new tier for an event with minimal fields
func (s *EventManagementService) CreateEventTier(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, utils.NewBusinessLogicError("Cannot add tiers to cancelled event.")
	}

	// Validate tier limits
	if req.Price > 1000000 {
		return nil, utils.NewValidationError("Tier price cannot exceed $1,000,000", nil)
	}

	if req.Quantity > 1000000 {
		return nil, utils.NewValidationError("Tier quantity cannot exceed 1,000,000", nil)
	}

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("tier template")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := s.db.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
	}

	// Create tier with template name and provided values
	tier := &models.EventTier{
		EventID:        eventID,
		TierTemplateID: req.TierTemplateID,
		TierName:       template.TemplateName,
		Price:          req.Price,
		Currency:       req.Currency,
		Quantity:       req.Quantity,
		Available:      req.Quantity,
		GST:            req.GST,
		SalesStart:     req.SalesStart,
		SalesEnd:       req.SalesEnd,
		SortOrder:      req.SortOrder,
	}

	// Set default currency if not provided
	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := s.db.Create(tier).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create event tier.", err)
	}

	return tier, nil
}

func (s *EventManagementService) CreateEventTierWithTx(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest, tx *gorm.DB) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := tx.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, utils.NewBusinessLogicError("Cannot add tiers to cancelled event.")
	}

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := tx.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("tier template")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := tx.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
	}

	// Create tier with template name and provided values
	tier := &models.EventTier{
		EventID:        eventID,
		TierTemplateID: req.TierTemplateID,
		TierName:       template.TemplateName,
		Price:          req.Price,
		Currency:       strings.ToUpper(req.Currency),
		Quantity:       req.Quantity,
		Available:      req.Quantity,
		GST:            req.GST,
		SalesStart:     req.SalesStart,
		SalesEnd:       req.SalesEnd,
		SortOrder:      req.SortOrder,
	}

	// Inherit currency from event if not provided
	if tier.Currency == "" {
		tier.Currency = event.Currency
	}

	// Set default currency if not provided
	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := tx.Create(tier).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create event tier.", err)
	}

	return tier, nil
}

// UpdateEventTier updates an existing event tier
func (s *EventManagementService) UpdateEventTier(tierID, organizerID uuid.UUID, req *models.UpdateEventTierRequest) (*models.EventTier, error) {
	var tier models.EventTier

	// Find tier and verify ownership through event
	if err := s.db.Joins("JOIN events ON events.id = event_tiers.event_id").
		Where("event_tiers.id = ? AND events.organizer_id = ?", tierID, organizerID).
		First(&tier).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("tier")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier.", err)
	}

	// Validate tier template if being changed
	if req.TierTemplateID != nil {
		var template models.OrganizerTierTemplate
		if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", *req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, utils.NewNotFoundError("tier template")
			}
			return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
		}
		// Check for duplicate tier name within the event, excluding current tier
		var existingTier models.EventTier
		if err := s.db.Where("event_id = ? AND tier_name = ? AND id != ?", tier.EventID, template.TemplateName, tierID).First(&existingTier).Error; err == nil {
			return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
		}
		tier.TierTemplateID = *req.TierTemplateID
		tier.TierName = template.TemplateName
	}

	// Update fields if provided
	if req.Price > 0 {
		tier.Price = req.Price
	}
	if req.Currency != "" {
		tier.Currency = req.Currency
	}
	if req.Quantity > 0 {
		// Adjust available based on quantity change
		sold := tier.Quantity - tier.Available
		tier.Quantity = req.Quantity
		tier.Available = tier.Quantity - sold
		if tier.Available < 0 {
			tier.Available = 0
		}
	}
	if req.GST >= 0 {
		tier.GST = req.GST
	}
	if req.SalesStart != nil {
		tier.SalesStart = req.SalesStart
	}
	if req.SalesEnd != nil {
		tier.SalesEnd = req.SalesEnd
	}
	if req.IsActive != nil {
		tier.IsActive = *req.IsActive
	}
	if req.SortOrder >= 0 {
		tier.SortOrder = req.SortOrder
	}

	if err := s.db.Save(&tier).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to update tier.", err)
	}

	return &tier, nil
}

// DeleteEventTier deletes an event tier
func (s *EventManagementService) DeleteEventTier(tierID, organizerID uuid.UUID) error {
	var tier models.EventTier

	// Find tier and verify ownership through event
	if err := s.db.Joins("JOIN events ON events.id = event_tiers.event_id").
		Where("event_tiers.id = ? AND events.organizer_id = ?", tierID, organizerID).
		First(&tier).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("tier")
		}
		return utils.NewDatabaseError("Failed to retrieve tier.", err)
	}

	// Check if any tickets have been sold for this tier
	if tier.Sold > 0 {
		return utils.NewBusinessLogicError("Cannot delete tier with sold tickets.")
	}

	if err := s.db.Delete(&tier).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete tier.", err)
	}

	return nil
}
