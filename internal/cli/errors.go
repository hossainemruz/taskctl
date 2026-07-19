package cli

import (
	"github.com/hossainemruz/taskctl/internal/app"
)

const (
	ExitSuccess         = 0
	ExitInternal        = 1
	ExitUsage           = 2
	ExitMissingContext  = 3
	ExitNotFound        = 4
	ExitConflict        = 5
	ExitInvalidData     = 6
	ExitExternalCommand = 7
	ExitPartialUpdate   = 8
)

// ExitCode maps stable application categories to process exit codes.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	kind, ok := app.ErrorKindOf(err)
	if !ok {
		return ExitInternal
	}
	switch kind {
	case app.ErrorUsage:
		return ExitUsage
	case app.ErrorMissingContext:
		return ExitMissingContext
	case app.ErrorNotFound:
		return ExitNotFound
	case app.ErrorConflict:
		return ExitConflict
	case app.ErrorInvalidData:
		return ExitInvalidData
	case app.ErrorExternalCommand:
		return ExitExternalCommand
	case app.ErrorPartialUpdate:
		return ExitPartialUpdate
	default:
		return ExitInternal
	}
}
