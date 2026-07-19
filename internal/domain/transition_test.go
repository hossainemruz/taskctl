package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func taskWithStepStatus(status StepStatus) Task {
	task := startedTask(status)
	return task
}

func TestReplaceDraftPlan(t *testing.T) {
	t.Parallel()
	task, err := NewTask("TASKCTL-001", "Task", "project", testCreated)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.ReplaceDraftPlan(validPlan()); err != nil {
		t.Fatalf("ReplaceDraftPlan() error = %v", err)
	}
	if len(task.PRs) != 2 || task.Status() != TaskDraft {
		t.Fatalf("Task after plan = %#v", task)
	}

	replacement := validPlan()
	replacement.PRs[0].Title = "Revised storage"
	if err := task.ReplaceDraftPlan(replacement); err != nil {
		t.Fatalf("draft replacement error = %v", err)
	}
	if task.PRs[0].Title != "Revised storage" {
		t.Fatalf("replacement title = %q", task.PRs[0].Title)
	}
	if err := task.StartPR("PR-001", "feat/storage", testStarted); err != nil {
		t.Fatal(err)
	}
	before := task.Clone()
	if err := task.ReplaceDraftPlan(validPlan()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("started replacement error = %v, want ErrInvalidTransition", err)
	}
	if !reflect.DeepEqual(task, before) {
		t.Fatal("failed replacement mutated Task")
	}
}

func TestCorrectPlanTitlesPreservesExecutionStateAndEvolvedTopology(t *testing.T) {
	t.Parallel()
	task, err := NewTask("TASKCTL-001", "Task", "project", testCreated)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.ReplaceDraftPlan(validPlan()); err != nil {
		t.Fatal(err)
	}
	if err := task.StartPR("PR-001", "feat/storage", testStarted); err != nil {
		t.Fatal(err)
	}
	correction, err := task.AddStep("PR-001", "Address final review")
	if err != nil || correction != "STEP-004" {
		t.Fatalf("AddStep() = %s, %v", correction, err)
	}
	plan := planForTask(task)
	plan.PRs[0].Title = "Revised storage"
	plan.PRs[0].Steps[0].Title = "Revised schema"
	if err := plan.Validate(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("initial-plan Validate() error = %v, want evolved traversal rejection", err)
	}
	startedAt := *task.PRs[0].StartedAt
	if err := task.CorrectPlanTitles(plan); err != nil {
		t.Fatalf("CorrectPlanTitles() error = %v", err)
	}
	if task.PRs[0].Title != "Revised storage" || task.PRs[0].Steps[0].Title != "Revised schema" {
		t.Fatalf("corrected titles = %#v", task.PRs[0])
	}
	if task.PRs[0].Branch != "feat/storage" || !task.PRs[0].StartedAt.Equal(startedAt) || task.PRs[0].Steps[0].Status != StepPending {
		t.Fatalf("execution state changed = %#v", task.PRs[0])
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("corrected Task is invalid: %v", err)
	}
}

