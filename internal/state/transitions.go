package state

import "event-ticketing-backend/internal/models"

// ==============================
// PaymentIntent State Machine
// ==============================

var PaymentIntentTransitions = map[models.PaymentIntentStatus][]models.PaymentIntentStatus{

	models.PaymentIntentRequiresPaymentMethod: {
		models.PaymentIntentSucceeded,
		models.PaymentIntentFailed,
		models.PaymentIntentRequiresConfirmation,
		models.PaymentIntentCanceled,
	},

	models.PaymentIntentRequiresConfirmation: {
		models.PaymentIntentProcessing,
		models.PaymentIntentCanceled,
		models.PaymentIntentSucceeded,
		models.PaymentIntentFailed,
	},

	models.PaymentIntentProcessing: {
		models.PaymentIntentSucceeded,
		models.PaymentIntentCanceled,
		models.PaymentIntentExpired,
		models.PaymentIntentFailed,
	},

	// terminal states
	models.PaymentIntentSucceeded: {},
	models.PaymentIntentCanceled:  {},
	models.PaymentIntentExpired:   {},
	models.PaymentIntentFailed:    {},
}

// ==============================
// PaymentAttempt State Machine
// ==============================

var PaymentAttemptTransitions = map[models.PaymentAttemptStatus][]models.PaymentAttemptStatus{

	models.PaymentAttemptInitiated: {
		models.PaymentAttemptFailed,
		models.PaymentAttemptAuthorized,
	},

	models.PaymentAttemptAuthorized: {
		models.PaymentAttemptFailed,
		// success usually creates transaction instead of changing attempt
	},

	// terminal
	models.PaymentAttemptFailed: {},
}

// ==============================
// Transaction State Machine
// ==============================

// ==============================
// Transaction State Machine
// ==============================

var TransactionTransitions = map[models.TransactionStatus][]models.TransactionStatus{

	// NEW TRANSACTION FLOW
	models.TransactionPending: {
		models.TransactionProcessing,
		models.TransactionSucceeded, // ✅ allow direct success from webhook
		models.TransactionFailed,
		models.TransactionCanceled,
		models.TransactionExpired,
	},

	models.TransactionProcessing: {
		models.TransactionSucceeded,
		models.TransactionFailed,
		models.TransactionCanceled,
		models.TransactionExpired,
	},

	models.TransactionSucceeded: {
		models.TransactionRefunded,
	},

	// terminal
	models.TransactionFailed:   {},
	models.TransactionCanceled: {},
	models.TransactionExpired:  {},
	models.TransactionRefunded: {},
}

// ==============================
// Refund State Machine
// ==============================

var RefundTransitions = map[models.RefundStatus][]models.RefundStatus{

	models.RefundPending: {
		models.RefundProcessing, // gateway/manual: pending → processing → succeeded
		models.RefundRejected,   // admin rejects
		models.RefundSucceeded,  // allowed direct completion when applicable
		models.RefundCancelled,
	},

	models.RefundProcessing: {
		models.RefundSucceeded,
		models.RefundFailed,
		models.RefundCancelled,
	},

	models.RefundFailed: {
		models.RefundProcessing,
		models.RefundCancelled,
	},

	// terminal
	models.RefundSucceeded: {},
	models.RefundCancelled: {},
	models.RefundRejected:  {},
}

// ==============================
// Ticket State Machine (optional)
// ==============================

var TicketTransitions = map[models.TicketStatus][]models.TicketStatus{

	models.TicketActive: {
		models.TicketCheckedIn,
		models.TicketCanceled,
		models.TicketRefunded,
		models.TicketPartiallyRefunded,
	},

	models.TicketPartiallyRefunded: {
		models.TicketRefunded,
	},

	// terminal
	models.TicketCheckedIn: {},
	models.TicketCanceled:  {},
	models.TicketRefunded:  {},
}

// ==============================
// Event State Machine
// ==============================

var EventTransitions = map[models.EventStatus][]models.EventStatus{
	models.EventStatusDraft: {
		models.EventStatusPending,
		models.EventStatusCancelled,
	},

	models.EventStatusPending: {
		models.EventStatusScheduled,
		models.EventStatusOnSale,
		models.EventStatusRejected,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
	},

	models.EventStatusApproved: {
		models.EventStatusScheduled,
		models.EventStatusOnSale,
		models.EventStatusRejected,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
	},

	models.EventStatusRejected: {
		models.EventStatusPending,
		models.EventStatusCancelled,
	},

	models.EventStatusScheduled: {
		models.EventStatusOnSale,
		models.EventStatusSalesUpcoming,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusHold,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusSalesUpcoming: {
		models.EventStatusOnSale,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusHold,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusOnSale: {
		models.EventStatusSalesUpcoming,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusHold,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusSalesEnd: {
		models.EventStatusOnSale,
		models.EventStatusLive,
		models.EventStatusHold,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusHold: {
		models.EventStatusOnSale,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusLive: {
		models.EventStatusCancelPending,
		models.EventStatusCompleted,
		models.EventStatusCancelled,
	},

	models.EventStatusHeld: {
		models.EventStatusOnSale,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusCancelPending,
		models.EventStatusCancelled,
		models.EventStatusCompleted,
	},

	models.EventStatusCancelPending: {
		models.EventStatusCancelled,
		models.EventStatusScheduled,
		models.EventStatusOnSale,
		models.EventStatusSalesUpcoming,
		models.EventStatusSalesEnd,
		models.EventStatusLive,
		models.EventStatusHold,
	},

	models.EventStatusCompleted: {},
	models.EventStatusCancelled: {},
}

// ==============================
// Event Sales State Machine
// ==============================

var EventSalesTransitions = map[models.EventSalesStatus][]models.EventSalesStatus{
	models.EventSalesStatusActive: {
		models.EventSalesStatusPaused,
		models.EventSalesStatusStopped,
	},
	models.EventSalesStatusPaused: {
		models.EventSalesStatusActive,
		models.EventSalesStatusStopped,
	},
	models.EventSalesStatusStopped: {
		models.EventSalesStatusActive,
		models.EventSalesStatusPaused,
	},
}
