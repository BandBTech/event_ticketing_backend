package gateways

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/pkg/config"
)

// EsewaProvider implements PaymentProvider for eSewa
type EsewaProvider struct {
	merchantId string
	baseURL    string
	verifyURL  string
	secretKey  string
}

// EsewaPaymentRequest represents eSewa payment initiation request
type EsewaPaymentRequest struct {
	Amt   float64 `json:"amt"`   // Amount
	Psc   float64 `json:"psc"`   // Service charge
	Pdc   float64 `json:"pdc"`   // Delivery charge
	TxAmt float64 `json:"txAmt"` // Tax amount
	TAmt  float64 `json:"tAmt"`  // Total amount
	Pid   string  `json:"pid"`   // Product ID (unique identifier)
	Scd   string  `json:"scd"`   // Merchant code
	Su    string  `json:"su"`    // Success URL
	Fu    string  `json:"fu"`    // Failure URL
}

// EsewaVerificationRequest represents eSewa payment verification request
type EsewaVerificationRequest struct {
	Amt string `xml:"amt"`
	Rid string `xml:"rid"` // Reference ID
	Pid string `xml:"pid"` // Product ID
	Scd string `xml:"scd"` // Merchant code
}

// EsewaVerificationResponse represents eSewa payment verification response
type EsewaVerificationResponse struct {
	ResponseCode    string `xml:"response_code"`
	ResponseMessage string `xml:"response_message"`
	MerchantID      string `xml:"merchant_id"`
	ProductID       string `xml:"product_id"`
	Amount          string `xml:"amount"`
	ReferenceID     string `xml:"reference_id"`
	TransactionID   string `xml:"transaction_id"`
}

// EsewaRefundRequest represents eSewa refund request
type EsewaRefundRequest struct {
	ProductID    string `json:"product_id"`
	ReferenceID  string `json:"reference_id"`
	RefundAmount string `json:"refund_amount"`
	RefundReason string `json:"refund_reason,omitempty"`
}

// NewEsewaProvider creates a new eSewa provider instance
func NewEsewaProvider(cfg *config.Config) *EsewaProvider {
	return &EsewaProvider{
		merchantId: cfg.Payment.Gateways.Esewa.MerchantID,
		baseURL:    cfg.Payment.Gateways.Esewa.BaseURL,
		verifyURL:  cfg.Payment.Gateways.Esewa.VerifyURL,
		secretKey:  cfg.Payment.Gateways.Esewa.SecretKey,
	}
}

// GetName returns "ESEWA"
func (ep *EsewaProvider) GetName() string {
	return "ESEWA"
}

// GetSupportedCurrencies returns currencies supported by eSewa
func (ep *EsewaProvider) GetSupportedCurrencies() []string {
	return []string{"NPR"}
}

// GetSupportedCountries returns countries supported by eSewa
func (ep *EsewaProvider) GetSupportedCountries() []string {
	return []string{"NP"} // Nepal
}

// CreatePayment creates an eSewa payment
func (ep *EsewaProvider) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// eSewa expects form-encoded data
	data := url.Values{}
	data.Set("amt", fmt.Sprintf("%.2f", req.Amount))
	data.Set("pid", req.IdempotencyKey)
	data.Set("scd", ep.merchantId)
	data.Set("su", req.SuccessURL)
	data.Set("fu", req.CancelURL)

	// Calculate total amount (including any fees)
	totalAmount := req.Amount
	data.Set("tAmt", fmt.Sprintf("%.2f", totalAmount))

	// Set service charge and delivery charge to 0 by default
	data.Set("psc", "0")
	data.Set("pdc", "0")
	data.Set("txAmt", "0")

	encodedData := data.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.baseURL, strings.NewReader(encodedData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow redirects for eSewa payment flow
			return nil
		},
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// eSewa redirects to payment page, so we need to capture the redirect URL
	finalURL := resp.Request.URL.String()

	return &CreatePaymentResponse{
		Status:        "pending",
		Amount:        req.Amount,
		Currency:      req.Currency,
		RedirectURL:   finalURL,
		ProviderTxnID: req.IdempotencyKey,
		CreatedAt:     time.Now(),
		Metadata: map[string]interface{}{
			"pid": req.IdempotencyKey,
		},
	}, nil
}

