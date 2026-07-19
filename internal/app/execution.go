package app

import (
	"context"
	"errors"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
	planutil "github.com/hossainemruz/taskctl/internal/plan"
	"github.com/hossainemruz/taskctl/internal/vault"
)

// ArtifactPaths is the stable, sparse artifact contract used by agent-facing
// commands. A field is omitted when its artifact does not exist.
type ArtifactPaths struct {
	Task     string `json:"task,omitempty"`
	Research string `json:"research,omitempty"`
	Plan     string `json:"plan,omitempty"`
	Review   string `json:"review,omitempty"`
}

type StepGetResult struct {
	TaskID    domain.TaskID     `json:"task_id"`
	PRID      domain.PRID       `json:"pr_id"`
	StepID    domain.StepID     `json:"step_id"`
	Status    domain.StepStatus `json:"status"`
	Artifacts ArtifactPaths     `json:"artifacts"`
}

type stepOperation string

const (
	stepStart    stepOperation = "start"
	stepSubmit   stepOperation = "submit"
	stepRevise   stepOperation = "revise"
	stepComplete stepOperation = "complete"
	stepSkip     stepOperation = "skip"
	stepReopen   stepOperation = "reopen"
)

// StartPR associates a pending PR with the user's current named branch. It
// inspects Git for facts only; branch creation and topology remain user-owned.
func (w *Workflow) StartPR(ctx context.Context, input ProjectInput, prValue string) (PRListItem, error) {
	prID, err := parsePRID(prValue)
	if err != nil {
		return PRListItem{}, err
	}
	_, store, projects, err := w.runtime()
	if err != nil {
		return PRListItem{}, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return PRListItem{}, err
	}
	if resolved.Branch == "" {
		return PRListItem{}, NewError(ErrorMissingContext, "a named current Git branch is required to start %s", prID)
	}
	if resolved.CurrentPR != nil {
		return PRListItem{}, NewError(ErrorConflict, "branch %q is already associated with %s in Task %s",
			resolved.Branch, resolved.CurrentPR.ID, resolved.Task.ID)
	}

	candidate := resolved.Task.Clone()
	if err := candidate.StartPR(prID, resolved.Branch, w.currentTime()); err != nil {
		return PRListItem{}, planningDomainError("start PR", err)
	}
	markdown, err := readPlanArtifact(store, projects, resolved)
	if err != nil {
		return PRListItem{}, err
	}
	if err := planutil.ValidateHeadings(markdown, taskPlan(resolved.Task)); err != nil {
		return PRListItem{}, WrapError(ErrorInvalidData, err, "plan.md does not match the registered Task plan: %v", err)
	}
	if err := ensureBranchAvailable(store, projects, resolved, prID); err != nil {
		return PRListItem{}, err
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return PRListItem{}, err
	}
	item, found := findPRListItem(candidate, prID)
	if !found {
		return PRListItem{}, NewError(ErrorInternal, "started PR %s disappeared from Task %s", prID, candidate.ID)
	}
	return item, nil
}

// GetStep selects the sole active Step in the branch-associated PR, or its
// first pending Step when no Step is active.
func (w *Workflow) GetStep(ctx context.Context, input ProjectInput) (StepGetResult, error) {
	store, projects, resolved, pr, err := w.currentExecutionPR(ctx, input)
	if err != nil {
		return StepGetResult{}, err
	}
	step, err := selectedStep(pr)
	if err != nil {
		return StepGetResult{}, err
	}
	artifacts, err := existingArtifactPaths(store, projects, resolved.Project.ID, resolved.Task.ID)
	if err != nil {
		return StepGetResult{}, err
	}
	return StepGetResult{
		TaskID: resolved.Task.ID, PRID: pr.ID, StepID: step.ID, Status: step.Status, Artifacts: artifacts,
	}, nil
}

func (w *Workflow) StartStep(ctx context.Context, input ProjectInput, stepValue string) (StepListItem, error) {
	return w.transitionStep(ctx, input, stepValue, stepStart, "")
}

func (w *Workflow) SubmitStep(ctx context.Context, input ProjectInput, stepValue string) (StepListItem, error) {
	return w.transitionStep(ctx, input, stepValue, stepSubmit, "")
}

