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
