package cli

import (
	"errors"
	"testing"

	"github.com/hossainemruz/taskctl/internal/app"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", want: ExitSuccess},
		{name: "internal", err: app.NewError(app.ErrorInternal, "failed"), want: ExitInternal},
		{name: "usage", err: app.NewError(app.ErrorUsage, "failed"), want: ExitUsage},
		{name: "missing context", err: app.NewError(app.ErrorMissingContext, "failed"), want: ExitMissingContext},
		{name: "not found", err: app.NewError(app.ErrorNotFound, "failed"), want: ExitNotFound},
		{name: "conflict", err: app.NewError(app.ErrorConflict, "failed"), want: ExitConflict},
		{name: "invalid data", err: app.NewError(app.ErrorInvalidData, "failed"), want: ExitInvalidData},
		{name: "external command", err: app.NewError(app.ErrorExternalCommand, "failed"), want: ExitExternalCommand},
		{name: "partial update", err: app.NewError(app.ErrorPartialUpdate, "failed"), want: ExitPartialUpdate},
		{name: "uncategorized", err: errors.New("failed"), want: ExitInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitCode(test.err); got != test.want {
				t.Fatalf("ExitCode() = %d, want %d", got, test.want)
			}
		})
	}
}
