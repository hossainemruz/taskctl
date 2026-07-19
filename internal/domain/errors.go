// Package domain contains the filesystem-independent task model and all of its
// lifecycle rules.
package domain

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidState      = errors.New("invalid domain state")
	ErrInvalidTransition = errors.New("invalid lifecycle transition")
	ErrNotFound          = errors.New("domain entity not found")
	ErrInvalidID         = errors.New("invalid identifier")
	ErrIDOverflow        = errors.New("identifier sequence exhausted")
	ErrInvalidStatus     = errors.New("invalid status")
)

// ValidationError identifies one invalid field or invariant in a model.
type ValidationError struct {
	Path    string
	Problem string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Problem
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Problem)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidState }

// TransitionError describes a rejected intention without exposing a generic
// status setter.
type TransitionError struct {
	Entity    string
	ID        string
	Operation string
	Status    string
	Problem   string
}

func (e *TransitionError) Error() string {
	subject := e.Entity
	if e.ID != "" {
		subject += " " + e.ID
	}
	message := fmt.Sprintf("cannot %s %s", e.Operation, subject)
	if e.Status != "" {
		message += " from " + e.Status
	}
	if e.Problem != "" {
		message += ": " + e.Problem
	}
	return message
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }

// NotFoundError identifies a missing Task child.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %s not found", e.Entity, e.ID)
}

func (e *NotFoundError) Unwrap() error { return ErrNotFound }

func invalid(path, format string, args ...any) error {
	return &ValidationError{Path: path, Problem: fmt.Sprintf(format, args...)}
}

func transition(entity, id, operation, status, format string, args ...any) error {
	return &TransitionError{
		Entity:    entity,
		ID:        id,
		Operation: operation,
		Status:    status,
		Problem:   fmt.Sprintf(format, args...),
	}
}
