package gateways

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/checkout/session"
	"github.com/stripe/stripe-go/v83/webhook"
)

type konbiniGateway struct {
	apiKey        string
	webhookSecret string
	expiryDays    int64
}

func NewKonbini(apiKey, webhookSecret string, expiryDays int) Gateway {
	if expiryDays < 1 || expiryDays > 3 {
		expiryDays = 3
	}

	return &konbiniGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		expiryDays:    int64(expiryDays),
	}
}

func (g *konbiniGateway) Name() string {
	return "konbini"
}

func (g *konbiniGateway) InitSession(
	_ context.Context,
	req *SessionRequest,
) (*SessionResponse, error) {

	if !strings.EqualFold(req.Currency, "JPY") {
		return nil, fmt.Errorf(
			"konbini supports only JPY currency, got: %s",
			req.Currency,
		)
	}

	stripe.Key = g.apiKey

	lineItems := make([]*stripe.CheckoutSessionLineItemParams, 0, len(req.LineItems))

	for _, item := range req.LineItems {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: stripe.Int64(int64(item.Quantity)),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String("jpy"),

				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String(item.Name),
					Description: stripe.String(item.Description),
				},

				// JPY is zero-decimal
				UnitAmount: stripe.Int64(item.UnitAmount),
			},
		})
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),

		CustomerEmail: stripe.String(req.CustomerEmail),

		SuccessURL: stripe.String(req.SuccessURL),
		CancelURL:  stripe.String(req.CancelURL),

		LineItems: lineItems,

		PaymentMethodTypes: stripe.StringSlice([]string{
			"konbini",
		}),

		PaymentMethodOptions: &stripe.CheckoutSessionPaymentMethodOptionsParams{
			Konbini: &stripe.CheckoutSessionPaymentMethodOptionsKonbiniParams{
				ExpiresAfterDays: stripe.Int64(g.expiryDays),
			},
		},
	}

	for k, v := range req.Metadata {
		params.AddMetadata(k, v)
	}

	sess, err := session.New(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create konbini checkout session: %w", err)
	}

	return &SessionResponse{
		GatewaySessionID: sess.ID,

		// Customer lands here after Stripe renders instructions
		RedirectURL: sess.URL,
	}, nil
}

func (g *konbiniGateway) CreateRefund(
	_ context.Context,
	_ *RefundRequest,
) (*RefundResponse, error) {

	// Stripe does not support refunds for Konbini cash payments
	return nil, ErrNotRefundable
}

func (g *konbiniGateway) VerifyWebhookSignature(
	body []byte,
	signature string,
) (*WebhookEvent, error) {

	event, err := webhook.ConstructEvent(
		body,
		signature,
		g.webhookSecret,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid stripe webhook signature: %w",
			err,
		)
	}

	checkoutToken, err := extractStripeCheckoutTokenV83(event)
	if err != nil {
		return nil, err
	}

	return &WebhookEvent{
		ProviderEventID: event.ID,
		EventType:       string(event.Type),
		CheckoutToken:   checkoutToken,
		RawObject:       event.Data.Raw,
	}, nil
}

// GetRefund returns an error for konbini as it is not refundable via gateway
func (g *konbiniGateway) GetRefund(_ context.Context, providerRefundID string) (*RefundResponse, error) {
	return nil, ErrNotRefundable
}

func extractStripeCheckoutTokenV83(event stripe.Event) (string, error) {

	switch event.Type {

	case "checkout.session.completed":
		var s stripe.CheckoutSession

		if err := json.Unmarshal(event.Data.Raw, &s); err != nil {
			return "", err
		}

		token := s.Metadata["checkout_token"]

		if token == "" {
			return "", fmt.Errorf("checkout_token missing in session metadata")
		}

		return token, nil

	case "payment_intent.succeeded":
		var pi stripe.PaymentIntent

		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return "", err
		}

		token := pi.Metadata["checkout_token"]

		if token == "" {
			return "", fmt.Errorf("checkout_token missing in payment intent metadata")
		}

		return token, nil
	}

	return "", fmt.Errorf(
		"unsupported webhook event type: %s",
		event.Type,
	)
}
