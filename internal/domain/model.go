package domain

import (
	"fmt"
	"strings"
	"time"
)

const SchemaVersion = 1

type TaskID string
type PRID string
type StepID string
type TaskPrefix string

// Task is the canonical persisted task state. Status is intentionally absent:
// it is derived from cancellation and ordered PR state.
type Task struct {
	SchemaVersion int        `yaml:"schema_version"`
	ID            TaskID     `yaml:"id"`
	Title         string     `yaml:"title"`
	ProjectID     string     `yaml:"project_id"`
	CreatedAt     time.Time  `yaml:"created_at"`
	CancelledAt   *time.Time `yaml:"cancelled_at"`
	PRs           []PR       `yaml:"prs"`
}

// PR is one planned delivery unit. Its status is derived from start/skip
// metadata and Step state rather than persisted independently.
type PR struct {
	ID         PRID       `yaml:"id"`
	Title      string     `yaml:"title"`
	Branch     string     `yaml:"branch"`
	StartedAt  *time.Time `yaml:"started_at"`
	SkippedAt  *time.Time `yaml:"skipped_at"`
	SkipReason string     `yaml:"skip_reason"`
	Steps      []Step     `yaml:"steps"`
}

// Step is the only model whose lifecycle status is persisted directly.
type Step struct {
	ID         StepID     `yaml:"id"`
	Title      string     `yaml:"title"`
	Status     StepStatus `yaml:"status"`
	SkipReason string     `yaml:"skip_reason"`
}

func NewTask(id TaskID, title, projectID string, createdAt time.Time) (Task, error) {
	task := Task{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Title:         title,
		ProjectID:     projectID,
		CreatedAt:     createdAt,
		PRs:           []PR{},
	}
	if err := task.Validate(); err != nil {
		return Task{}, err
	}
	return task, nil
}

// Clone returns a deep copy suitable for prepare-validate-commit workflows.
func (t Task) Clone() Task {
	clone := t
	clone.CancelledAt = cloneTime(t.CancelledAt)
	clone.PRs = make([]PR, len(t.PRs))
	for prIndex := range t.PRs {
		clone.PRs[prIndex] = t.PRs[prIndex]
		clone.PRs[prIndex].StartedAt = cloneTime(t.PRs[prIndex].StartedAt)
		clone.PRs[prIndex].SkippedAt = cloneTime(t.PRs[prIndex].SkippedAt)
		clone.PRs[prIndex].Steps = append([]Step(nil), t.PRs[prIndex].Steps...)
	}
	return clone
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

// Validate rejects corrupt or impossible persisted state.
func (t Task) Validate() error {
	if t.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "got %d, want %d", t.SchemaVersion, SchemaVersion)
	}
	if _, _, err := ParseTaskID(string(t.ID)); err != nil {
		return invalid("id", "%v", err)
	}
	if err := validateTitle("title", t.Title); err != nil {
		return err
	}
	if err := validateProjectID(t.ProjectID); err != nil {
		return invalid("project_id", "%v", err)
	}
	if t.CreatedAt.IsZero() {
		return invalid("created_at", "must not be zero")
	}
	if t.CancelledAt != nil && t.CancelledAt.Before(t.CreatedAt) {
		return invalid("cancelled_at", "must not be before created_at")
	}

	prIDs := make(map[PRID]struct{}, len(t.PRs))
	stepIDs := make(map[StepID]struct{})
	stepNumbers := make(map[uint64]StepID)
	branches := make(map[string]PRID)
	for prIndex := range t.PRs {
		path := fmt.Sprintf("prs[%d]", prIndex)
		pr := &t.PRs[prIndex]
		if err := pr.validate(path, t.CreatedAt); err != nil {
			return err
		}
		prNumber, _ := ParsePRID(string(pr.ID))
		if prNumber != uint64(prIndex+1) {
			expected, _ := FormatPRID(uint64(prIndex + 1))
			return invalid(path+".id", "got %s, want %s to preserve PR order", pr.ID, expected)
		}
		if _, exists := prIDs[pr.ID]; exists {
			return invalid(path+".id", "duplicate PR ID %s", pr.ID)
		}
		prIDs[pr.ID] = struct{}{}
		if pr.Branch != "" {
			if owner, exists := branches[pr.Branch]; exists {
				return invalid(path+".branch", "branch %q is also associated with %s", pr.Branch, owner)
			}
			branches[pr.Branch] = pr.ID
		}
		var previousStepNumber uint64
		for stepIndex := range pr.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", path, stepIndex)
			step := &pr.Steps[stepIndex]
			if err := step.validate(stepPath); err != nil {
				return err
			}
			if _, exists := stepIDs[step.ID]; exists {
				return invalid(stepPath+".id", "duplicate Step ID %s", step.ID)
			}
			stepIDs[step.ID] = struct{}{}
			stepNumber, _ := ParseStepID(string(step.ID))
			if stepNumber <= previousStepNumber {
				return invalid(stepPath+".id", "Step IDs must increase within a PR")
			}
			previousStepNumber = stepNumber
			stepNumbers[stepNumber] = step.ID
		}
		if _, err := pr.ActiveStep(); err != nil {
			return invalid(path+".steps", "%v", err)
		}
	}
	for expected := uint64(1); expected <= uint64(len(stepIDs)); expected++ {
		if _, exists := stepNumbers[expected]; !exists {
			id, _ := FormatStepID(expected)
			return invalid("prs.steps", "missing sequential Step ID %s", id)
		}
	}
	return nil
}

