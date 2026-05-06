package gateways

import (
	"fmt"
	"strings"

	"event-ticketing-backend/pkg/config"
)

// Registry holds all configured gateways.
//
// To add a new gateway (e.g. eSewa for Nepal):
//  1. Create internal/gateways/esewa.go implementing Gateway.
//  2. Add one case in the switch below.
//  3. Add the API key to config / .env.
//     Done. No other file changes needed.
type Registry struct {
	gateways map[string]Gateway
}

// NewRegistry reads config and registers all configured gateways.
func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{gateways: make(map[string]Gateway)}

	if key := cfg.Payment.Gateways.StripeAPIKey; key != "" {
		g := NewStripe(key, cfg.Payment.Gateways.StripeWebhookSecret)
		r.gateways[g.Name()] = g

		// Konbini runs on the same Stripe account but is a separate gateway
		// because it has different UX, currency rules, and refund policy.
		k := NewKonbini(key, cfg.Payment.Gateways.StripeWebhookSecret, 3)
		r.gateways[k.Name()] = k
	}

	// Add more gateways here as you expand to new countries:
	// if key := cfg.Payment.Gateways.EsewaAPIKey; key != "" {
	//     g := NewEsewa(key, cfg.Payment.Gateways.EsewaSecret)
	//     r.gateways[g.Name()] = g
	// }

	return r
}

// Get returns the gateway for the given name (case-insensitive).
// Returns a descriptive error if the gateway is not configured.
func (r *Registry) Get(name string) (Gateway, error) {
	gw, ok := r.gateways[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("gateway %q is not configured; supported: %v", name, r.Supported())
	}
	return gw, nil
}

// Supported returns the names of all registered gateways.
func (r *Registry) Supported() []string {
	names := make([]string, 0, len(r.gateways))
	for k := range r.gateways {
		names = append(names, k)
	}
	return names
}
