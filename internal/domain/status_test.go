package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestStatusEnums(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value string
		parse func(string) (string, error)
	}{
		{name: "Task", value: "in_progress", parse: func(value string) (string, error) { status, err := ParseTaskStatus(value); return status.String(), err }},
		{name: "PR", value: "completed", parse: func(value string) (string, error) { status, err := ParsePRStatus(value); return status.String(), err }},
		{name: "Step", value: "ready_for_review", parse: func(value string) (string, error) { status, err := ParseStepStatus(value); return status.String(), err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.parse(test.value)
			if err != nil || got != test.value {
				t.Fatalf("parse(%q) = %q, %v", test.value, got, err)
			}
			if _, err := test.parse("unknown"); !errors.Is(err, ErrInvalidStatus) {
				t.Fatalf("parse(unknown) error = %v, want ErrInvalidStatus", err)
			}
		})
	}
}

func TestPRStatusAndProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		pr           PR
		wantStatus   PRStatus
		wantProgress Progress
	}{
		{name: "empty pending", pr: PR{}, wantStatus: PRPending},
		{name: "empty started is not vacuously complete", pr: PR{StartedAt: &testStarted}, wantStatus: PRInProgress},
		{name: "active", pr: startedTask(StepCompleted, StepInProgress).PRs[0], wantStatus: PRInProgress, wantProgress: Progress{Completed: 1, Total: 2}},
		{name: "all completed", pr: startedTask(StepCompleted, StepCompleted).PRs[0], wantStatus: PRCompleted, wantProgress: Progress{Completed: 2, Total: 2}},
		{name: "completed and skipped", pr: startedTask(StepCompleted, StepSkipped).PRs[0], wantStatus: PRCompleted, wantProgress: Progress{Completed: 1, Skipped: 1, Total: 2}},
		{name: "explicitly skipped", pr: PR{SkippedAt: &testStarted}, wantStatus: PRSkipped},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.pr.Status(); got != test.wantStatus {
				t.Fatalf("Status() = %s, want %s", got, test.wantStatus)
			}
			if got := test.pr.Progress(); !reflect.DeepEqual(got, test.wantProgress) {
				t.Fatalf("Progress() = %#v, want %#v", got, test.wantProgress)
			}
		})
	}
}

func TestTaskStatusAndProgress(t *testing.T) {
	t.Parallel()
	pending := pendingTask().PRs[0]
	completed := startedTask(StepCompleted).PRs[0]
	skipped := pending
	skipped.SkippedAt = cloneTime(&testStarted)
	skipped.SkipReason = "out of scope"
	newPending := PR{ID: "PR-002", Title: "New", Steps: []Step{{ID: "STEP-002", Title: "New", Status: StepPending}}}

	tests := []struct {
		name         string
		prs          []PR
		cancelled    bool
		wantStatus   TaskStatus
		wantProgress Progress
	}{
		{name: "empty draft is not vacuously complete", wantStatus: TaskDraft},
		{name: "applied plan remains draft", prs: []PR{pending}, wantStatus: TaskDraft, wantProgress: Progress{Total: 1}},
		{name: "started with pending PR", prs: []PR{completed, newPending}, wantStatus: TaskInProgress, wantProgress: Progress{Completed: 1, Total: 2}},
		{name: "all completed", prs: []PR{completed}, wantStatus: TaskCompleted, wantProgress: Progress{Completed: 1, Total: 1}},
		{name: "all skipped", prs: []PR{skipped}, wantStatus: TaskCompleted, wantProgress: Progress{Skipped: 1, Total: 1}},
		{name: "skipped and pending without execution", prs: []PR{skipped, newPending}, wantStatus: TaskDraft, wantProgress: Progress{Skipped: 1, Total: 2}},
		{name: "cancelled overrides aggregate", prs: []PR{completed}, cancelled: true, wantStatus: TaskCancelled, wantProgress: Progress{Completed: 1, Total: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := pendingTask()
			task.PRs = test.prs
			if test.cancelled {
				task.CancelledAt = cloneTime(&testLater)
			}
			if got := task.Status(); got != test.wantStatus {
				t.Fatalf("Status() = %s, want %s", got, test.wantStatus)
			}
			if got := task.Progress(); !reflect.DeepEqual(got, test.wantProgress) {
				t.Fatalf("Progress() = %#v, want %#v", got, test.wantProgress)
			}
		})
	}
}