func (p PR) validate(path string, taskCreatedAt time.Time) error {
	if _, err := ParsePRID(string(p.ID)); err != nil {
		return invalid(path+".id", "%v", err)
	}
	if err := validateTitle(path+".title", p.Title); err != nil {
		return err
	}
	if strings.ContainsRune(p.Branch, '\x00') {
		return invalid(path+".branch", "contains a NUL byte")
	}
	if p.Branch != strings.TrimSpace(p.Branch) {
		return invalid(path+".branch", "must not have surrounding whitespace")
	}
	if p.StartedAt == nil && p.Branch != "" {
		return invalid(path+".branch", "must be empty before the PR starts")
	}
	if p.StartedAt != nil {
		if p.Branch == "" {
			return invalid(path+".branch", "is required after the PR starts")
		}
		if p.StartedAt.Before(taskCreatedAt) {
			return invalid(path+".started_at", "must not be before Task created_at")
		}
		if len(p.Steps) == 0 {
			return invalid(path+".steps", "a started PR must contain at least one Step")
		}
	}
	if p.SkippedAt == nil {
		if p.SkipReason != "" {
			return invalid(path+".skip_reason", "must be empty unless the PR is skipped")
		}
	} else {
		if strings.TrimSpace(p.SkipReason) == "" {
			return invalid(path+".skip_reason", "is required for a skipped PR")
		}
		if strings.ContainsRune(p.SkipReason, '\x00') {
			return invalid(path+".skip_reason", "contains a NUL byte")
		}
		if p.SkippedAt.Before(taskCreatedAt) {
			return invalid(path+".skipped_at", "must not be before Task created_at")
		}
		if p.StartedAt != nil && p.SkippedAt.Before(*p.StartedAt) {
			return invalid(path+".skipped_at", "must not be before started_at")
		}
	}
	if p.StartedAt == nil {
		for stepIndex := range p.Steps {
			step := p.Steps[stepIndex]
			if step.Status != StepPending {
				return invalid(fmt.Sprintf("%s.steps[%d].status", path, stepIndex), "must be pending before the PR starts")
			}
		}
	}
	return nil
}

func (s Step) validate(path string) error {
	if _, err := ParseStepID(string(s.ID)); err != nil {
		return invalid(path+".id", "%v", err)
	}
	if err := validateTitle(path+".title", s.Title); err != nil {
		return err
	}
	if !s.Status.Valid() {
		return invalid(path+".status", "%q is not recognized", s.Status)
	}
	if s.Status == StepSkipped {
		if strings.TrimSpace(s.SkipReason) == "" {
			return invalid(path+".skip_reason", "is required for a skipped Step")
		}
		if strings.ContainsRune(s.SkipReason, '\x00') {
			return invalid(path+".skip_reason", "contains a NUL byte")
		}
	} else if s.SkipReason != "" {
		return invalid(path+".skip_reason", "must be empty unless the Step is skipped")
	}
	return nil
}

func validateTitle(path, title string) error {
	if strings.TrimSpace(title) == "" {
		return invalid(path, "must not be empty")
	}
	if strings.ContainsRune(title, '\x00') {
		return invalid(path, "contains a NUL byte")
	}
	return nil
}

func validateProjectID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("must be a nonempty path-safe identifier")
	}
	for index, character := range id {
		if character == '/' || character == '\\' || character == '\x00' {
			return fmt.Errorf("contains an unsafe path character")
		}
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') &&
			character != '_' && character != '-' && character != '.' {
			return fmt.Errorf("character %d is not path-safe", index+1)
		}
	}
	return nil
}
