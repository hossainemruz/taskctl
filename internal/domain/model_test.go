package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var (
	testCreated = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	testStarted = testCreated.Add(time.Hour)
	testLater   = testStarted.Add(time.Hour)
)

func pendingTask() Task {
	return Task{
		SchemaVersion: SchemaVersion,
		ID:            "TASKCTL-001",
		Title:         "Implement taskctl",
		ProjectID:     "hossainemruz_taskctl",
		CreatedAt:     testCreated,
		PRs: []PR{
			{
				ID:    "PR-001",
				Title: "Implement storage",
				Steps: []Step{
					{ID: "STEP-001", Title: "Define schema", Status: StepPending},
					{ID: "STEP-002", Title: "Implement store", Status: StepPending},
				},
			},
		},
	}
}

func startedTask(statuses ...StepStatus) Task {
	task := pendingTask()
	task.PRs[0].Branch = "feat/storage"
	task.PRs[0].StartedAt = cloneTime(&testStarted)
	if len(statuses) != 0 {
		task.PRs[0].Steps = make([]Step, len(statuses))
		for index, status := range statuses {
			id, _ := FormatStepID(uint64(index + 1))
			task.PRs[0].Steps[index] = Step{ID: id, Title: "Step", Status: status}
			if status == StepSkipped {
				task.PRs[0].Steps[index].SkipReason = "not needed"
			}
		}
	}
	return task
}

func TestNewTask(t *testing.T) {
	t.Parallel()
	task, err := NewTask("TASKCTL-001", "Implement taskctl", "hossainemruz_taskctl", testCreated)
	if err != nil {
		t.Fatalf("NewTask() error = %v", err)
	}
	if task.SchemaVersion != SchemaVersion || task.Status() != TaskDraft || task.PRs == nil {
		t.Fatalf("NewTask() = %#v", task)
	}
}

func TestTaskValidateValidFixtures(t *testing.T) {
	t.Parallel()
	fixtures := []Task{
		pendingTask(),
		startedTask(StepInProgress, StepPending),
		startedTask(StepCompleted, StepSkipped),
	}
	interleaved := pendingTask()
	interleaved.PRs[0].Steps = append(interleaved.PRs[0].Steps, Step{ID: "STEP-003", Title: "Correction", Status: StepPending})
	interleaved.PRs = append(interleaved.PRs, PR{
		ID: "PR-002", Title: "Second",
		Steps: []Step{{ID: "STEP-002", Title: "Second Step", Status: StepPending}},
	})
	interleaved.PRs[0].Steps = []Step{
		{ID: "STEP-001", Title: "First", Status: StepPending},
		{ID: "STEP-003", Title: "Correction", Status: StepPending},
	}
	fixtures = append(fixtures, interleaved)
	skipped := pendingTask()
	skipped.PRs[0].SkippedAt = cloneTime(&testStarted)
	skipped.PRs[0].SkipReason = "out of scope"
	fixtures = append(fixtures, skipped)
	startedSkipped := startedTask(StepInProgress, StepPending)
	startedSkipped.PRs[0].SkippedAt = cloneTime(&testLater)
	startedSkipped.PRs[0].SkipReason = "superseded"
	fixtures = append(fixtures, startedSkipped)
	cancelled := pendingTask()
	cancelled.CancelledAt = cloneTime(&testStarted)
	fixtures = append(fixtures, cancelled)

	for index, fixture := range fixtures {
		if err := fixture.Validate(); err != nil {
			t.Fatalf("fixture %d Validate() error = %v", index, err)
		}
	}
}

