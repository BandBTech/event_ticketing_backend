package services

import (
	"log"
	"sync"
	"time"
)

// SSEClient represents a connected SSE client
type SSEClient struct {
	CheckoutToken string
	Channel       chan map[string]interface{}
}

// SSEService manages real-time SSE connections for payment updates
type SSEService struct {
	mu      sync.RWMutex
	clients map[string][]*SSEClient // checkoutToken -> []*SSEClient
}

// NewSSEService creates a new SSE service
func NewSSEService() *SSEService {
	return &SSEService{
		clients: make(map[string][]*SSEClient),
	}
}

// Subscribe adds a new client listening to a checkout token
// Returns a channel to receive payment updates
func (s *SSEService) Subscribe(checkoutToken string) *SSEClient {
	s.mu.Lock()
	defer s.mu.Unlock()

	client := &SSEClient{
		CheckoutToken: checkoutToken,
		Channel:       make(chan map[string]interface{}, 10), // Buffered to prevent blocking
	}

	s.clients[checkoutToken] = append(s.clients[checkoutToken], client)
	log.Printf("[SSE_SUBSCRIBE] Client subscribed to %s | Total clients: %d", checkoutToken, len(s.clients[checkoutToken]))

	return client
}

// Unsubscribe removes a client from listening
func (s *SSEService) Unsubscribe(checkoutToken string, client *SSEClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clients, exists := s.clients[checkoutToken]
	if !exists {
		return
	}

	// Remove this client from the list
	for i, c := range clients {
		if c == client {
			// Close the channel
			close(c.Channel)

			// Remove from slice
			s.clients[checkoutToken] = append(clients[:i], clients[i+1:]...)
			log.Printf("[SSE_UNSUBSCRIBE] Client unsubscribed from %s | Remaining clients: %d", checkoutToken, len(s.clients[checkoutToken]))

			// Clean up empty lists
			if len(s.clients[checkoutToken]) == 0 {
				delete(s.clients, checkoutToken)
				log.Printf("[SSE_CLEANUP] Removed empty client list for %s", checkoutToken)
			}
			break
		}
	}
}

// sanitizeUpdate removes sensitive/internal fields before broadcasting
// SECURITY: Never expose stripe secrets, card data, or internal IDs
func (s *SSEService) sanitizeUpdate(update map[string]interface{}) map[string]interface{} {
	// Allowlist approach: only keep safe fields
	allowedFields := map[string]bool{
		"event":           true,
		"status":          true,
		"message":         true,
		"ticket_count":    true,
		"reason":          true,
		"tickets_created": true,
		"timestamp":       true,
	}

	sanitized := make(map[string]interface{})
	for key, value := range update {
		if allowedFields[key] {
			sanitized[key] = value
		}
	}

	return sanitized
}

// BroadcastPaymentUpdate sends a payment update to all clients listening to this checkout token
// Called by webhook after processing payment; sanitizes data before sending
func (s *SSEService) BroadcastPaymentUpdate(checkoutToken string, update map[string]interface{}) {
	// ============================================
	// SECURITY: Sanitize update before broadcasting
	// ============================================
	sanitized := s.sanitizeUpdate(update)

	s.mu.RLock()
	clients, exists := s.clients[checkoutToken]
	s.mu.RUnlock()

	if !exists || len(clients) == 0 {
		log.Printf("[SSE_BROADCAST] No clients listening to %s", checkoutToken)
		return
	}

	log.Printf("[SSE_BROADCAST] Broadcasting to %d clients for %s: %v", len(clients), checkoutToken, sanitized)

	for _, client := range clients {
		select {
		case client.Channel <- sanitized:
			log.Printf("[SSE_BROADCAST_OK] Update delivered to client")
		default:
			// Channel is full, log and skip (client might be slow or disconnected)
			log.Printf("[SSE_BROADCAST_SKIP] Client channel full for %s, skipping update (slow consumer)", checkoutToken)
		}
	}
}

// BroadcastTicketCreated sends ticket creation event with safe fields only
// SECURITY: Does NOT send raw ticket IDs (send count instead)
func (s *SSEService) BroadcastTicketCreated(checkoutToken string, ticketIDs []interface{}, ticketCount int) {
	log.Printf("[SSE_TICKET_CREATED] Broadcasting ticket creation: count=%d", ticketCount)

	update := map[string]interface{}{
		"event":           "payment_complete",
		"status":          "completed",
		"tickets_created": true,
		"ticket_count":    ticketCount,
		// SECURITY: Do NOT include ticket_ids - these are sensitive
		// Frontend will fetch tickets via authenticated endpoint using checkout_token
		"timestamp": time.Now().Unix(),
	}
	s.BroadcastPaymentUpdate(checkoutToken, update)
}

// BroadcastPaymentFailed sends payment failure event with generic reason only
// SECURITY: Does NOT send Stripe error details or payment specifics
func (s *SSEService) BroadcastPaymentFailed(checkoutToken string, reason string) {
	log.Printf("[SSE_PAYMENT_FAILED] Broadcasting payment failure: reason=%s", reason)

	// Map specific errors to generic user-friendly messages
	userFacingReason := reason
	switch reason {
	case "insufficient_funds":
		userFacingReason = "Insufficient funds"
	case "card_declined":
		userFacingReason = "Card declined"
	case "expired_card":
		userFacingReason = "Card expired"
	default:
		// For any other reason, use generic message
		if reason != "" && reason != "user_cancelled" {
			userFacingReason = "Payment failed"
		}
	}

	update := map[string]interface{}{
		"event":  "payment_failed",
		"status": "failed",
		"reason": userFacingReason,
		// SECURITY: Do NOT include:
		// - Stripe error codes
		// - Detailed decline reasons
		// - Any Stripe-specific information
		"timestamp": time.Now().Unix(),
	}
	s.BroadcastPaymentUpdate(checkoutToken, update)
}

// BroadcastPaymentProcessing sends processing event
func (s *SSEService) BroadcastPaymentProcessing(checkoutToken string) {
	log.Printf("[SSE_PAYMENT_PROCESSING] Broadcasting processing state")

	update := map[string]interface{}{
		"event":     "payment_processing",
		"status":    "processing",
		"message":   "Processing your payment...",
		"timestamp": time.Now().Unix(),
	}
	s.BroadcastPaymentUpdate(checkoutToken, update)
}

// GetClientCount returns the number of clients listening to a checkout token
func (s *SSEService) GetClientCount(checkoutToken string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count, _ := s.clients[checkoutToken]
	return len(count)
}
