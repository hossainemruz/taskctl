package domain

import (
	"fmt"
	"strings"
	"time"
)

func (t *Task) ReplaceDraftPlan(plan Plan) error {
	models, err := plan.PRModels()
	if err != nil {
		return err
	}
	return t.mutate(func(candidate *Task) error {
		if candidate.Status() != TaskDraft {
			return transition("Task", string(candidate.ID), "replace plan", candidate.Status().String(), "only a draft Task permits hierarchy replacement")
		}
		candidate.PRs = models
		return nil
	})
}

func (t *Task) AddPR(title string) (PRID, error) {
	if err := validateTitle("title", title); err != nil {
		return "", err
	}
	var allocated PRID
	err := t.mutate(func(candidate *Task) error {
		if err := candidate.ensureNotCancelled("add PR"); err != nil {
			return err
		}
		id, err := candidate.NextPRID()
		if err != nil {
			return err
		}
		allocated = id
		candidate.PRs = append(candidate.PRs, PR{ID: id, Title: title, Steps: []Step{}})
		return nil
	})
	return allocated, err
}

func (t *Task) AddStep(prID PRID, title string) (StepID, error) {
	if err := validateTitle("title", title); err != nil {
		return "", err
	}
	var allocated StepID
	err := t.mutate(func(candidate *Task) error {
		if err := candidate.ensureNotCancelled("add Step"); err != nil {
			return err
		}
		pr, err := candidate.findPR(prID)
		if err != nil {
			return err
		}
		if pr.Status() == PRSkipped {
			return transition("PR", string(pr.ID), "add Step to", pr.Status().String(), "a skipped PR is out of scope")
		}
		id, err := candidate.NextStepID()
		if err != nil {
			return err
		}
		allocated = id
		pr.Steps = append(pr.Steps, Step{ID: id, Title: title, Status: StepPending})
		return nil
	})
	return allocated, err
}

func (t *Task) Cancel(at time.Time) error {
	return t.mutate(func(candidate *Task) error {
		status := candidate.Status()
		if status == TaskCancelled {
			return transition("Task", string(candidate.ID), "cancel", status.String(), "Task is already cancelled")
		}
		if status == TaskCompleted {
			return transition("Task", string(candidate.ID), "cancel", status.String(), "a completed Task cannot be abandoned")
		}
		if at.IsZero() {
			return transition("Task", string(candidate.ID), "cancel", status.String(), "cancellation time must not be zero")
		}
		if at.Before(candidate.CreatedAt) {
			return transition("Task", string(candidate.ID), "cancel", status.String(), "cancellation time must not be before creation")
		}
		candidate.CancelledAt = cloneTime(&at)
		return nil
	})
}

func (t *Task) StartPR(id PRID, branch string, at time.Time) error {
	return t.mutate(func(candidate *Task) error {
		if err := candidate.ensureNotCancelled("start PR"); err != nil {
			return err
		}
		pr, err := candidate.findPR(id)
		if err != nil {
			return err
		}
		status := pr.Status()
		if status != PRPending {
			return transition("PR", string(id), "start", status.String(), "PR must be pending")
		}
		if len(pr.Steps) == 0 {
			return transition("PR", string(id), "start", status.String(), "PR must contain at least one Step")
		}
		if strings.TrimSpace(branch) == "" || branch != strings.TrimSpace(branch) || strings.ContainsRune(branch, '\x00') {
			return transition("PR", string(id), "start", status.String(), "a valid named branch is required")
		}
		if at.IsZero() || at.Before(candidate.CreatedAt) {
			return transition("PR", string(id), "start", status.String(), "start time must not be zero or before Task creation")
		}
		for prIndex := range candidate.PRs {
			other := &candidate.PRs[prIndex]
			if other.ID != id && other.Branch == branch {
				return transition("PR", string(id), "start", status.String(), "branch %q is already associated with %s", branch, other.ID)
			}
		}
		pr.Branch = branch
		pr.StartedAt = cloneTime(&at)
		return nil
	})
}

// SkipPR removes a PR from scope while preserving any branch and Step history.
func (t *Task) SkipPR(id PRID, reason string, at time.Time) error {
	return t.mutate(func(candidate *Task) error {
		if err := candidate.ensureNotCancelled("skip PR"); err != nil {
			return err
		}
		pr, err := candidate.findPR(id)
		if err != nil {
			return err
		}
		status := pr.Status()
		if status == PRSkipped {
			return transition("PR", string(id), "skip", status.String(), "PR is already skipped")
		}
		if strings.TrimSpace(reason) == "" || strings.ContainsRune(reason, '\x00') {
			return transition("PR", string(id), "skip", status.String(), "a reason is required")
		}
		if at.IsZero() || at.Before(candidate.CreatedAt) {
			return transition("PR", string(id), "skip", status.String(), "skip time must not be zero or before Task creation")
		}
		if pr.StartedAt != nil && at.Before(*pr.StartedAt) {
			return transition("PR", string(id), "skip", status.String(), "skip time must not be before PR start")
		}
		pr.SkippedAt = cloneTime(&at)
		pr.SkipReason = reason
		return nil
	})
}