func TestTaskValidateRejectsInvalidState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Task)
	}{
		{name: "schema", mutate: func(task *Task) { task.SchemaVersion = 2 }},
		{name: "Task ID", mutate: func(task *Task) { task.ID = "bad" }},
		{name: "Task title", mutate: func(task *Task) { task.Title = " \t" }},
		{name: "Task title NUL", mutate: func(task *Task) { task.Title = "bad\x00title" }},
		{name: "project ID", mutate: func(task *Task) { task.ProjectID = "../unsafe" }},
		{name: "created at", mutate: func(task *Task) { task.CreatedAt = time.Time{} }},
		{name: "cancellation before creation", mutate: func(task *Task) {
			value := task.CreatedAt.Add(-time.Second)
			task.CancelledAt = &value
		}},
		{name: "duplicate PR ID", mutate: func(task *Task) {
			task.PRs = append(task.PRs, PR{ID: "PR-001", Title: "Duplicate", Steps: []Step{}})
		}},
		{name: "PR ID gap", mutate: func(task *Task) {
			task.PRs = append(task.PRs, PR{ID: "PR-003", Title: "Third", Steps: []Step{}})
		}},
		{name: "duplicate Step ID across PRs", mutate: func(task *Task) {
			task.PRs = append(task.PRs, PR{ID: "PR-002", Title: "Second", Steps: []Step{{ID: "STEP-001", Title: "Duplicate", Status: StepPending}}})
		}},
		{name: "Step ID gap", mutate: func(task *Task) { task.PRs[0].Steps[1].ID = "STEP-003" }},
		{name: "Step order within PR", mutate: func(task *Task) {
			task.PRs[0].Steps[0].ID = "STEP-002"
			task.PRs[0].Steps[1].ID = "STEP-001"
		}},
		{name: "duplicate branch", mutate: func(task *Task) {
			*task = startedTask(StepCompleted)
			task.PRs = append(task.PRs, PR{
				ID: "PR-002", Title: "Second", Branch: "feat/storage", StartedAt: cloneTime(&testStarted),
				Steps: []Step{{ID: "STEP-002", Title: "Other", Status: StepPending}},
			})
		}},
		{name: "PR ID", mutate: func(task *Task) { task.PRs[0].ID = "PR-1" }},
		{name: "PR title", mutate: func(task *Task) { task.PRs[0].Title = "" }},
		{name: "branch before start", mutate: func(task *Task) { task.PRs[0].Branch = "feat/storage" }},
		{name: "started without branch", mutate: func(task *Task) { task.PRs[0].StartedAt = cloneTime(&testStarted) }},
		{name: "started without Steps", mutate: func(task *Task) {
			task.PRs[0].StartedAt = cloneTime(&testStarted)
			task.PRs[0].Branch = "feat/storage"
			task.PRs[0].Steps = nil
		}},
		{name: "started before Task", mutate: func(task *Task) {
			value := task.CreatedAt.Add(-time.Second)
			task.PRs[0].StartedAt = &value
			task.PRs[0].Branch = "feat/storage"
		}},
		{name: "skipped without reason", mutate: func(task *Task) { task.PRs[0].SkippedAt = cloneTime(&testStarted) }},
		{name: "skipped reason NUL", mutate: func(task *Task) {
			task.PRs[0].SkippedAt = cloneTime(&testStarted)
			task.PRs[0].SkipReason = "bad\x00reason"
		}},
		{name: "reason without skip", mutate: func(task *Task) { task.PRs[0].SkipReason = "reason" }},
		{name: "skipped before start time", mutate: func(task *Task) {
			*task = startedTask(StepPending)
			value := testStarted.Add(-time.Second)
			task.PRs[0].SkippedAt = &value
			task.PRs[0].SkipReason = "reason"
		}},
		{name: "nonpending Step before PR start", mutate: func(task *Task) { task.PRs[0].Steps[0].Status = StepInProgress }},
		{name: "Step ID", mutate: func(task *Task) { task.PRs[0].Steps[0].ID = "STEP-01" }},
		{name: "Step title", mutate: func(task *Task) { task.PRs[0].Steps[0].Title = "" }},
		{name: "Step status", mutate: func(task *Task) { task.PRs[0].Steps[0].Status = "unknown" }},
		{name: "skipped Step without reason", mutate: func(task *Task) {
			*task = startedTask(StepSkipped)
			task.PRs[0].Steps[0].SkipReason = ""
		}},
		{name: "skipped Step reason NUL", mutate: func(task *Task) {
			*task = startedTask(StepSkipped)
			task.PRs[0].Steps[0].SkipReason = "bad\x00reason"
		}},
		{name: "nonskipped Step with reason", mutate: func(task *Task) {
			*task = startedTask(StepPending)
			task.PRs[0].Steps[0].SkipReason = "reason"
		}},
		{name: "multiple active Steps", mutate: func(task *Task) {
			*task = startedTask(StepInProgress, StepReadyForReview)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			task := pendingTask()
			test.mutate(&task)
			if err := task.Validate(); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("Validate() error = %v, want ErrInvalidState", err)
			}
		})
	}
}

func TestPersistedModelsDoNotContainAggregateStatus(t *testing.T) {
	t.Parallel()
	for _, model := range []reflect.Type{reflect.TypeFor[Task](), reflect.TypeFor[PR]()} {
		if _, exists := model.FieldByName("Status"); exists {
			t.Fatalf("%s persists derived Status", model.Name())
		}
	}
}

func TestTaskCloneIsDeep(t *testing.T) {
	t.Parallel()
	task := startedTask(StepPending)
	task.CancelledAt = cloneTime(&testLater)
	clone := task.Clone()
	*clone.CancelledAt = clone.CancelledAt.Add(time.Hour)
	*clone.PRs[0].StartedAt = clone.PRs[0].StartedAt.Add(time.Hour)
	clone.PRs[0].Steps[0].Title = "Changed"
	if task.CancelledAt.Equal(*clone.CancelledAt) || task.PRs[0].StartedAt.Equal(*clone.PRs[0].StartedAt) || task.PRs[0].Steps[0].Title == "Changed" {
		t.Fatal("Clone() shares mutable storage with source")
	}
}
