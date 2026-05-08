package services

import (
"context"
"errors"
"fmt"
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
//   Admin: ApproveForStripe → calls gateway → processing (webhook → succeeded)
//   Admin: ApproveForBillings → creates RefundBill → succeeded directly
//   Admin: RejectRefund → rejected (terminal)

type RefundService struct {
db         *gorm.DB
gwRegistry *gateways.Registry
sm         *state.StateMachine[models.RefundStatus]
refundCalc *RefundCalculator
}

func NewRefundService(
db *gorm.DB,
gw *gateways.Registry,
sm *state.StateMachine[models.RefundStatus],
refundCalc *RefundCalculator,
) *RefundService {
return &RefundService{
db:         db,
gwRegistry: gw,
sm:         sm,
refundCalc: refundCalc,
}
}

// ─── Input types ────────────────────────────────────────────────────────────

// AdminCancelTicketRequest is the input for admin-initiated ticket cancellation.
type AdminCancelTicketRequest struct {
TicketID     *uuid.UUID // one of these must be set
TicketNumber string
Reason       string
}

// ApproveForBillingsRequest contains the manual bank-transfer details for konbini refunds.
type ApproveForBillingsRequest struct {
BankName          string `json:"bank_name" binding:"required"`
AccountHolderName string `json:"account_holder_name" binding:"required"`
AccountNumber     string `json:"account_number" binding:"required"`
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
) (*models.Refund, error) {
return s.createRefund(ctx, ticketID, userID, "user", reason, false)
}

// ─── Admin flow ──────────────────────────────────────────────────────────────

// AdminCancelTicket lets an admin cancel a ticket by ID or ticket number.
// It creates a pending Refund that still requires explicit approval.
func (s *RefundService) AdminCancelTicket(
ctx context.Context,
req AdminCancelTicketRequest,
adminID uuid.UUID,
) (*models.Refund, error) {
// Resolve ticket
var ticket models.Ticket
if req.TicketID != nil {
if err := s.db.WithContext(ctx).
Preload("Event").
First(&ticket, *req.TicketID).Error; err != nil {
return nil, utils.NewNotFoundError("ticket")
}
} else if req.TicketNumber != "" {
if err := s.db.WithContext(ctx).
Preload("Event").
Where("ticket_number = ?", req.TicketNumber).
First(&ticket).Error; err != nil {
return nil, utils.NewNotFoundError("ticket")
}
} else {
return nil, utils.NewValidationError("ticket_id or ticket_number is required", nil)
}

return s.createRefund(ctx, ticket.ID, adminID, "admin", req.Reason, true)
}

// ─── Admin approval/rejection ─────────────────────────────────────────────

// ApproveForStripe approves a pending refund for a Stripe payment.
// It calls the Stripe API which moves the refund to "processing".
// The refund is completed when Stripe sends the "charge.refunded" webhook.
func (s *RefundService) ApproveForStripe(
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
return fmt.Errorf("stripe refund call failed: %w", err)
}

return tx.Model(&refund).Update("provider_refund_id", resp.GatewayRefundID).Error
})

return &refund, err
}