func (t *Task) StartStep(id StepID) error {
	return t.mutateStep(id, "start", func(pr *PR, step *Step) error {
		if step.Status != StepPending {
			return transition("Step", string(id), "start", step.Status.String(), "Step must be pending")
		}
		active, err := pr.ActiveStep()
		if err != nil {
			return invalid("steps", "%v", err)
		}
		if active != nil {
			return transition("Step", string(id), "start", step.Status.String(), "%s is already active", active.ID)
		}
		step.Status = StepInProgress
		return nil
	})
}

func (t *Task) SubmitStep(id StepID) error {
	return t.mutateStep(id, "submit", func(_ *PR, step *Step) error {
		if step.Status != StepInProgress {
			return transition("Step", string(id), "submit", step.Status.String(), "Step must be in progress")
		}
		step.Status = StepReadyForReview
		return nil
	})
}

func (t *Task) ReviseStep(id StepID) error {
	return t.mutateStep(id, "revise", func(_ *PR, step *Step) error {
		if step.Status != StepReadyForReview {
			return transition("Step", string(id), "revise", step.Status.String(), "Step must be ready for review")
		}
		step.Status = StepInProgress
		return nil
	})
}

func (t *Task) CompleteStep(id StepID) error {
	return t.mutateStep(id, "complete", func(_ *PR, step *Step) error {
		if step.Status != StepReadyForReview {
			return transition("Step", string(id), "complete", step.Status.String(), "user acceptance requires ready for review")
		}
		step.Status = StepCompleted
		return nil
	})
}

func (t *Task) SkipStep(id StepID, reason string) error {
	return t.mutateStep(id, "skip", func(_ *PR, step *Step) error {
		if step.Status == StepSkipped {
			return transition("Step", string(id), "skip", step.Status.String(), "Step is already skipped")
		}
		if strings.TrimSpace(reason) == "" || strings.ContainsRune(reason, '\x00') {
			return transition("Step", string(id), "skip", step.Status.String(), "a reason is required")
		}
		step.Status = StepSkipped
		step.SkipReason = reason
		return nil
	})
}

func (t *Task) ReopenStep(id StepID) error {
	return t.mutateStep(id, "reopen", func(_ *PR, step *Step) error {
		if step.Status != StepCompleted && step.Status != StepSkipped {
			return transition("Step", string(id), "reopen", step.Status.String(), "only a completed or skipped Step can be reopened")
		}
		step.Status = StepPending
		step.SkipReason = ""
		return nil
	})
}

func (t *Task) mutateStep(id StepID, operation string, change func(*PR, *Step) error) error {
	return t.mutate(func(candidate *Task) error {
		if err := candidate.ensureNotCancelled(operation + " Step"); err != nil {
			return err
		}
		pr, step, err := candidate.findStep(id)
		if err != nil {
			return err
		}
		if pr.Status() == PRSkipped {
			return transition("Step", string(id), operation, step.Status.String(), "parent PR %s is skipped", pr.ID)
		}
		if pr.StartedAt == nil {
			return transition("Step", string(id), operation, step.Status.String(), "parent PR %s has not started", pr.ID)
		}
		return change(pr, step)
	})
}

func (t *Task) mutate(change func(*Task) error) error {
	if t == nil {
		return invalid("task", "must not be nil")
	}
	if err := t.Validate(); err != nil {
		return err
	}
	candidate := t.Clone()
	if err := change(&candidate); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("transition produced invalid state: %w", err)
	}
	*t = candidate
	return nil
}

func (t *Task) ensureNotCancelled(operation string) error {
	if t.CancelledAt != nil {
		return transition("Task", string(t.ID), operation, TaskCancelled.String(), "cancelled Tasks are immutable")
	}
	return nil
}

func (t *Task) findPR(id PRID) (*PR, error) {
	for prIndex := range t.PRs {
		if t.PRs[prIndex].ID == id {
			return &t.PRs[prIndex], nil
		}
	}
	return nil, &NotFoundError{Entity: "PR", ID: string(id)}
}

func (t *Task) findStep(id StepID) (*PR, *Step, error) {
	for prIndex := range t.PRs {
		pr := &t.PRs[prIndex]
		for stepIndex := range pr.Steps {
			if pr.Steps[stepIndex].ID == id {
				return pr, &pr.Steps[stepIndex], nil
			}
		}
	}
	return nil, nil, &NotFoundError{Entity: "Step", ID: string(id)}
}
