package domain

import "fmt"

type TaskStatus string

const (
	TaskDraft      TaskStatus = "draft"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskCancelled  TaskStatus = "cancelled"
)

type PRStatus string

const (
	PRPending    PRStatus = "pending"
	PRInProgress PRStatus = "in_progress"
	PRCompleted  PRStatus = "completed"
	PRSkipped    PRStatus = "skipped"
)

type StepStatus string

const (
	StepPending        StepStatus = "pending"
	StepInProgress     StepStatus = "in_progress"
	StepReadyForReview StepStatus = "ready_for_review"
	StepCompleted      StepStatus = "completed"
	StepSkipped        StepStatus = "skipped"
)

type Progress struct {
	Completed int `json:"completed"`
	Skipped   int `json:"skipped"`
	Total     int `json:"total"`
}

func (s TaskStatus) String() string { return string(s) }
func (s PRStatus) String() string   { return string(s) }
func (s StepStatus) String() string { return string(s) }

func (s TaskStatus) Valid() bool {
	switch s {
	case TaskDraft, TaskInProgress, TaskCompleted, TaskCancelled:
		return true
	default:
		return false
	}
}

func (s PRStatus) Valid() bool {
	switch s {
	case PRPending, PRInProgress, PRCompleted, PRSkipped:
		return true
	default:
		return false
	}
}

func (s StepStatus) Valid() bool {
	switch s {
	case StepPending, StepInProgress, StepReadyForReview, StepCompleted, StepSkipped:
		return true
	default:
		return false
	}
}

func ParseTaskStatus(value string) (TaskStatus, error) {
	status := TaskStatus(value)
	if !status.Valid() {
		return "", fmt.Errorf("%w: Task status %q", ErrInvalidStatus, value)
	}
	return status, nil
}

func ParsePRStatus(value string) (PRStatus, error) {
	status := PRStatus(value)
	if !status.Valid() {
		return "", fmt.Errorf("%w: PR status %q", ErrInvalidStatus, value)
	}
	return status, nil
}

func ParseStepStatus(value string) (StepStatus, error) {
	status := StepStatus(value)
	if !status.Valid() {
		return "", fmt.Errorf("%w: Step status %q", ErrInvalidStatus, value)
	}
	return status, nil
}

// Status derives Task state without storing an aggregate status field.
func (t Task) Status() TaskStatus {
	if t.CancelledAt != nil {
		return TaskCancelled
	}
	if len(t.PRs) > 0 {
		allTerminal := true
		for prIndex := range t.PRs {
			status := t.PRs[prIndex].Status()
			if status != PRCompleted && status != PRSkipped {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return TaskCompleted
		}
	}
	for prIndex := range t.PRs {
		if t.PRs[prIndex].StartedAt != nil {
			return TaskInProgress
		}
	}
	return TaskDraft
}

// Progress counts terminal PRs at Task scope.
func (t Task) Progress() Progress {
	progress := Progress{Total: len(t.PRs)}
	for prIndex := range t.PRs {
		switch t.PRs[prIndex].Status() {
		case PRCompleted:
			progress.Completed++
		case PRSkipped:
			progress.Skipped++
		}
	}
	return progress
}

// Status derives PR state with non-vacuous completion for its Step collection.
func (p PR) Status() PRStatus {
	if p.SkippedAt != nil {
		return PRSkipped
	}
	if p.StartedAt == nil {
		return PRPending
	}
	if len(p.Steps) > 0 {
		allTerminal := true
		for stepIndex := range p.Steps {
			status := p.Steps[stepIndex].Status
			if status != StepCompleted && status != StepSkipped {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return PRCompleted
		}
	}
	return PRInProgress
}

// Progress counts terminal Steps at PR scope.
func (p PR) Progress() Progress {
	progress := Progress{Total: len(p.Steps)}
	for stepIndex := range p.Steps {
		switch p.Steps[stepIndex].Status {
		case StepCompleted:
			progress.Completed++
		case StepSkipped:
			progress.Skipped++
		}
	}
	return progress
}

// ActiveStep returns the sole implementing/reviewing Step, if any, and rejects
// a corrupt PR containing multiple active Steps.
func (p *PR) ActiveStep() (*Step, error) {
	var active *Step
	for stepIndex := range p.Steps {
		step := &p.Steps[stepIndex]
		if step.Status != StepInProgress && step.Status != StepReadyForReview {
			continue
		}
		if active != nil {
			return nil, fmt.Errorf("multiple active Steps: %s and %s", active.ID, step.ID)
		}
		active = step
	}
	return active, nil
}
