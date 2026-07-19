package plan

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hossainemruz/taskctl/internal/domain"
)

func progressTestTask() domain.Task {
	return domain.Task{PRs: []domain.PR{
		{ID: "PR-001", Title: "Storagé", StartedAt: pointerTime(), Steps: []domain.Step{
			{ID: "STEP-001", Title: "Schema", Status: domain.StepCompleted},
			{ID: "STEP-002", Title: "Persistence", Status: domain.StepReadyForReview},
		}},
		{ID: "PR-002", Title: "Old work", SkippedAt: pointerTime(), SkipReason: "superseded", Steps: []domain.Step{
			{ID: "STEP-003", Title: "Commands", Status: domain.StepSkipped, SkipReason: "not needed"},
		}},
	}}
}

func pointerTime() *time.Time {
	value := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	return &value
}

func TestRenderAndReplaceProgressPreservesOutsideBytes(t *testing.T) {
	t.Parallel()
	input := []byte("before\r\n" + ProgressStartMarker + "\r\n\r\nold\r\n\r\n" + ProgressEndMarker + "\r\nafter\r\n")
	got, err := ReplaceProgress(input, progressTestTask())
	if err != nil {
		t.Fatal(err)
	}
	want := "before\r\n" + ProgressStartMarker + "\r\n\r\n" +
		"- PR-001: Storagé — In Progress\r\n" +
		"  - STEP-001: Schema — Completed\r\n" +
		"  - STEP-002: Persistence — Ready for Review\r\n" +
		"- PR-002: Old work — Skipped\r\n" +
		"  - STEP-003: Commands — Skipped\r\n\r\n" + ProgressEndMarker + "\r\nafter\r\n"
	if string(got) != want {
		t.Fatalf("ReplaceProgress() = %q\nwant %q", got, want)
	}
	if strings.Contains(string(got), "superseded") {
		t.Fatalf("generated block included non-status prose: %q", got)
	}
}

func TestReplaceProgressRejectsMalformedMarkers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		text string
	}{
		{name: "missing start", text: ProgressEndMarker},
		{name: "missing end", text: ProgressStartMarker + "\n"},
		{name: "duplicate", text: ProgressStartMarker + "\n" + ProgressStartMarker + "\n" + ProgressEndMarker},
		{name: "reversed", text: ProgressEndMarker + "\n" + ProgressStartMarker + "\n"},
		{name: "indented", text: " " + ProgressStartMarker + "\n" + ProgressEndMarker},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ReplaceProgress([]byte(test.text), domain.Task{}); !errors.Is(err, ErrInvalidMarkers) {
				t.Fatalf("ReplaceProgress() error = %v, want ErrInvalidMarkers", err)
			}
		})
	}
}