func TestCorrectPlanTitlesRejectsTopologyChangesWithoutMutation(t *testing.T) {
	t.Parallel()
	base := startedTask(StepPending, StepPending)
	valid := planForTask(base)
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{name: "delete Step", mutate: func(plan *Plan) { plan.PRs[0].Steps = plan.PRs[0].Steps[:1] }},
		{name: "reorder Steps", mutate: func(plan *Plan) {
			plan.PRs[0].Steps[0], plan.PRs[0].Steps[1] = plan.PRs[0].Steps[1], plan.PRs[0].Steps[0]
		}},
		{name: "replace PR", mutate: func(plan *Plan) {
			plan.PRs = append(plan.PRs, PlannedPR{ID: "PR-002", Title: "Other", Steps: []PlannedStep{{ID: "STEP-003", Title: "Other"}}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			task := base.Clone()
			before := task.Clone()
			plan := valid
			plan.PRs = append([]PlannedPR(nil), valid.PRs...)
			for index := range plan.PRs {
				plan.PRs[index].Steps = append([]PlannedStep(nil), valid.PRs[index].Steps...)
			}
			test.mutate(&plan)
			if err := task.CorrectPlanTitles(plan); err == nil {
				t.Fatal("CorrectPlanTitles() accepted topology change")
			}
			if !reflect.DeepEqual(task, before) {
				t.Fatal("failed correction mutated Task")
			}
		})
	}
}

func planForTask(task Task) Plan {
	result := Plan{PRs: make([]PlannedPR, len(task.PRs))}
	for prIndex, pr := range task.PRs {
		result.PRs[prIndex] = PlannedPR{ID: pr.ID, Title: pr.Title, Steps: make([]PlannedStep, len(pr.Steps))}
		for stepIndex, step := range pr.Steps {
			result.PRs[prIndex].Steps[stepIndex] = PlannedStep{ID: step.ID, Title: step.Title}
		}
	}
	return result
}

func TestAddPRAndStepAllocationAndReopening(t *testing.T) {
	t.Parallel()
	task := startedTask(StepCompleted)
	if task.Status() != TaskCompleted {
		t.Fatalf("initial status = %s", task.Status())
	}
	stepID, err := task.AddStep("PR-001", "Address final review")
	if err != nil || stepID != "STEP-002" {
		t.Fatalf("AddStep() = %s, %v", stepID, err)
	}
	if task.Status() != TaskInProgress || task.PRs[0].Status() != PRInProgress {
		t.Fatalf("statuses after correction = Task %s, PR %s", task.Status(), task.PRs[0].Status())
	}

	prID, err := task.AddPR("Documentation")
	if err != nil || prID != "PR-002" {
		t.Fatalf("AddPR() = %s, %v", prID, err)
	}
	stepID, err = task.AddStep(prID, "Write docs")
	if err != nil || stepID != "STEP-003" {
		t.Fatalf("AddStep(new PR) = %s, %v", stepID, err)
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Task Validate() error = %v", err)
	}
}

func TestCorrectiveStepKeepsTaskWideSequenceWhenAddedToEarlierPR(t *testing.T) {
	t.Parallel()
	models, err := validPlan().PRModels()
	if err != nil {
		t.Fatal(err)
	}
	task := pendingTask()
	task.PRs = models
	task.PRs[0].Branch = "feat/storage"
	task.PRs[0].StartedAt = cloneTime(&testStarted)
	for index := range task.PRs[0].Steps {
		task.PRs[0].Steps[index].Status = StepCompleted
	}
	correction, err := task.AddStep("PR-001", "Address final review")
	if err != nil || correction != "STEP-004" {
		t.Fatalf("AddStep() = %s, %v", correction, err)
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Task Validate() error = %v", err)
	}
	if got := task.PRs[0].Steps[len(task.PRs[0].Steps)-1].ID; got != "STEP-004" {
		t.Fatalf("corrective Step ID = %s", got)
	}
}

func TestAddStepRejectsSkippedPR(t *testing.T) {
	t.Parallel()
	task := pendingTask()
	if err := task.SkipPR("PR-001", "not needed", testStarted); err != nil {
		t.Fatal(err)
	}
	before := task.Clone()
	if _, err := task.AddStep("PR-001", "Unexpected"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("AddStep() error = %v, want ErrInvalidTransition", err)
	}
	if !reflect.DeepEqual(task, before) {
		t.Fatal("failed AddStep mutated Task")
	}
}

func TestCancelTransition(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		task    Task
		at      time.Time
		wantErr bool
	}{
		{name: "draft", task: pendingTask(), at: testStarted},
		{name: "in progress", task: startedTask(StepPending), at: testLater},
		{name: "completed", task: startedTask(StepCompleted), at: testLater, wantErr: true},
		{name: "already cancelled", task: func() Task { task := pendingTask(); task.CancelledAt = cloneTime(&testStarted); return task }(), at: testLater, wantErr: true},
		{name: "zero time", task: pendingTask(), wantErr: true},
		{name: "before creation", task: pendingTask(), at: testCreated.Add(-time.Second), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before := test.task.Clone()
			err := test.task.Cancel(test.at)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("Cancel() error = %v, want ErrInvalidTransition", err)
				}
				if !reflect.DeepEqual(test.task, before) {
					t.Fatal("failed Cancel mutated Task")
				}
				return
			}
			if err != nil || test.task.Status() != TaskCancelled || test.task.CancelledAt == nil {
				t.Fatalf("Cancel() error = %v, Task = %#v", err, test.task)
			}
		})
	}
}

