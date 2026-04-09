package models

// PaymentGateway represents the available payment gateway options
type PaymentGateway string

const (
	PaymentGatewayCash   PaymentGateway = "cash"
	PaymentGatewayStripe PaymentGateway = "STRIPE"
	PaymentGatewayEsewa  PaymentGateway = "ESEWA"
	PaymentGatewayKhalti PaymentGateway = "KHALTI"
	PaymentGatewayPayPal PaymentGateway = "paypal"
	PaymentGatewayIMEPay PaymentGateway = "imepay"
)

// ActivePaymentGateways contains the currently supported payment gateways for purchases
// Only add gateways here that are fully integrated and tested for purchases
//
// HOW TO ADD A NEW PAYMENT GATEWAY FOR PURCHASES:
// 1. Add the gateway constant above (e.g., PaymentGatewayPayPal)
// 2. Add it to ActivePaymentGateways ONLY when fully integrated and tested
// 3. Implement the gateway logic in payment services
// 4. Test thoroughly before enabling for purchases
//
// Example for adding PayPal:
// const PaymentGatewayPayPal PaymentGateway = "paypal"
// var ActivePaymentGateways = []PaymentGateway{ PaymentGatewayCash, PaymentGatewayStripe, PaymentGatewayPayPal }
var ActivePaymentGateways = []PaymentGateway{
	PaymentGatewayCash,   // Always allowed - no integration needed
	PaymentGatewayStripe, // Fully integrated and tested
	// Add new gateways here when they are fully integrated:
	// PaymentGatewayPayPal,  // Uncomment when PayPal is integrated
	// PaymentGatewayEsewa,   // Uncomment when eSewa is integrated
	// PaymentGatewayKhalti,  // Uncomment when Khalti is integrated
	// PaymentGatewayIMEPay,  // Uncomment when IME Pay is integrated
}

// String returns the string representation of PaymentGateway
func (pg PaymentGateway) String() string {
	return string(pg)
}

// IsValid checks if the payment gateway is valid and currently active for purchases
func (pg PaymentGateway) IsValid() bool {
	for _, activePG := range ActivePaymentGateways {
		if pg == activePG {
			return true
		}
	}
	return false
}

// PaymentMethod represents the available payment method options
type PaymentMethod string

const (
	PaymentMethodBankTransfer  PaymentMethod = "bank_transfer"
	PaymentMethodCheck         PaymentMethod = "check"
	PaymentMethodCash          PaymentMethod = "cash"
	PaymentMethodDigitalWallet PaymentMethod = "digital_wallet"
	PaymentMethodCard          PaymentMethod = "card"
	PaymentMethodUPI           PaymentMethod = "upi"
)

// String returns the string representation of PaymentMethod
func (pm PaymentMethod) String() string {
	return string(pm)
}

// IsValid checks if the payment method is valid
func (pm PaymentMethod) IsValid() bool {
	switch pm {
	case PaymentMethodBankTransfer, PaymentMethodCheck, PaymentMethodCash, PaymentMethodDigitalWallet, PaymentMethodCard, PaymentMethodUPI:
		return true
	default:
		return false
	}
}