func (w *Workflow) ReviseStep(ctx context.Context, input ProjectInput, stepValue string) (StepListItem, error) {
	return w.transitionStep(ctx, input, stepValue, stepRevise, "")
}

func (w *Workflow) CompleteStep(ctx context.Context, input ProjectInput, stepValue string) (StepListItem, error) {
	return w.transitionStep(ctx, input, stepValue, stepComplete, "")
}

func (w *Workflow) ReopenStep(ctx context.Context, input ProjectInput, stepValue string) (StepListItem, error) {
	return w.transitionStep(ctx, input, stepValue, stepReopen, "")
}

func (w *Workflow) transitionStep(ctx context.Context, input ProjectInput, stepValue string, operation stepOperation, reason string) (StepListItem, error) {
	store, projects, resolved, pr, err := w.currentExecutionPR(ctx, input)
	if err != nil {
		return StepListItem{}, err
	}
	stepID, err := resolveStepID(resolved.Task, pr, stepValue, operation)
	if err != nil {
		return StepListItem{}, err
	}

	candidate := resolved.Task.Clone()
	switch operation {
	case stepStart:
		err = candidate.StartStep(stepID)
	case stepSubmit:
		err = candidate.SubmitStep(stepID)
	case stepRevise:
		err = candidate.ReviseStep(stepID)
	case stepComplete:
		err = candidate.CompleteStep(stepID)
	case stepSkip:
		err = candidate.SkipStep(stepID, reason)
	case stepReopen:
		err = candidate.ReopenStep(stepID)
	default:
		return StepListItem{}, NewError(ErrorInternal, "unknown Step operation %q", operation)
	}
	if err != nil {
		return StepListItem{}, planningDomainError(string(operation)+" Step", err)
	}
	markdown, err := readPlanArtifact(store, projects, resolved)
	if err != nil {
		return StepListItem{}, err
	}
	if err := persistPlanningMutation(store, projects, candidate, markdown); err != nil {
		return StepListItem{}, err
	}
	item, found := findStepListItem(candidate, stepID)
	if !found {
		return StepListItem{}, NewError(ErrorInternal, "Step %s disappeared after %s in Task %s", stepID, operation, candidate.ID)
	}
	return item, nil
}

func (w *Workflow) currentExecutionPR(ctx context.Context, input ProjectInput) (*vault.Store, *ProjectService, ResolvedContext, *domain.PR, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return nil, nil, ResolvedContext{}, nil, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return nil, nil, ResolvedContext{}, nil, err
	}
	if resolved.CurrentPR == nil {
		if resolved.Branch == "" {
			return nil, nil, ResolvedContext{}, nil, NewError(ErrorMissingContext,
				"a named current Git branch associated by taskctl pr start is required")
		}
		return nil, nil, ResolvedContext{}, nil, NewError(ErrorMissingContext,
			"current branch %q is not associated with a PR; run taskctl pr start", resolved.Branch)
	}
	for index := range resolved.Task.PRs {
		if resolved.Task.PRs[index].ID == resolved.CurrentPR.ID {
			return store, projects, resolved, &resolved.Task.PRs[index], nil
		}
	}
	return nil, nil, ResolvedContext{}, nil, NewError(ErrorInvalidData,
		"current PR %s is not present in Task %s", resolved.CurrentPR.ID, resolved.Task.ID)
}

func selectedStep(pr *domain.PR) (*domain.Step, error) {
	if pr.Status() == domain.PRSkipped {
		return nil, NewError(ErrorNotFound, "PR %s is skipped and has no available Step", pr.ID)
	}
	active, err := pr.ActiveStep()
	if err != nil {
		return nil, WrapError(ErrorInvalidData, err, "PR %s has invalid active Step state: %v", pr.ID, err)
	}
	if active != nil {
		return active, nil
	}
	for index := range pr.Steps {
		if pr.Steps[index].Status == domain.StepPending {
			return &pr.Steps[index], nil
		}
	}
	return nil, NewError(ErrorNotFound, "PR %s has no active or pending Step", pr.ID)
}