func TestStartPR(t *testing.T) {
	t.Parallel()
	task := pendingTask()
	if err := task.StartPR("PR-001", "feat/storage", testStarted); err != nil {
		t.Fatalf("StartPR() error = %v", err)
	}
	if task.PRs[0].Status() != PRInProgress || task.PRs[0].Branch != "feat/storage" || task.Status() != TaskInProgress {
		t.Fatalf("Task after StartPR = %#v", task)
	}

	tests := []struct {
		name   string
		mutate func(*Task)
		branch string
		at     time.Time
	}{
		{name: "empty PR", mutate: func(task *Task) { task.PRs[0].Steps = nil }, branch: "feat/storage", at: testStarted},
		{name: "blank branch", branch: " ", at: testStarted},
		{name: "zero time", branch: "feat/storage"},
		{name: "time before creation", branch: "feat/storage", at: testCreated.Add(-time.Second)},
		{name: "already started", mutate: func(task *Task) { *task = startedTask(StepPending) }, branch: "feat/other", at: testLater},
		{name: "skipped", mutate: func(task *Task) {
			task.PRs[0].SkippedAt = cloneTime(&testStarted)
			task.PRs[0].SkipReason = "reason"
		}, branch: "feat/storage", at: testLater},
		{name: "duplicate branch", mutate: func(task *Task) {
			task.PRs = append(task.PRs, PR{
				ID: "PR-002", Title: "Started", Branch: "feat/storage", StartedAt: cloneTime(&testStarted),
				Steps: []Step{{ID: "STEP-003", Title: "Other", Status: StepPending}},
			})
		}, branch: "feat/storage", at: testLater},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			task := pendingTask()
			if test.mutate != nil {
				test.mutate(&task)
			}
			before := task.Clone()
			if err := task.StartPR("PR-001", test.branch, test.at); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("StartPR() error = %v, want ErrInvalidTransition", err)
			}
			if !reflect.DeepEqual(task, before) {
				t.Fatal("failed StartPR mutated Task")
			}
		})
	}
}

func TestSkipPR(t *testing.T) {
	t.Parallel()
	task := pendingTask()
	if err := task.SkipPR("PR-001", "out of scope", testStarted); err != nil {
		t.Fatalf("SkipPR() error = %v", err)
	}
	if task.PRs[0].Status() != PRSkipped || task.PRs[0].SkipReason != "out of scope" || task.Status() != TaskCompleted {
		t.Fatalf("Task after SkipPR = %#v", task)
	}
	for _, source := range []Task{startedTask(StepPending), startedTask(StepCompleted)} {
		branch := source.PRs[0].Branch
		if err := source.SkipPR("PR-001", "superseded", testLater); err != nil {
			t.Fatalf("SkipPR(%s) error = %v", source.PRs[0].Status(), err)
		}
		if source.PRs[0].Status() != PRSkipped || source.PRs[0].Branch != branch || source.PRs[0].StartedAt == nil {
			t.Fatalf("started PR history was not preserved: %#v", source.PRs[0])
		}
	}

	tests := []struct {
		name   string
		task   Task
		reason string
		at     time.Time
	}{
		{name: "missing reason", task: pendingTask(), at: testStarted},
		{name: "zero time", task: pendingTask(), reason: "reason"},
		{name: "before start time", task: startedTask(StepPending), reason: "reason", at: testStarted.Add(-time.Second)},
		{name: "already skipped", task: task, reason: "reason", at: testLater},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before := test.task.Clone()
			if err := test.task.SkipPR("PR-001", test.reason, test.at); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("SkipPR() error = %v, want ErrInvalidTransition", err)
			}
			if !reflect.DeepEqual(test.task, before) {
				t.Fatal("failed SkipPR mutated Task")
			}
		})
	}
}

