package app

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
	planutil "github.com/hossainemruz/taskctl/internal/plan"
	"github.com/hossainemruz/taskctl/internal/vault"
)

type PlanApplyResult struct {
	Task      domain.Task
	PRCount   int
	StepCount int
}

type PRListItem struct {
	ID         domain.PRID     `json:"id"`
	Title      string          `json:"title"`
	Status     domain.PRStatus `json:"status"`
	Progress   domain.Progress `json:"progress"`
	Branch     string          `json:"branch,omitempty"`
	SkipReason string          `json:"skip_reason,omitempty"`
}

type StepListItem struct {
	ID         domain.StepID     `json:"id"`
	PRID       domain.PRID       `json:"pr_id"`
	Title      string            `json:"title"`
	Status     domain.StepStatus `json:"status"`
	SkipReason string            `json:"skip_reason,omitempty"`
}

func (w *Workflow) ApplyPlan(ctx context.Context, input ProjectInput, reader io.Reader) (PlanApplyResult, error) {
	structured, err := planutil.DecodeJSON(reader)
	if err != nil {
		return PlanApplyResult{}, WrapError(ErrorUsage, err, "invalid structured plan: %v", err)
	}
	store, projects, resolved, markdown, err := w.planContext(ctx, input)
	if err != nil {
		return PlanApplyResult{}, err
	}
	candidate := resolved.Task.Clone()
	if hasStartedPR(candidate) {
		err = candidate.CorrectPlanTitles(structured)
	} else {
		err = candidate.ReplaceDraftPlan(structured)
	}
	if err != nil {
		return PlanApplyResult{}, planningDomainError("apply plan", err)
	}
	if err := planutil.ValidateHeadings(markdown, structured); err != nil {
		return PlanApplyResult{}, WrapError(ErrorInvalidData, err, "plan.md does not match the structured plan: %v", err)
	}
	projected, err := planutil.ReplaceProgress(markdown, candidate)
	if err != nil {
		return PlanApplyResult{}, WrapError(ErrorInvalidData, err, "cannot refresh plan progress: %v", err)
	}
	if err := saveTaskAndProjection(store, projects, candidate, projected); err != nil {
		return PlanApplyResult{}, err
	}
	return PlanApplyResult{Task: candidate, PRCount: len(candidate.PRs), StepCount: countSteps(candidate)}, nil
}

func (w *Workflow) AddPR(ctx context.Context, input ProjectInput, title string) (domain.PRID, error) {
	store, projects, resolved, markdown, err := w.planContext(ctx, input)
	if err != nil {
		return "", err
	}
	candidate := resolved.Task.Clone()
	id, err := candidate.AddPR(title)
	if err != nil {
		return "", planningDomainError("add PR", err)
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return "", err
	}
	return id, nil
}

func (w *Workflow) AddStep(ctx context.Context, input ProjectInput, prValue, title string) (domain.StepID, error) {
	prID, err := parsePRID(prValue)
	if err != nil {
		return "", err
	}
	store, projects, resolved, markdown, err := w.planContext(ctx, input)
	if err != nil {
		return "", err
	}
	candidate := resolved.Task.Clone()
	id, err := candidate.AddStep(prID, title)
	if err != nil {
		return "", planningDomainError("add Step", err)
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return "", err
	}
	return id, nil
}

func (w *Workflow) SkipPR(ctx context.Context, input ProjectInput, prValue, reason string) (PRListItem, error) {
	prID, err := parsePRID(prValue)
	if err != nil {
		return PRListItem{}, err
	}
	store, projects, resolved, markdown, err := w.planContext(ctx, input)
	if err != nil {
		return PRListItem{}, err
	}
	candidate := resolved.Task.Clone()
	if err := candidate.SkipPR(prID, reason, w.currentTime()); err != nil {
		return PRListItem{}, planningDomainError("skip PR", err)
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return PRListItem{}, err
	}
	for _, item := range prListItems(candidate) {
		if item.ID == prID {
			return item, nil
		}
	}
	return PRListItem{}, NewError(ErrorInternal, "skipped PR %s disappeared from Task %s", prID, candidate.ID)
}

func (w *Workflow) SkipStep(ctx context.Context, input ProjectInput, stepValue, reason string) (StepListItem, error) {
	stepID, err := parseStepID(stepValue)
	if err != nil {
		return StepListItem{}, err
	}
	store, projects, resolved, markdown, err := w.planContext(ctx, input)
	if err != nil {
		return StepListItem{}, err
	}
	candidate := resolved.Task.Clone()
	if err := candidate.SkipStep(stepID, reason); err != nil {
		return StepListItem{}, planningDomainError("skip Step", err)
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return StepListItem{}, err
	}
	for _, item := range stepListItems(candidate) {
		if item.ID == stepID {
			return item, nil
		}
	}
	return StepListItem{}, NewError(ErrorInternal, "skipped Step %s disappeared from Task %s", stepID, candidate.ID)
}

