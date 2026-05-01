package services

import (
	"fmt"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/checkout/session"
	"gorm.io/gorm"
)

type PaymentGatewayService struct {
	db  *gorm.DB
	cfg *config.Config
}

// =======================================================
// RESPONSE
// =======================================================

type GatewayInitResponse struct {
	SessionID string
	URL       string
}

// =======================================================
// MAIN ENTRY
// =======================================================

func (s *PaymentGatewayService) InitializeGatewayData(
	paymentIntent *models.PaymentIntent,
	req *UnifiedPurchaseRequest,
	eventTitle string,
) (*GatewayInitResponse, error) {

	switch paymentIntent.PaymentGateway {

	// =========================
	// STRIPE
	// =========================
	case models.PaymentGatewayStripe:

		stripe.Key = s.cfg.Payment.Gateways.StripeAPIKey

		lineItems := []*stripe.CheckoutSessionLineItemParams{}

		// NOTE: You may want to fetch tiers again OR pass them in request (recommended)
		for _, t := range req.Tiers {

			var tier models.EventTier
			if err := s.db.Where("id = ?", t.TierID).First(&tier).Error; err != nil {
				return nil, fmt.Errorf("tier not found: %w", err)
			}

			lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(tier.Currency)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String(eventTitle + " - " + tier.TierName),
						Description: stripe.String(fmt.Sprintf("%d tickets", t.Quantity)),
					},
					UnitAmount: stripe.Int64(int64(tier.Price * 100)),
				},
				Quantity: stripe.Int64(int64(t.Quantity)),
			})
		}

		params := &stripe.CheckoutSessionParams{
			LineItems:     lineItems,
			Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
			CustomerEmail: stripe.String(req.Email),
			Currency:      stripe.String(string(paymentIntent.Currency)),

			SuccessURL: stripe.String(
				fmt.Sprintf("%s?checkout_token=%s",
					s.cfg.Payment.SuccessURL,
					paymentIntent.CheckoutToken,
				),
			),

			CancelURL: stripe.String(
				fmt.Sprintf("%s?checkout_token=%s",
					s.cfg.Payment.CancelURL,
					paymentIntent.CheckoutToken,
				),
			),
		}

		params.AddMetadata("checkout_token", paymentIntent.CheckoutToken)
		params.AddMetadata("actor_id", req.ActorID.String())
		params.AddMetadata("actor_type", req.ActorType)

		session, err := session.New(params)
		if err != nil {
			return nil, fmt.Errorf("stripe session error: %w", err)
		}

		// update DB
		paymentIntent.SessionID = session.ID
		paymentIntent.GatewayMetadata = map[string]interface{}{
			"url": session.URL,
		}

		if err := s.db.Save(paymentIntent).Error; err != nil {
			return nil, err
		}

		return &GatewayInitResponse{
			SessionID: session.ID,
			URL:       session.URL,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported gateway")
	}
}