func TestStepTransitionMatrix(t *testing.T) {
	t.Parallel()
	statuses := []StepStatus{StepPending, StepInProgress, StepReadyForReview, StepCompleted, StepSkipped}
	tests := []struct {
		name      string
		operation func(*Task) error
		allowed   map[StepStatus]StepStatus
	}{
		{name: "start", operation: func(task *Task) error { return task.StartStep("STEP-001") }, allowed: map[StepStatus]StepStatus{StepPending: StepInProgress}},
		{name: "submit", operation: func(task *Task) error { return task.SubmitStep("STEP-001") }, allowed: map[StepStatus]StepStatus{StepInProgress: StepReadyForReview}},
		{name: "revise", operation: func(task *Task) error { return task.ReviseStep("STEP-001") }, allowed: map[StepStatus]StepStatus{StepReadyForReview: StepInProgress}},
		{name: "complete", operation: func(task *Task) error { return task.CompleteStep("STEP-001") }, allowed: map[StepStatus]StepStatus{StepReadyForReview: StepCompleted}},
		{name: "skip", operation: func(task *Task) error { return task.SkipStep("STEP-001", "not needed") }, allowed: map[StepStatus]StepStatus{
			StepPending: StepSkipped, StepInProgress: StepSkipped, StepReadyForReview: StepSkipped, StepCompleted: StepSkipped,
		}},
		{name: "reopen", operation: func(task *Task) error { return task.ReopenStep("STEP-001") }, allowed: map[StepStatus]StepStatus{
			StepCompleted: StepPending, StepSkipped: StepPending,
		}},
	}

	for _, test := range tests {
		for _, source := range statuses {
			t.Run(test.name+"/"+source.String(), func(t *testing.T) {
				t.Parallel()
				task := taskWithStepStatus(source)
				before := task.Clone()
				err := test.operation(&task)
				want, allowed := test.allowed[source]
				if !allowed {
					if !errors.Is(err, ErrInvalidTransition) {
						t.Fatalf("operation error = %v, want ErrInvalidTransition", err)
					}
					if !reflect.DeepEqual(task, before) {
						t.Fatal("failed transition mutated Task")
					}
					return
				}
				if err != nil {
					t.Fatalf("operation error = %v", err)
				}
				if task.PRs[0].Steps[0].Status != want {
					t.Fatalf("status = %s, want %s", task.PRs[0].Steps[0].Status, want)
				}
				if source == StepSkipped && task.PRs[0].Steps[0].SkipReason != "" {
					t.Fatalf("reopen retained skip reason %q", task.PRs[0].Steps[0].SkipReason)
				}
			})
		}
	}
}

func TestStartStepEnforcesOneActiveStep(t *testing.T) {
	t.Parallel()
	task := startedTask(StepInProgress, StepPending)
	before := task.Clone()
	if err := task.StartStep("STEP-002"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("StartStep() error = %v, want ErrInvalidTransition", err)
	}
	if !reflect.DeepEqual(task, before) {
		t.Fatal("failed StartStep mutated Task")
	}
}

func TestStepTransitionsRequireStartedPR(t *testing.T) {
	t.Parallel()
	operations := []func(*Task) error{
		func(task *Task) error { return task.StartStep("STEP-001") },
		func(task *Task) error { return task.SubmitStep("STEP-001") },
		func(task *Task) error { return task.ReviseStep("STEP-001") },
		func(task *Task) error { return task.CompleteStep("STEP-001") },
		func(task *Task) error { return task.SkipStep("STEP-001", "reason") },
		func(task *Task) error { return task.ReopenStep("STEP-001") },
	}
	for index, operation := range operations {
		task := pendingTask()
		if err := operation(&task); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("operation %d error = %v, want ErrInvalidTransition", index, err)
		}
	}
}