func TestActiveStep(t *testing.T) {
	t.Parallel()
	pr := startedTask(StepCompleted, StepPending).PRs[0]
	active, err := pr.ActiveStep()
	if err != nil || active != nil {
		t.Fatalf("ActiveStep() = %#v, %v, want nil", active, err)
	}
	pr.Steps[1].Status = StepReadyForReview
	active, err = pr.ActiveStep()
	if err != nil || active == nil || active.ID != "STEP-002" {
		t.Fatalf("ActiveStep() = %#v, %v", active, err)
	}
	pr.Steps[0].Status = StepInProgress
	if _, err := pr.ActiveStep(); err == nil {
		t.Fatal("ActiveStep() error = nil, want multiple-active error")
	}
}

func TestPRStatusPropertiesAcrossStepCombinations(t *testing.T) {
	t.Parallel()
	statuses := []StepStatus{StepPending, StepInProgress, StepReadyForReview, StepCompleted, StepSkipped}
	for _, first := range statuses {
		for _, second := range statuses {
			for _, third := range statuses {
				steps := []Step{{Status: first}, {Status: second}, {Status: third}}
				pr := PR{StartedAt: &testStarted, Steps: steps}
				wantStatus := PRInProgress
				if terminalStep(first) && terminalStep(second) && terminalStep(third) {
					wantStatus = PRCompleted
				}
				if got := pr.Status(); got != wantStatus {
					t.Fatalf("Status(%s, %s, %s) = %s, want %s", first, second, third, got, wantStatus)
				}
				wantProgress := Progress{Total: 3}
				for _, status := range []StepStatus{first, second, third} {
					switch status {
					case StepCompleted:
						wantProgress.Completed++
					case StepSkipped:
						wantProgress.Skipped++
					}
				}
				if got := pr.Progress(); !reflect.DeepEqual(got, wantProgress) {
					t.Fatalf("Progress(%s, %s, %s) = %#v, want %#v", first, second, third, got, wantProgress)
				}
			}
		}
	}
}

func TestTaskStatusPropertiesAcrossPRCombinations(t *testing.T) {
	t.Parallel()
	statuses := []PRStatus{PRPending, PRInProgress, PRCompleted, PRSkipped}
	for _, first := range statuses {
		for _, second := range statuses {
			for _, third := range statuses {
				prs := []PR{prWithStatus(first), prWithStatus(second), prWithStatus(third)}
				task := Task{PRs: prs}
				wantStatus := TaskDraft
				if terminalPR(first) && terminalPR(second) && terminalPR(third) {
					wantStatus = TaskCompleted
				} else if startedPR(first) || startedPR(second) || startedPR(third) {
					wantStatus = TaskInProgress
				}
				if got := task.Status(); got != wantStatus {
					t.Fatalf("Status(%s, %s, %s) = %s, want %s", first, second, third, got, wantStatus)
				}
				wantProgress := Progress{Total: 3}
				for _, status := range []PRStatus{first, second, third} {
					switch status {
					case PRCompleted:
						wantProgress.Completed++
					case PRSkipped:
						wantProgress.Skipped++
					}
				}
				if got := task.Progress(); !reflect.DeepEqual(got, wantProgress) {
					t.Fatalf("Progress(%s, %s, %s) = %#v, want %#v", first, second, third, got, wantProgress)
				}
			}
		}
	}
}

func terminalStep(status StepStatus) bool {
	return status == StepCompleted || status == StepSkipped
}

func terminalPR(status PRStatus) bool {
	return status == PRCompleted || status == PRSkipped
}

func startedPR(status PRStatus) bool {
	return status == PRInProgress || status == PRCompleted
}

func prWithStatus(status PRStatus) PR {
	switch status {
	case PRPending:
		return PR{}
	case PRInProgress:
		return PR{StartedAt: &testStarted, Steps: []Step{{Status: StepPending}}}
	case PRCompleted:
		return PR{StartedAt: &testStarted, Steps: []Step{{Status: StepCompleted}}}
	case PRSkipped:
		return PR{SkippedAt: &testStarted}
	default:
		panic("unknown PR status")
	}
}
