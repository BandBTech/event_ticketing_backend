package models

// PaymentGateway represents the available payment gateway options
type PaymentGateway string

const (
	PaymentGatewayCash     PaymentGateway = "cash"
	PaymentGatewayStripe   PaymentGateway = "stripe"
	PaymentGatewayPayPal   PaymentGateway = "paypal"
	PaymentGatewayEsewa    PaymentGateway = "esewa"
	PaymentGatewayKhalti   PaymentGateway = "khalti"
	PaymentGatewayIMEPay   PaymentGateway = "imepay"
)

// String returns the string representation of PaymentGateway
func (pg PaymentGateway) String() string {
	return string(pg)
}

// IsValid checks if the payment gateway is valid
func (pg PaymentGateway) IsValid() bool {
	switch pg {
	case PaymentGatewayCash, PaymentGatewayStripe, PaymentGatewayPayPal, PaymentGatewayEsewa, PaymentGatewayKhalti, PaymentGatewayIMEPay:
		return true
	default:
		return false
	}
}

// PaymentMethod represents the available payment method options
type PaymentMethod string

const (
	PaymentMethodBankTransfer PaymentMethod = "bank_transfer"
	PaymentMethodCheck        PaymentMethod = "check"
	PaymentMethodCash         PaymentMethod = "cash"
	PaymentMethodDigitalWallet PaymentMethod = "digital_wallet"
	PaymentMethodCard         PaymentMethod = "card"
	PaymentMethodUPI          PaymentMethod = "upi"
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