func resolveStepID(task domain.Task, currentPR *domain.PR, value string, operation stepOperation) (domain.StepID, error) {
	if strings.TrimSpace(value) == "" {
		selected, err := selectedStep(currentPR)
		if err == nil {
			return selected.ID, nil
		}
		if operation != stepReopen || !hasErrorKindValue(err, ErrorNotFound) {
			return "", err
		}
		var reopenable *domain.Step
		for index := range currentPR.Steps {
			step := &currentPR.Steps[index]
			if step.Status != domain.StepCompleted && step.Status != domain.StepSkipped {
				continue
			}
			if reopenable != nil {
				return "", NewError(ErrorConflict, "multiple Steps in %s can be reopened; specify a Step ID", currentPR.ID)
			}
			reopenable = step
		}
		if reopenable != nil {
			return reopenable.ID, nil
		}
		return "", err
	}

	id, err := parseStepID(value)
	if err != nil {
		return "", err
	}
	for index := range currentPR.Steps {
		if currentPR.Steps[index].ID == id {
			return id, nil
		}
	}
	for _, pr := range task.PRs {
		for _, step := range pr.Steps {
			if step.ID == id {
				return "", NewError(ErrorConflict,
					"Step %s belongs to %s, but current branch %q is associated with %s", id, pr.ID, currentPR.Branch, currentPR.ID)
			}
		}
	}
	return "", NewError(ErrorNotFound, "Step %s not found in Task %s", id, task.ID)
}

func readPlanArtifact(store *vault.Store, projects *ProjectService, resolved ResolvedContext) ([]byte, error) {
	markdown, err := store.ReadArtifact(resolved.Project.ID, resolved.Task.ID, vault.ArtifactPlan)
	if errors.Is(err, vault.ErrNotFound) {
		return nil, WrapError(ErrorNotFound, err,
			"plan.md does not exist for Task %s; run taskctl artifact ensure plan", resolved.Task.ID)
	}
	if err != nil {
		return nil, projects.vaultError("read plan.md", err)
	}
	return markdown, nil
}

func ensureBranchAvailable(store *vault.Store, projects *ProjectService, resolved ResolvedContext, target domain.PRID) error {
	tasks, err := store.ListTasks(resolved.Project.ID)
	if err != nil {
		return projects.vaultError("scan Tasks for branch associations", err)
	}
	for _, task := range tasks {
		for _, pr := range task.PRs {
			if pr.Branch != resolved.Branch || (task.ID == resolved.Task.ID && pr.ID == target) {
				continue
			}
			return NewError(ErrorConflict, "branch %q is already associated with %s in Task %s", resolved.Branch, pr.ID, task.ID)
		}
	}
	return nil
}

func taskPlan(task domain.Task) domain.Plan {
	result := domain.Plan{PRs: make([]domain.PlannedPR, len(task.PRs))}
	for prIndex, pr := range task.PRs {
		planned := domain.PlannedPR{ID: pr.ID, Title: pr.Title, Steps: make([]domain.PlannedStep, len(pr.Steps))}
		for stepIndex, step := range pr.Steps {
			planned.Steps[stepIndex] = domain.PlannedStep{ID: step.ID, Title: step.Title}
		}
		result.PRs[prIndex] = planned
	}
	return result
}

func existingArtifactPaths(store *vault.Store, projects *ProjectService, projectID string, taskID domain.TaskID) (ArtifactPaths, error) {
	var result ArtifactPaths
	for _, artifact := range []vault.Artifact{vault.ArtifactTask, vault.ArtifactResearch, vault.ArtifactPlan, vault.ArtifactReview} {
		path, err := store.ArtifactPath(projectID, taskID, artifact)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return ArtifactPaths{}, projects.vaultError("resolve "+string(artifact)+" artifact path", err)
		}
		switch artifact {
		case vault.ArtifactTask:
			result.Task = path
		case vault.ArtifactResearch:
			result.Research = path
		case vault.ArtifactPlan:
			result.Plan = path
		case vault.ArtifactReview:
			result.Review = path
		}
	}
	return result, nil
}

func findPRListItem(task domain.Task, id domain.PRID) (PRListItem, bool) {
	for _, item := range prListItems(task) {
		if item.ID == id {
			return item, true
		}
	}
	return PRListItem{}, false
}

func findStepListItem(task domain.Task, id domain.StepID) (StepListItem, bool) {
	for _, item := range stepListItems(task) {
		if item.ID == id {
			return item, true
		}
	}
	return StepListItem{}, false
}

func hasErrorKindValue(err error, wanted ErrorKind) bool {
	kind, ok := ErrorKindOf(err)
	return ok && kind == wanted
}
