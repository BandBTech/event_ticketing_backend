package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/config"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RefundWorker struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	db          *gorm.DB
	gwRegistry  *gateways.Registry
	refundSM    *state.StateMachine[models.RefundStatus]
	refundQueue *services.RefundQueueService
}

func NewRefundWorker(cfg *config.Config, db *gorm.DB, gwRegistry *gateways.Registry, refundQueue *services.RefundQueueService) *RefundWorker {
	dbInt := 0
	if cfg.Redis.DB != "" {
		if parsed, err := strconv.Atoi(cfg.Redis.DB); err == nil {
			dbInt = parsed
		}
	}
	redisOpts := asynq.RedisClientOpt{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           dbInt,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		PoolSize:     10,
	}

	server := asynq.NewServer(redisOpts, asynq.Config{
		Concurrency: 5,
		Queues: map[string]int{
			"queue:refund:normal": 1,
		},
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return time.Duration(n*n) * 30 * time.Second
		},
	})
	mux := asynq.NewServeMux()

	w := &RefundWorker{
		server:      server,
		mux:         mux,
		db:          db,
		gwRegistry:  gwRegistry,
		refundSM:    state.NewStateMachine(state.RefundTransitions),
		refundQueue: refundQueue,
	}
	mux.HandleFunc(services.RefundProcessTaskType, w.handleProcessRefund)
	return w
}

func (w *RefundWorker) Start() {
	go func() {
		_ = w.server.Run(w.mux)
	}()
}

func (w *RefundWorker) Stop() {
	w.server.Shutdown()
}

func (w *RefundWorker) handleProcessRefund(ctx context.Context, task *asynq.Task) error {
	var payload services.RefundProcessTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return w.processRefund(ctx, payload.RefundID)
}

