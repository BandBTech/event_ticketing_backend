package models

// PaymentIntent Status tracks the lifecycle of a payment intent, from creation to completion or failure.
type PaymentIntentStatus string

const (
	PaymentIntentRequiresPaymentMethod PaymentIntentStatus = "requires_payment_method"
	PaymentIntentRequiresConfirmation  PaymentIntentStatus = "requires_confirmation"
	PaymentIntentProcessing            PaymentIntentStatus = "processing"
	PaymentIntentSucceeded             PaymentIntentStatus = "succeeded"
	PaymentIntentCanceled              PaymentIntentStatus = "canceled"
	PaymentIntentExpired               PaymentIntentStatus = "expired"
	PaymentIntentFailed                PaymentIntentStatus = "failed"
)

// PaymentAttemptStatus tracks the lifecycle of a payment attempt, including retries and failures.
type PaymentAttemptStatus string

const (
	PaymentAttemptInitiated  PaymentAttemptStatus = "initiated"
	PaymentAttemptAuthorized PaymentAttemptStatus = "authorized"
	PaymentAttemptFailed     PaymentAttemptStatus = "failed"
)

// PaymentStatus tracks the lifecycle of a PaymentIntent.
type TransactionStatus string

const (
	TransactionPending    TransactionStatus = "pending"
	TransactionProcessing TransactionStatus = "processing"
	TransactionSucceeded  TransactionStatus = "succeeded"
	TransactionFailed     TransactionStatus = "failed"
	TransactionCanceled   TransactionStatus = "canceled"
	TransactionExpired    TransactionStatus = "expired"
	TransactionRefunded   TransactionStatus = "refunded"
)

// ReservationStatus tracks ticket-hold lifecycle.
type ReservationStatus string

const (
	ReservationReserved  ReservationStatus = "reserved"
	ReservationConfirmed ReservationStatus = "confirmed"
	ReservationExpired   ReservationStatus = "expired"
)

// TicketStatus tracks the issued ticket lifecycle.
type TicketStatus string

const (
	TicketActive            TicketStatus = "active"
	TicketUsed              TicketStatus = "used"
	TicketCanceled          TicketStatus = "canceled"
	TicketRefunded          TicketStatus = "refunded"
	TicketPartiallyRefunded TicketStatus = "partially_refunded"
)

// PaymentGateway represents the available payment gateway options
type PaymentGateway string

const (
	PaymentGatewayStripe   PaymentGateway = "stripe"
	PaymentGatewayPayPal   PaymentGateway = "paypal"
	PaymentGatewayEsewa    PaymentGateway = "esewa"
	PaymentGatewayKhalti   PaymentGateway = "khalti"
	PaymentGatewayIMEPay   PaymentGateway = "imepay"
	PaymentGatewayRazorpay PaymentGateway = "razorpay"
	PaymentGatewayKonbini  PaymentGateway = "konbini"
	PaymentGatewayPayPay   PaymentGateway = "paypay"
)

// RefundStatus tracks refund lifecycle.
type RefundStatus string

const (
	RefundPending    RefundStatus = "pending"
	RefundProcessing RefundStatus = "processing"
	RefundSucceeded  RefundStatus = "succeeded"
	RefundFailed     RefundStatus = "failed"
	RefundRejected   RefundStatus = "rejected"
)

// ActorType distinguishes logged-in users from guests.
type ActorType string

const (
	ActorUser  ActorType = "user"
	ActorGuest ActorType = "guest"
)

// TicketRefundStatus tracks the refund status of individual tickets.
type TicketRefundStatus string

const (
	TicketRefundNone    TicketRefundStatus = "none"
	TicketRefundPartial TicketRefundStatus = "partial"
	TicketRefundFull    TicketRefundStatus = "full"
)

// RefundInitiatorType tracks who initiated the refund.
type RefundInitiatorType string

const (
	RefundInitiatorCustomer RefundInitiatorType = "customer_request"
	RefundInitiatorAdmin    RefundInitiatorType = "admin_request"
)
