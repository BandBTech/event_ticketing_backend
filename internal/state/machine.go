package state

import (
	"event-ticketing-backend/internal/models"
	"fmt"
)

type StateMachine struct{}

var transitions = map[models.PaymentStatus][]models.PaymentStatus{
	models.PaymentPending:    {models.PaymentProcessing, models.PaymentCanceled},
	models.PaymentProcessing: {models.PaymentSucceeded, models.PaymentFailed},
	models.PaymentSucceeded:  {models.PaymentCanceled},
}

func (sm *StateMachine) Can(from, to models.PaymentStatus) bool {
	allowed := transitions[from]
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

func (sm *StateMachine) Transition(from, to models.PaymentStatus) error {
	if from == to {
		return nil
	}

	if !sm.Can(from, to) {
		return fmt.Errorf("invalid transition %s → %s", from, to)
	}

	return nil
}