// ApproveForBillings approves a pending refund for a konbini (cash) payment.
// It creates a RefundBill record with the supplied bank-transfer details,
// marks the ticket as refunded, and sets the refund status to succeeded directly
// (no gateway call needed — the admin will physically transfer the money).
func (s *RefundService) ApproveForBillings(
ctx context.Context,
refundID uuid.UUID,
adminID uuid.UUID,
req ApproveForBillingsRequest,
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

if err := s.sm.Transition(refund.Status, models.RefundSucceeded); err != nil {
return err
}

// Resolve user info for the bill
var ticket models.Ticket
if err := tx.First(&ticket, refund.TicketID).Error; err != nil {
return err
}

userName, userEmail := s.resolveUserInfo(tx, ticket)

// Create billing record
bill := &models.RefundBill{
BillNumber:        s.generateBillNumber(),
RefundID:          refund.ID,
AdminID:           adminID,
UserName:          userName,
UserEmail:         userEmail,
Amount:            refund.Amount,
Currency:          refund.Currency,
BankName:          req.BankName,
AccountHolderName: req.AccountHolderName,
AccountNumber:     req.AccountNumber,
RoutingNumber:     req.RoutingNumber,
Notes:             req.Notes,
Status:            "pending",
}
if ticket.ActorType == models.ActorUser {
bill.UserID = &ticket.ActorID
}
if err := tx.Create(bill).Error; err != nil {
return err
}

// Mark ticket refunded and link bill
now := time.Now()
if err := tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(map[string]any{
"status":       models.TicketRefunded,
"refund_id":    refund.ID,
"refunded_at":  now,
"refund_amount": refund.Amount,
}).Error; err != nil {
return err
}

// Restore tier inventory
s.restoreInventory(tx, ticket.TierID)

		// Finalize refund
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":         models.RefundSucceeded,
			"approved_by":    adminID,
			"approved_at":    now,
			"processed_at":   now,
			"refund_bill_id": bill.ID,
		}).Error; err != nil {
			return err
		}

		return s.logRefundStatusHistory(tx, refund.ID, models.RefundPending, models.RefundSucceeded, &adminID, "admin", "approved for billing")
	})

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
return tx.Model(&refund).Updates(map[string]any{
"status":           models.RefundRejected,
"rejected_by":      adminID,
"rejected_at":      now,
"rejection_reason": reason,
}).Error
})

return &refund, err
}

// ─── Stripe webhook ──────────────────────────────────────────────────────────

// ConfirmRefundWebhook is called when Stripe sends a "charge.refunded" event.
// It transitions the refund to succeeded and marks the ticket as refunded.
func (s *RefundService) ConfirmRefundWebhook(
ctx context.Context,
providerRefundID string,
) error {
return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
var refund models.Refund
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

return nil
})
}

// ─── Query methods ────────────────────────────────────────────────────────────

// AdminGetAllRefundsList returns a paginated, filtered list of refunds.
func (s *RefundService) AdminGetAllRefundsList(
ctx context.Context,
status, search, refundType string,
startDate, endDate *time.Time,
page, limit int,
sortBy, sortOrder string,
) ([]models.Refund, int64, error) {
var refunds []models.Refund
var total int64

query := s.db.WithContext(ctx).Model(&models.Refund{}).Preload("Event")

if status != "" {
query = query.Where("status = ?", status)
}

if refundType == "full" {
query = query.Where("is_full_refund = ?", true)
} else if refundType == "partial" {
query = query.Where("is_full_refund = ?", false)
}

if search != "" {
query = query.Joins("JOIN events ON refunds.event_id = events.id").
Where("events.title ILIKE ?", "%"+search+"%")
}

if startDate != nil {
query = query.Where("refunds.created_at >= ?", *startDate)
}
if endDate != nil {
query = query.Where("refunds.created_at <= ?", *endDate)
}

if err := query.Count(&total).Error; err != nil {
return nil, 0, err
}

offset := (page - 1) * limit
if err := query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder)).
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

// GetUserRefunds returns all refunds initiated by a given user.
func (s *RefundService) GetUserRefunds(
ctx context.Context,
userID uuid.UUID,
page, limit int,
) ([]models.Refund, int64, error) {
var refunds []models.Refund
var total int64

query := s.db.WithContext(ctx).Model(&models.Refund{}).
Where("initiated_by = ? AND initiator_type = ?", userID, "user")

if err := query.Count(&total).Error; err != nil {
return nil, 0, err
}

offset := (page - 1) * limit
if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&refunds).Error; err != nil {
return nil, 0, err
}

return refunds, total, nil
}

