package app

import (
	"errors"
	"fmt"
)

// ErrorKind identifies a stable class of command failure. The CLI maps these
// classes to exit codes; application code should not depend on those numbers.
type ErrorKind uint8

const (
	ErrorInternal ErrorKind = iota
	ErrorUsage
	ErrorMissingContext
	ErrorNotFound
	ErrorConflict
	ErrorInvalidData
	ErrorExternalCommand
	ErrorPartialUpdate
)

// Error is an actionable application failure with a stable category.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "taskctl failed"
}

func (e *Error) Unwrap() error {
	return e.Err
}

// NewError constructs a categorized error without an underlying cause.
func NewError(kind ErrorKind, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// WrapError constructs a categorized error while retaining its cause.
func WrapError(kind ErrorKind, err error, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), Err: err}
}

// ErrorKindOf returns the category of err and whether err is categorized.
func ErrorKindOf(err error) (ErrorKind, bool) {
	var applicationError *Error
	if !errors.As(err, &applicationError) {
		return ErrorInternal, false
	}
	return applicationError.Kind, true
}
