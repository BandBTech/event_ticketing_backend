package state

import "event-ticketing-backend/internal/models"

// ==============================
// PaymentIntent State Machine
// ==============================

var PaymentIntentTransitions = map[models.PaymentIntentStatus][]models.PaymentIntentStatus{

	models.PaymentIntentRequiresPaymentMethod: {
		models.PaymentIntentRequiresConfirmation,
		models.PaymentIntentCanceled,
	},

	models.PaymentIntentRequiresConfirmation: {
		models.PaymentIntentProcessing,
		models.PaymentIntentCanceled,
	},

	models.PaymentIntentProcessing: {
		models.PaymentIntentSucceeded,
		models.PaymentIntentCanceled,
		models.PaymentIntentExpired,
	},

	// terminal states
	models.PaymentIntentSucceeded: {},
	models.PaymentIntentCanceled:  {},
	models.PaymentIntentExpired:   {},
}

// ==============================
// PaymentAttempt State Machine
// ==============================

var PaymentAttemptTransitions = map[models.PaymentAttemptStatus][]models.PaymentAttemptStatus{

	models.PaymentAttemptInitiated: {
		models.PaymentAttemptPending,
		models.PaymentAttemptFailed,
	},

	models.PaymentAttemptPending: {
		models.PaymentAttemptAuthorized,
		models.PaymentAttemptFailed,
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

var TransactionTransitions = map[models.TransactionStatus][]models.TransactionStatus{

	models.TransactionPending: {
		models.TransactionProcessing,
		models.TransactionFailed,
	},

	models.TransactionProcessing: {
		models.TransactionSucceeded,
		models.TransactionFailed,
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
		models.RefundProcessing,
		models.RefundRejected,
	},

	models.RefundProcessing: {
		models.RefundSucceeded,
		models.RefundFailed,
	},

	// terminal
	models.RefundSucceeded: {},
	models.RefundFailed:    {},
	models.RefundRejected:  {},
}

// ==============================
// Ticket State Machine (optional)
// ==============================

var TicketTransitions = map[models.TicketStatus][]models.TicketStatus{

	models.TicketActive: {
		models.TicketUsed,
		models.TicketCanceled,
		models.TicketRefunded,
		models.TicketPartiallyRefunded,
	},

	models.TicketPartiallyRefunded: {
		models.TicketRefunded,
	},

	// terminal
	models.TicketUsed:     {},
	models.TicketCanceled: {},
	models.TicketRefunded: {},
}
