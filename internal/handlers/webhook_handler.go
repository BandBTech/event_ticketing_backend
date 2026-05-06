package handlers

import (
	"io"
	"net/http"

	"event-ticketing-backend/internal/workers"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	worker *workers.PaymentWorker
}

func NewWebhookHandler(worker *workers.PaymentWorker) *WebhookHandler {
	return &WebhookHandler{
		worker: worker,
	}
}

func (h *WebhookHandler) StripeWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	signature := c.GetHeader("Stripe-Signature")

	err = h.worker.HandleStripeWebhook(
		c.Request.Context(),
		body,
		signature,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"received": true,
	})
}
