package gateways

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/pkg/config"
)

// KhaltiProvider implements PaymentProvider for Khalti
type KhaltiProvider struct {
	apiKey       string
	secretKey    string
	baseURL      string
	verifySecret string
}

// KhaltiPaymentRequest represents Khalti payment initiation request
type KhaltiPaymentRequest struct {
	ReturnURL         string                  `json:"return_url"`
	WebsiteURL        string                  `json:"website_url"`
	Amount            int64                   `json:"amount"` // Amount in paisa (multiply by 100)
	PurchaseOrderID   string                  `json:"purchase_order_id"`
	PurchaseOrderName string                  `json:"purchase_order_name"`
	CustomerInfo      KhaltiCustomerInfo      `json:"customer_info"`
	AmountBreakdown   []KhaltiAmountBreakdown `json:"amount_breakdown,omitempty"`
	ProductDetails    []KhaltiProductDetail   `json:"product_details,omitempty"`
}

type KhaltiCustomerInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone,omitempty"`
}

type KhaltiAmountBreakdown struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type KhaltiProductDetail struct {
	Identity   string  `json:"identity"`
	Name       string  `json:"name"`
	TotalPrice float64 `json:"total_price"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
}

// KhaltiPaymentResponse represents Khalti payment initiation response
type KhaltiPaymentResponse struct {
	Pidx       string `json:"pidx"`
	PaymentURL string `json:"payment_url"`
	ExpiresAt  string `json:"expires_at"`
	ExpiresIn  int    `json:"expires_in"`
}

// KhaltiVerificationRequest represents Khalti payment verification request
type KhaltiVerificationRequest struct {
	Pidx string `json:"pidx"`
}

// KhaltiVerificationResponse represents Khalti payment verification response
type KhaltiVerificationResponse struct {
	Pidx          string  `json:"pidx"`
	TotalAmount   float64 `json:"total_amount"`
	Status        string  `json:"status"`
	TransactionID string  `json:"transaction_id"`
	Fee           float64 `json:"fee"`
	Refunded      bool    `json:"refunded"`
}

// KhaltiRefundRequest represents Khalti refund request
type KhaltiRefundRequest struct {
	Pidx   string  `json:"pidx"`
	Amount float64 `json:"amount,omitempty"` // Optional, full refund if not specified
}

// KhaltiRefundResponse represents Khalti refund response
type KhaltiRefundResponse struct {
	RefundedAmount float64 `json:"refunded_amount"`
	RefundedFee    float64 `json:"refunded_fee"`
	Status         string  `json:"status"`
}

// NewKhaltiProvider creates a new Khalti provider instance
func NewKhaltiProvider(cfg *config.Config) *KhaltiProvider {
	return &KhaltiProvider{
		apiKey:       cfg.Payment.Gateways.Khalti.APIKey,
		secretKey:    cfg.Payment.Gateways.Khalti.SecretKey,
		baseURL:      cfg.Payment.Gateways.Khalti.BaseURL,
		verifySecret: cfg.Payment.Gateways.Khalti.VerifySecret,
	}
}

// GetName returns "KHALTI"
func (kp *KhaltiProvider) GetName() string {
	return "KHALTI"
}

// GetSupportedCurrencies returns currencies supported by Khalti
func (kp *KhaltiProvider) GetSupportedCurrencies() []string {
	return []string{"NPR"}
}

// GetSupportedCountries returns countries supported by Khalti
func (kp *KhaltiProvider) GetSupportedCountries() []string {
	return []string{"NP"} // Nepal
}

// CreatePayment creates a Khalti payment
func (kp *KhaltiProvider) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// Convert amount to paisa (multiply by 100)
	amountPaisa := int64(req.Amount * 100)

	khaltiReq := KhaltiPaymentRequest{
		ReturnURL:         req.ReturnURL,
		WebsiteURL:        req.SuccessURL, // Use success URL as website URL
		Amount:            amountPaisa,
		PurchaseOrderID:   req.IdempotencyKey,
		PurchaseOrderName: req.Description,
		CustomerInfo: KhaltiCustomerInfo{
			Name:  req.CustomerName,
			Email: req.CustomerEmail,
		},
	}

	// Add amount breakdown if metadata contains fee info
	if req.Metadata != nil {
		if platformFee, ok := req.Metadata["platform_fee"]; ok {
			if fee, err := strconv.ParseFloat(platformFee, 64); err == nil {
				khaltiReq.AmountBreakdown = []KhaltiAmountBreakdown{
					{Label: "Platform Fee", Amount: fee},
					{Label: "Ticket Amount", Amount: req.Amount - fee},
				}
			}
		}
	}

	jsonData, err := json.Marshal(khaltiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", kp.baseURL+"/api/v2/epayment/initiate/", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Key "+kp.secretKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("khalti API error: %s", string(body))
	}

	var khaltiResp KhaltiPaymentResponse
	if err := json.Unmarshal(body, &khaltiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &CreatePaymentResponse{
		Status:        "pending",
		Amount:        req.Amount,
		Currency:      req.Currency,
		RedirectURL:   khaltiResp.PaymentURL,
		ProviderTxnID: khaltiResp.Pidx,
		CreatedAt:     time.Now(),
		Metadata: map[string]interface{}{
			"pidx":       khaltiResp.Pidx,
			"expires_at": khaltiResp.ExpiresAt,
		},
	}, nil
}

// VerifyPayment verifies a Khalti payment
func (kp *KhaltiProvider) VerifyPayment(ctx context.Context, payload interface{}) (*VerifyPaymentResponse, error) {
	// For Khalti, payload should be the pidx (payment ID)
	pidx, ok := payload.(string)
	if !ok {
		return nil, fmt.Errorf("invalid payload type, expected string pidx")
	}

	verifyReq := KhaltiVerificationRequest{Pidx: pidx}
	jsonData, err := json.Marshal(verifyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal verification request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", kp.baseURL+"/api/v2/epayment/lookup/", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create verification request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Key "+kp.secretKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send verification request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read verification response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("khalti verification API error: %s", string(body))
	}

	var verifyResp KhaltiVerificationResponse
	if err := json.Unmarshal(body, &verifyResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verification response: %w", err)
	}

	// Convert amount from paisa to rupees
	amount := verifyResp.TotalAmount / 100

	status := "pending"
	switch strings.ToLower(verifyResp.Status) {
	case "completed", "success":
		status = "success"
	case "failed", "cancelled":
		status = "failed"
	}

	return &VerifyPaymentResponse{
		ProviderTxnID: verifyResp.TransactionID,
		Status:        status,
		Amount:        amount,
		Currency:      "NPR",
		Fee:           &verifyResp.Fee,
		ProcessedAt:   time.Now(),
		Metadata: map[string]interface{}{
			"pidx":     verifyResp.Pidx,
			"refunded": verifyResp.Refunded,
		},
	}, nil
}

// RefundPayment refunds a Khalti payment
func (kp *KhaltiProvider) RefundPayment(ctx context.Context, req *RefundRequest) error {
	if req.ChargeID == "" {
		return fmt.Errorf("khalti charge ID (pidx) is required for refunds")
	}

	refundReq := KhaltiRefundRequest{
		Pidx: req.ChargeID,
	}

	// If amount is specified, set it
	if req.Amount > 0 {
		refundReq.Amount = req.Amount
	}

	jsonData, err := json.Marshal(refundReq)
	if err != nil {
		return fmt.Errorf("failed to marshal refund request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", kp.baseURL+"/api/v2/epayment/refund/", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create refund request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Key "+kp.secretKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send refund request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read refund response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("khalti refund API error: %s", string(body))
	}

	var refundResp KhaltiRefundResponse
	if err := json.Unmarshal(body, &refundResp); err != nil {
		return fmt.Errorf("failed to unmarshal refund response: %w", err)
	}

	// Check if refund was successful
	if strings.ToLower(refundResp.Status) != "completed" && strings.ToLower(refundResp.Status) != "success" {
		return fmt.Errorf("refund failed with status: %s", refundResp.Status)
	}

	return nil
}

// VerifyWebhook verifies Khalti webhook signatures and extracts event data
func (kp *KhaltiProvider) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	// Khalti webhook verification - for now, basic implementation
	// TODO: Implement proper Khalti webhook signature verification
	// Khalti may not provide webhook signatures, so this is a placeholder

	var eventData map[string]interface{}
	if err := json.Unmarshal(payload, &eventData); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	webhookEvent := &WebhookEvent{
		Gateway:   "KHALTI",
		EventID:   "khalti-" + time.Now().Format("20060102150405"),
		Type:      "payment_intent.succeeded", // Default type, should be extracted from payload
		Data:      eventData,
		CreatedAt: time.Now(),
	}

	return webhookEvent, nil
}