// GetUserRefundStatusHistory returns the status-change history for a refund,
// verifying that the refund was initiated by the requesting user.
func (s *RefundService) GetUserRefundStatusHistory(
ctx context.Context,
userID uuid.UUID,
refundID uuid.UUID,
) ([]models.RefundStatusHistory, error) {
// Ownership check
var refund models.Refund
if err := s.db.WithContext(ctx).
Where("id = ? AND initiated_by = ? AND initiator_type = ?", refundID, userID, "user").
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
func (s *RefundService) createRefund(
ctx context.Context,
ticketID uuid.UUID,
initiatorID uuid.UUID,
initiatorType string,
reason string,
isAdmin bool,
) (*models.Refund, error) {
var returnRefund models.Refund

err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
var ticket models.Ticket
if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
Preload("Event").
First(&ticket, ticketID).Error; err != nil {
return utils.NewNotFoundError("ticket")
}

// Ownership check (users can only cancel their own tickets)
if !isAdmin && ticket.ActorID != initiatorID {
return utils.NewForbiddenError("ticket does not belong to this user")
}

// Eligibility checks
if err := s.checkTicketEligibility(&ticket, isAdmin); err != nil {
return err
}

// Block duplicate pending refunds for the same ticket
var existing models.Refund
if err := tx.Where("ticket_id = ? AND status IN ?", ticketID,
[]models.RefundStatus{models.RefundPending, models.RefundProcessing},
).First(&existing).Error; err == nil {
return fmt.Errorf("a refund is already in progress for this ticket")
} else if !errors.Is(err, gorm.ErrRecordNotFound) {
return err
}

// Load transaction for amount calculation
var txn models.Transaction
if err := tx.First(&txn, ticket.TransactionID).Error; err != nil {
return fmt.Errorf("transaction not found for ticket: %w", err)
}

// Calculate per-ticket refund amount
refundAmount, err := s.refundCalc.CalculateRefundForTicket(&txn)
if err != nil {
return fmt.Errorf("failed to calculate refund amount: %w", err)
}

// Mark ticket as cancelled
if err := tx.Model(&ticket).Update("status", models.TicketCanceled).Error; err != nil {
return err
}

refund := &models.Refund{
ID:              uuid.New(),
RefundNumber:    fmt.Sprintf("REF-%s", uuid.New().String()[:8]),
TicketID:        ticketID,
TransactionID:   txn.ID,
PaymentIntentID: txn.PaymentIntentID,
EventID:         txn.EventID,
Provider:        txn.PaymentGateway,
Amount:          refundAmount,
Currency:        txn.Currency,
Reason:          reason,
InitiatedBy:     initiatorID,
InitiatorType:   initiatorType,
Status:          models.RefundPending,
IsFullRefund:    true, // one ticket = full unit refund
}

		if err := tx.Create(refund).Error; err != nil {
			return err
		}

		if err := s.logRefundStatusHistory(tx, refund.ID, "", models.RefundPending, &initiatorID, initiatorType, "refund requested"); err != nil {
			return err
		}

		returnRefund = *refund
		return nil
	})

	return &returnRefund, err
}

// checkTicketEligibility validates whether a ticket can be cancelled/refunded.
// Admins bypass the time-window check but still cannot refund used or already-refunded tickets.
func (s *RefundService) checkTicketEligibility(ticket *models.Ticket, isAdmin bool) error {
if ticket.Status == models.TicketRefunded {
return utils.NewBusinessLogicError("ticket has already been refunded")
}
if ticket.Status == models.TicketCanceled {
return utils.NewBusinessLogicError("ticket is already cancelled")
}
if ticket.Status == models.TicketUsed || ticket.CheckedInAt != nil {
return utils.NewBusinessLogicError("ticket has already been used or checked in")
}

if !isAdmin {
if ticket.Event == nil {
return utils.NewBusinessLogicError("event information unavailable")
}
now := time.Now()
if ticket.Event.EndDate.Before(now) {
return utils.NewBusinessLogicError("event has already ended")
}
// Refund window: must be more than 2 hours before event start
refundCutoff := ticket.Event.StartDate.Add(-2 * time.Hour)
if now.After(refundCutoff) {
return utils.NewBusinessLogicError("refund window has closed (must be >2 hours before event start)")
}
}

return nil
}

// restoreInventory increments available count for the tier after a refund.
func (s *RefundService) restoreInventory(tx *gorm.DB, tierID uuid.UUID) {
tx.Exec(`UPDATE event_tiers SET available_quantity = available_quantity + 1 WHERE id = ?`, tierID)
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