func (w *RefundWorker) processRefund(ctx context.Context, refundID uuid.UUID) error {
	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var refund models.Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, refundID).Error; err != nil {
			return err
		}

		switch refund.Status {
		case models.RefundSucceeded, models.RefundRejected, models.RefundCancelled:
			return nil
		}

		var txn models.Transaction
		if err := tx.First(&txn, refund.TransactionID).Error; err != nil {
			return err
		}

		now := time.Now().UTC()
		oldStatus := refund.Status
		if refund.Status == models.RefundPending || refund.Status == models.RefundFailed {
			if err := w.refundSM.Transition(refund.Status, models.RefundProcessing); err != nil {
				return err
			}
			retryCount := refund.RetryCount
			if refund.Status == models.RefundFailed {
				retryCount++
			}
			if err := tx.Model(&refund).Updates(map[string]any{
				"status":      models.RefundProcessing,
				"retry_count": retryCount,
				"updated_at":  now,
			}).Error; err != nil {
				return err
			}
			refund.Status = models.RefundProcessing
		}

		if txn.PaymentGateway == models.PaymentGatewayKonbini && txn.Status != models.TransactionSucceeded {
			return w.markRefundFinal(tx, &refund, oldStatus, models.RefundCancelled, "unpaid konbini order cancelled", now)
		}

		gw, err := w.gwRegistry.Get(string(txn.PaymentGateway))
		if err != nil {
			return w.markRefundFailed(tx, &refund, oldStatus, fmt.Sprintf("unsupported payment provider: %v", err), now)
		}

		// If a provider refund id already exists, poll provider for status instead of creating a new refund
		if refund.ProviderRefundID != "" {
			resp, err := gw.GetRefund(ctx, refund.ProviderRefundID)
			if err != nil {
				// If provider doesn't support retrieval, mark as failed so admin can retry manually
				return w.markRefundFailed(tx, &refund, oldStatus, fmt.Sprintf("failed to fetch provider refund status: %v", err), now)
			}

			// Map provider status to internal status
			providerStatus := strings.ToLower(resp.Status)
			if providerStatus == "succeeded" || providerStatus == "succeeded" {
				// mark succeeded
				if err := tx.Model(&refund).Updates(map[string]any{
					"status":       models.RefundSucceeded,
					"processed_at": now,
					"updated_at":   now,
				}).Error; err != nil {
					return err
				}
				if err := tx.Create(&models.RefundStatusHistory{
					ID:            uuid.New(),
					RefundID:      refund.ID,
					OldStatus:     oldStatus,
					NewStatus:     models.RefundSucceeded,
					ChangedAt:     now,
					ChangedByType: "system",
					Remarks:       "refund succeeded via provider poll",
				}).Error; err != nil {
					return err
				}
				if err := w.applyTicketRefundEffects(tx, &refund, now); err != nil {
					return err
				}
				if err := tx.Model(&models.Transaction{}).Where("id = ?", txn.ID).Update("status", models.TransactionRefunded).Error; err != nil {
					return err
				}
				return nil
			}

			if providerStatus == "failed" || providerStatus == "canceled" {
				return w.markRefundFailed(tx, &refund, oldStatus, "provider reported failed/canceled", now)
			}

			// still pending/processing — leave as is and return nil so Asynq may retry
			return nil
		}

		// No provider refund id present — create refund on provider
		chargeID := txn.ProviderChargeID
		if chargeID == "" {
			chargeID = txn.PaymentIntentID.String()
		}
		resp, err := gw.CreateRefund(ctx, &gateways.RefundRequest{
			GatewayChargeID: chargeID,
			Amount:          refund.Amount,
			Currency:        refund.Currency,
			Reason:          refund.Reason,
			Metadata: map[string]string{
				"refund_id": refund.ID.String(),
			},
		})
		if err != nil {
			return w.markRefundFailed(tx, &refund, oldStatus, err.Error(), now)
		}

		update := map[string]any{
			"provider_refund_id": resp.GatewayRefundID,
			"payment_provider":   txn.PaymentGateway,
			"updated_at":         now,
			"error_message":      "",
		}

		nextStatus := models.RefundProcessing
		remark := "refund processing via gateway"
		if resp.Status == "succeeded" {
			nextStatus = models.RefundSucceeded
			update["processed_at"] = now
			remark = "refund succeeded via worker"
		}
		if resp.Status == "failed" || resp.Status == "canceled" {
			nextStatus = models.RefundFailed
			update["error_message"] = "gateway returned failed/canceled"
			remark = "refund failed via worker"
		}

		if err := tx.Model(&refund).Updates(mergeMaps(update, map[string]any{"status": nextStatus})).Error; err != nil {
			return err
		}

		if err := tx.Create(&models.RefundStatusHistory{
			ID:            uuid.New(),
			RefundID:      refund.ID,
			OldStatus:     oldStatus,
			NewStatus:     nextStatus,
			ChangedAt:     now,
			ChangedByType: "system",
			Remarks:       remark,
		}).Error; err != nil {
			return err
		}

		if nextStatus == models.RefundSucceeded {
			if err := w.applyTicketRefundEffects(tx, &refund, now); err != nil {
				return err
			}
			if err := tx.Model(&models.Transaction{}).Where("id = ?", txn.ID).Update("status", models.TransactionRefunded).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (w *RefundWorker) markRefundFailed(tx *gorm.DB, refund *models.Refund, oldStatus models.RefundStatus, message string, now time.Time) error {
	if err := tx.Model(refund).Updates(map[string]any{
		"status":        models.RefundFailed,
		"error_message": message,
		"updated_at":    now,
	}).Error; err != nil {
		return err
	}
	return tx.Create(&models.RefundStatusHistory{
		ID:            uuid.New(),
		RefundID:      refund.ID,
		OldStatus:     oldStatus,
		NewStatus:     models.RefundFailed,
		ChangedAt:     now,
		ChangedByType: "system",
		Remarks:       message,
	}).Error
}

func (w *RefundWorker) markRefundFinal(tx *gorm.DB, refund *models.Refund, oldStatus, newStatus models.RefundStatus, remark string, now time.Time) error {
	if err := tx.Model(refund).Updates(map[string]any{
		"status":       newStatus,
		"processed_at": now,
		"updated_at":   now,
		"error_message": func() string {
			if newStatus == models.RefundCancelled {
				return remark
			}
			return ""
		}(),
	}).Error; err != nil {
		return err
	}
	return tx.Create(&models.RefundStatusHistory{
		ID:            uuid.New(),
		RefundID:      refund.ID,
		OldStatus:     oldStatus,
		NewStatus:     newStatus,
		ChangedAt:     now,
		ChangedByType: "system",
		Remarks:       remark,
	}).Error
}

func (w *RefundWorker) applyTicketRefundEffects(tx *gorm.DB, refund *models.Refund, now time.Time) error {
	if refund.TicketID != uuid.Nil {
		return tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(map[string]any{
			"status":        models.TicketRefunded,
			"refund_id":     refund.ID,
			"refunded_at":   now,
			"refund_amount": refund.Amount,
			"updated_at":    now,
		}).Error
	}

	return tx.Model(&models.Ticket{}).
		Where("transaction_id = ? AND status <> ?", refund.TransactionID, models.TicketRefunded).
		Updates(map[string]any{
			"status":      models.TicketRefunded,
			"refund_id":   refund.ID,
			"refunded_at": now,
			"updated_at":  now,
		}).Error
}

func mergeMaps(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
