package domain

import (
	"errors"
	"math"
	"testing"
)

func TestTaskPrefixAndID(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"TASKCTL", "T2"} {
		if _, err := ParseTaskPrefix(prefix); err != nil {
			t.Fatalf("ParseTaskPrefix(%q) error = %v", prefix, err)
		}
	}
	for _, prefix := range []string{"", "taskctl", "2TASK", "TASK-CTL", "TASK_CTL"} {
		if _, err := ParseTaskPrefix(prefix); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParseTaskPrefix(%q) error = %v, want ErrInvalidID", prefix, err)
		}
	}

	prefix, number, err := ParseTaskID("TASKCTL-001")
	if err != nil || prefix != "TASKCTL" || number != 1 {
		t.Fatalf("ParseTaskID() = %s, %d, %v", prefix, number, err)
	}
	id, err := FormatTaskID("TASKCTL", 1000)
	if err != nil || id != "TASKCTL-1000" {
		t.Fatalf("FormatTaskID() = %s, %v", id, err)
	}
	for _, value := range []string{"TASKCTL-01", "TASKCTL-000", "TASKCTL-0001", "taskctl-001", "TASKCTL-x01"} {
		if _, _, err := ParseTaskID(value); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("ParseTaskID(%q) error = %v, want ErrInvalidID", value, err)
		}
	}
}

func TestScopedIDs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		parse func(string) (uint64, error)
	}{
		{value: "PR-001", parse: ParsePRID},
		{value: "STEP-1000", parse: ParseStepID},
	} {
		number, err := test.parse(test.value)
		if err != nil || (number != 1 && number != 1000) {
			t.Fatalf("parse(%q) = %d, %v", test.value, number, err)
		}
		for _, invalid := range []string{"001", "PR-01", "PR-000", "PR-0001", "PR-x01"} {
			if _, err := test.parse(invalid); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("parse(%q) error = %v, want ErrInvalidID", invalid, err)
			}
		}
	}
}

func TestNextTaskID(t *testing.T) {
	t.Parallel()
	first, err := NextTaskID("TASKCTL", nil)
	if err != nil || first != "TASKCTL-001" {
		t.Fatalf("first NextTaskID() = %s, %v", first, err)
	}
	id, err := NextTaskID("TASKCTL", []TaskID{"TASKCTL-001", "TASKCTL-003"})
	if err != nil || id != "TASKCTL-004" {
		t.Fatalf("NextTaskID() = %s, %v", id, err)
	}
	if _, err := NextTaskID("TASKCTL", []TaskID{"TASKCTL-001", "TASKCTL-001"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := NextTaskID("TASKCTL", []TaskID{"OTHER-001"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("prefix error = %v", err)
	}
	maximum, _ := FormatTaskID("TASKCTL", math.MaxUint64)
	if _, err := NextTaskID("TASKCTL", []TaskID{maximum}); !errors.Is(err, ErrIDOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestNextPRAndStepIDs(t *testing.T) {
	t.Parallel()
	task := pendingTask()
	task.PRs = append(task.PRs, PR{
		ID: "PR-003", Title: "Third",
		Steps: []Step{{ID: "STEP-004", Title: "Fourth", Status: StepPending}},
	})
	prID, err := task.NextPRID()
	if err != nil || prID != "PR-004" {
		t.Fatalf("NextPRID() = %s, %v", prID, err)
	}
	stepID, err := task.NextStepID()
	if err != nil || stepID != "STEP-005" {
		t.Fatalf("NextStepID() = %s, %v", stepID, err)
	}
	task.PRs[1].Steps[0].ID = "STEP-001"
	if _, err := task.NextStepID(); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("duplicate Step error = %v", err)
	}

	maximumPR, _ := FormatPRID(math.MaxUint64)
	task.PRs = []PR{{ID: maximumPR}}
	if _, err := task.NextPRID(); !errors.Is(err, ErrIDOverflow) {
		t.Fatalf("PR overflow error = %v", err)
	}
	maximumStep, _ := FormatStepID(math.MaxUint64)
	task.PRs = []PR{{ID: "PR-001", Steps: []Step{{ID: maximumStep}}}}
	if _, err := task.NextStepID(); !errors.Is(err, ErrIDOverflow) {
		t.Fatalf("Step overflow error = %v", err)
	}
}
