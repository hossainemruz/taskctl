// Package plan validates structured planning input and safely projects
// canonical Task state into the bounded generated block in plan.md.
package plan

import "errors"

var (
	ErrInvalidInput    = errors.New("invalid plan input")
	ErrInvalidHeadings = errors.New("invalid plan headings")
	ErrInvalidMarkers  = errors.New("invalid progress markers")
)