func TestStepTransitionsRejectSkippedParentPR(t *testing.T) {
	t.Parallel()
	task := startedTask(StepPending)
	if err := task.SkipPR("PR-001", "superseded", testLater); err != nil {
		t.Fatal(err)
	}
	before := task.Clone()
	if err := task.StartStep("STEP-001"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("StartStep() error = %v, want ErrInvalidTransition", err)
	}
	if !reflect.DeepEqual(task, before) {
		t.Fatal("failed Step transition mutated skipped PR")
	}
}

func TestCancelledTaskRejectsNormalMutations(t *testing.T) {
	t.Parallel()
	pending := pendingTask()
	pending.CancelledAt = cloneTime(&testStarted)
	started := startedTask(StepPending)
	started.CancelledAt = cloneTime(&testLater)
	operations := []struct {
		name string
		task Task
		run  func(*Task) error
	}{
		{name: "add PR", task: pending, run: func(task *Task) error { _, err := task.AddPR("New"); return err }},
		{name: "add Step", task: pending, run: func(task *Task) error { _, err := task.AddStep("PR-001", "New"); return err }},
		{name: "start PR", task: pending, run: func(task *Task) error { return task.StartPR("PR-001", "feat/storage", testLater) }},
		{name: "skip PR", task: pending, run: func(task *Task) error { return task.SkipPR("PR-001", "reason", testLater) }},
		{name: "start Step", task: started, run: func(task *Task) error { return task.StartStep("STEP-001") }},
		{name: "skip Step", task: started, run: func(task *Task) error { return task.SkipStep("STEP-001", "reason") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			before := operation.task.Clone()
			if err := operation.run(&operation.task); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("operation error = %v, want ErrInvalidTransition", err)
			}
			if !reflect.DeepEqual(operation.task, before) {
				t.Fatal("cancelled Task was mutated")
			}
		})
	}
}

func TestIncrementalReviewAndCorrectionWorkflow(t *testing.T) {
	t.Parallel()
	task := startedTask(StepPending)
	if err := task.StartStep("STEP-001"); err != nil {
		t.Fatal(err)
	}
	if err := task.SubmitStep("STEP-001"); err != nil {
		t.Fatal(err)
	}
	if err := task.ReviseStep("STEP-001"); err != nil {
		t.Fatal(err)
	}
	if err := task.SubmitStep("STEP-001"); err != nil {
		t.Fatal(err)
	}
	if err := task.CompleteStep("STEP-001"); err != nil {
		t.Fatal(err)
	}
	if task.PRs[0].Status() != PRCompleted || task.Status() != TaskCompleted {
		t.Fatalf("completed statuses = PR %s, Task %s", task.PRs[0].Status(), task.Status())
	}
	correction, err := task.AddStep("PR-001", "Address final review")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRs[0].Status() != PRInProgress || task.Status() != TaskInProgress {
		t.Fatalf("reopened statuses = PR %s, Task %s", task.PRs[0].Status(), task.Status())
	}
	if err := task.StartStep(correction); err != nil {
		t.Fatal(err)
	}
	if err := task.SubmitStep(correction); err != nil {
		t.Fatal(err)
	}
	if err := task.CompleteStep(correction); err != nil {
		t.Fatal(err)
	}
	if task.PRs[0].Status() != PRCompleted || task.Status() != TaskCompleted {
		t.Fatalf("recompleted statuses = PR %s, Task %s", task.PRs[0].Status(), task.Status())
	}
}

func TestNotFoundErrors(t *testing.T) {
	t.Parallel()
	task := pendingTask()
	if err := task.StartPR("PR-999", "feat/missing", testStarted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing PR error = %v", err)
	}
	started := startedTask(StepPending)
	if err := started.StartStep("STEP-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Step error = %v", err)
	}
}

func TestInvalidCurrentStatePreventsMutation(t *testing.T) {
	t.Parallel()
	task := startedTask(StepInProgress, StepReadyForReview)
	before := task.Clone()
	if err := task.CompleteStep("STEP-002"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CompleteStep() error = %v, want ErrInvalidState", err)
	}
	if !reflect.DeepEqual(task, before) {
		t.Fatal("invalid Task was mutated")
	}
}