// VerifyPayment verifies an eSewa payment
func (ep *EsewaProvider) VerifyPayment(ctx context.Context, payload interface{}) (*VerifyPaymentResponse, error) {
	// For eSewa, payload should contain verification data from the success callback
	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payload type, expected map[string]interface{}")
	}

	// Extract required fields from payload
	amt, ok := payloadMap["amt"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid amt field")
	}

	rid, ok := payloadMap["refId"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid refId field")
	}

	pid, ok := payloadMap["oid"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid oid field")
	}

	// Create verification request
	verifyReq := EsewaVerificationRequest{
		Amt: amt,
		Rid: rid,
		Pid: pid,
		Scd: ep.merchantId,
	}

	// Convert to XML
	xmlData, err := xml.Marshal(verifyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal verification request: %w", err)
	}

	// Add XML declaration
	xmlData = []byte(xml.Header + string(xmlData))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.verifyURL, strings.NewReader(string(xmlData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create verification request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "text/xml")

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

	var verifyResp EsewaVerificationResponse
	if err := xml.Unmarshal(body, &verifyResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verification response: %w", err)
	}

	// Check response code
	if verifyResp.ResponseCode != "Success" {
		return &VerifyPaymentResponse{
			ProviderTxnID: verifyResp.TransactionID,
			Status:        "failed",
			Amount:        0,
			Currency:      "NPR",
			ProcessedAt:   time.Now(),
			Metadata: map[string]interface{}{
				"response_code":    verifyResp.ResponseCode,
				"response_message": verifyResp.ResponseMessage,
			},
		}, nil
	}

	// Parse amount
	amount, err := strconv.ParseFloat(verifyResp.Amount, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount in verification response: %s", verifyResp.Amount)
	}

	return &VerifyPaymentResponse{
		ProviderTxnID: verifyResp.TransactionID,
		Status:        "success",
		Amount:        amount,
		Currency:      "NPR",
		ProcessedAt:   time.Now(),
		Metadata: map[string]interface{}{
			"reference_id": verifyResp.ReferenceID,
			"product_id":   verifyResp.ProductID,
		},
	}, nil
}

// RefundPayment refunds an eSewa payment
func (ep *EsewaProvider) RefundPayment(ctx context.Context, req *RefundRequest) error {
	// eSewa refund API - this is a simplified implementation
	// In practice, eSewa may require contacting their support or using a different API

	if req.ChargeID == "" {
		return fmt.Errorf("esewa reference ID is required for refunds")
	}

	// For eSewa, refunds are typically handled manually through their portal
	// This is a placeholder implementation

	refundReq := EsewaRefundRequest{
		ProductID:    req.ChargeID,
		ReferenceID:  req.ChargeID, // Assuming charge ID is reference ID
		RefundAmount: fmt.Sprintf("%.2f", req.Amount),
		RefundReason: req.Reason,
	}

	jsonData, err := json.Marshal(refundReq)
	if err != nil {
		return fmt.Errorf("failed to marshal refund request: %w", err)
	}

	// Note: eSewa may not have a public refund API
	// This would need to be implemented based on their documentation
	httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.baseURL+"/refund", strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create refund request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Add any required authentication headers

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send refund request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("esewa refund API not implemented or failed")
	}

	return nil
}

// VerifyWebhook verifies eSewa webhook signatures and extracts event data
func (ep *EsewaProvider) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	// eSewa webhook verification - for now, basic implementation
	// TODO: Implement proper eSewa webhook signature verification
	// eSewa may not provide webhook signatures, so this is a placeholder

	var eventData map[string]interface{}
	if err := json.Unmarshal(payload, &eventData); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	webhookEvent := &WebhookEvent{
		Gateway:   "ESEWA",
		EventID:   "esewa-" + time.Now().Format("20060102150405"),
		Type:      "payment_intent.succeeded", // Default type, should be extracted from payload
		Data:      eventData,
		CreatedAt: time.Now(),
	}

	return webhookEvent, nil
}