func (w *Workflow) ListPRs(ctx context.Context, input ProjectInput) ([]PRListItem, error) {
	_, _, projects, err := w.runtime()
	if err != nil {
		return nil, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return nil, err
	}
	return prListItems(resolved.Task), nil
}

func (w *Workflow) ListSteps(ctx context.Context, input ProjectInput) ([]StepListItem, error) {
	_, _, projects, err := w.runtime()
	if err != nil {
		return nil, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return nil, err
	}
	return stepListItems(resolved.Task), nil
}

func (w *Workflow) planContext(ctx context.Context, input ProjectInput) (*vault.Store, *ProjectService, ResolvedContext, []byte, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return nil, nil, ResolvedContext{}, nil, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return nil, nil, ResolvedContext{}, nil, err
	}
	markdown, err := store.ReadArtifact(resolved.Project.ID, resolved.Task.ID, vault.ArtifactPlan)
	if err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			return nil, nil, ResolvedContext{}, nil, WrapError(ErrorNotFound, err,
				"plan.md does not exist for Task %s; run taskctl artifact ensure plan", resolved.Task.ID)
		}
		return nil, nil, ResolvedContext{}, nil, projects.vaultError("read plan.md", err)
	}
	return store, projects, resolved, markdown, nil
}

func persistPlanningMutation(store *vault.Store, projects *ProjectService, candidate domain.Task, markdown []byte) error {
	projected, err := planutil.ReplaceProgress(markdown, candidate)
	if err != nil {
		return WrapError(ErrorInvalidData, err, "cannot refresh plan progress: %v", err)
	}
	return saveTaskAndProjection(store, projects, candidate, projected)
}

func saveTaskAndProjection(store *vault.Store, projects *ProjectService, task domain.Task, projected []byte) error {
	if err := store.SaveTask(task); err != nil {
		return projects.vaultError("save Task "+string(task.ID), err)
	}
	if err := store.WriteArtifact(task.ProjectID, task.ID, vault.ArtifactPlan, projected); err != nil {
		return WrapError(ErrorPartialUpdate, err,
			"Task %s was updated, but refreshing plan.md failed; rerun a lifecycle command after fixing the artifact: %v", task.ID, err)
	}
	return nil
}

func planningDomainError(operation string, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return WrapError(ErrorNotFound, err, "%s: %v", operation, err)
	case errors.Is(err, domain.ErrInvalidTransition):
		return WrapError(ErrorConflict, err, "%s: %v", operation, err)
	case errors.Is(err, domain.ErrInvalidState), errors.Is(err, domain.ErrInvalidID), errors.Is(err, domain.ErrIDOverflow):
		return WrapError(ErrorUsage, err, "%s: %v", operation, err)
	default:
		return WrapError(ErrorInternal, err, "%s: %v", operation, err)
	}
}

func parsePRID(value string) (domain.PRID, error) {
	id := domain.PRID(strings.TrimSpace(value))
	if _, err := domain.ParsePRID(string(id)); err != nil {
		return "", WrapError(ErrorUsage, err, "invalid PR ID %q: %v", value, err)
	}
	return id, nil
}

func parseStepID(value string) (domain.StepID, error) {
	id := domain.StepID(strings.TrimSpace(value))
	if _, err := domain.ParseStepID(string(id)); err != nil {
		return "", WrapError(ErrorUsage, err, "invalid Step ID %q: %v", value, err)
	}
	return id, nil
}

func hasStartedPR(task domain.Task) bool {
	for _, pr := range task.PRs {
		if pr.StartedAt != nil {
			return true
		}
	}
	return false
}

func countSteps(task domain.Task) int {
	total := 0
	for _, pr := range task.PRs {
		total += len(pr.Steps)
	}
	return total
}

func prListItems(task domain.Task) []PRListItem {
	items := make([]PRListItem, len(task.PRs))
	for index, pr := range task.PRs {
		items[index] = PRListItem{ID: pr.ID, Title: pr.Title, Status: pr.Status(), Progress: pr.Progress(),
			Branch: pr.Branch, SkipReason: pr.SkipReason}
	}
	return items
}

func stepListItems(task domain.Task) []StepListItem {
	items := make([]StepListItem, 0, countSteps(task))
	for _, pr := range task.PRs {
		for _, step := range pr.Steps {
			items = append(items, StepListItem{ID: step.ID, PRID: pr.ID, Title: step.Title,
				Status: step.Status, SkipReason: step.SkipReason})
		}
	}
	return items
}
