package state

import (
	"fmt"
)

// HookFunc allows you to inject logic before/after transitions
type HookFunc[T comparable] func(from, to T) error

// StateMachine is a generic reusable state machine
type StateMachine[T comparable] struct {
	transitions map[T][]T
	beforeHook  HookFunc[T]
	afterHook   HookFunc[T]
}

// NewStateMachine creates a new state machine
func NewStateMachine[T comparable](transitions map[T][]T) *StateMachine[T] {
	return &StateMachine[T]{
		transitions: transitions,
	}
}

// WithHooks attaches optional hooks
func (sm *StateMachine[T]) WithHooks(before, after HookFunc[T]) *StateMachine[T] {
	sm.beforeHook = before
	sm.afterHook = after
	return sm
}

// Can checks if transition is allowed
func (sm *StateMachine[T]) Can(from, to T) bool {
	allowed := sm.transitions[from]
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

// Transition validates + runs hooks
func (sm *StateMachine[T]) Transition(from, to T) error {
	// idempotent safe
	if from == to {
		return nil
	}

	if !sm.Can(from, to) {
		return fmt.Errorf("invalid transition %v → %v", from, to)
	}

	// before hook
	if sm.beforeHook != nil {
		if err := sm.beforeHook(from, to); err != nil {
			return err
		}
	}

	// after hook
	if sm.afterHook != nil {
		if err := sm.afterHook(from, to); err != nil {
			return err
		}
	}

	return nil
}
