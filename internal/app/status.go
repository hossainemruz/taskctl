package app

import (
	"context"

	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/gitcli"
	"github.com/hossainemruz/taskctl/internal/vault"
)

type ContextActiveStep struct {
	ID     domain.StepID     `json:"id"`
	Status domain.StepStatus `json:"status"`
}

type ContextPR struct {
	ID         domain.PRID        `json:"id"`
	Status     domain.PRStatus    `json:"status"`
	Progress   domain.Progress    `json:"progress"`
	ActiveStep *ContextActiveStep `json:"active_step,omitempty"`
}

type ContextResult struct {
	ProjectID string            `json:"project_id"`
	TaskID    domain.TaskID     `json:"task_id"`
	Status    domain.TaskStatus `json:"status"`
	Progress  domain.Progress   `json:"progress"`
	CurrentPR *ContextPR        `json:"current_pr,omitempty"`
	Artifacts ArtifactPaths     `json:"artifacts"`
}

type StatusStep struct {
	ID         domain.StepID
	Title      string
	Status     domain.StepStatus
	SkipReason string
	Active     bool
}

type StatusPR struct {
	ID         domain.PRID
	Title      string
	Status     domain.PRStatus
	Progress   domain.Progress
	Branch     string
	SkipReason string
	Current    bool
	Steps      []StatusStep
}

type StatusResult struct {
	ProjectID  string
	TaskID     domain.TaskID
	Title      string
	Status     domain.TaskStatus
	Progress   domain.Progress
	CurrentPR  domain.PRID
	ActiveStep domain.StepID
	PRs        []StatusPR
	Artifacts  ArtifactPaths
	Vault      VaultStatusResult
}

type VaultStatusResult = gitcli.VaultStatus

const (
	VaultStatusOK                = gitcli.VaultStatusOK
	VaultStatusNotRepository     = gitcli.VaultStatusNotRepository
	VaultStatusNoUpstream        = gitcli.VaultStatusNoUpstream
	VaultStatusRemoteUnavailable = gitcli.VaultStatusRemoteUnavailable
	VaultStatusUnavailable       = gitcli.VaultStatusUnavailable
)

func (w *Workflow) Context(ctx context.Context, input ProjectInput) (ContextResult, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return ContextResult{}, err
	}
	resolved, artifacts, err := resolveStatusSnapshot(ctx, store, projects, input)
	if err != nil {
		return ContextResult{}, err
	}
	result := ContextResult{
		ProjectID: resolved.Project.ID,
		TaskID:    resolved.Task.ID,
		Status:    resolved.Task.Status(),
		Progress:  resolved.Task.Progress(),
		Artifacts: artifacts,
	}
	if resolved.CurrentPR != nil {
		current, err := contextPR(*resolved.CurrentPR)
		if err != nil {
			return ContextResult{}, err
		}
		result.CurrentPR = &current
	}
	return result, nil
}

func (w *Workflow) Status(ctx context.Context, input ProjectInput) (StatusResult, error) {
	local, store, projects, err := w.runtime()
	if err != nil {
		return StatusResult{}, err
	}
	resolved, artifacts, err := resolveStatusSnapshot(ctx, store, projects, input)
	if err != nil {
		return StatusResult{}, err
	}
	result := StatusResult{
		ProjectID: resolved.Project.ID,
		TaskID:    resolved.Task.ID,
		Title:     resolved.Task.Title,
		Status:    resolved.Task.Status(),
		Progress:  resolved.Task.Progress(),
		Artifacts: artifacts,
		PRs:       make([]StatusPR, len(resolved.Task.PRs)),
	}
	if resolved.CurrentPR != nil {
		result.CurrentPR = resolved.CurrentPR.ID
	}
	for prIndex, pr := range resolved.Task.PRs {
		item := StatusPR{
			ID: pr.ID, Title: pr.Title, Status: pr.Status(), Progress: pr.Progress(), Branch: pr.Branch,
			SkipReason: pr.SkipReason, Current: pr.ID == result.CurrentPR, Steps: make([]StatusStep, len(pr.Steps)),
		}
		active, activeErr := pr.ActiveStep()
		if activeErr != nil {
			return StatusResult{}, WrapError(ErrorInvalidData, activeErr, "PR %s has invalid active Step state: %v", pr.ID, activeErr)
		}
		for stepIndex, step := range pr.Steps {
			isActive := active != nil && active.ID == step.ID
			item.Steps[stepIndex] = StatusStep{ID: step.ID, Title: step.Title, Status: step.Status,
				SkipReason: step.SkipReason, Active: isActive}
			if isActive && item.Current {
				result.ActiveStep = step.ID
			}
		}
		result.PRs[prIndex] = item
	}
	result.Vault = w.inspectVaultStatus(ctx, local.Vault)
	return result, nil
}

func (w *Workflow) VaultStatus(ctx context.Context) (VaultStatusResult, error) {
	local, _, _, err := w.runtime()
	if err != nil {
		return VaultStatusResult{}, err
	}
	return w.inspectVaultStatus(ctx, local.Vault), nil
}

func (w *Workflow) inspectVaultStatus(ctx context.Context, directory string) VaultStatusResult {
	return gitcli.NewClient(w.processes).InspectVault(ctx, directory)
}

func resolveStatusSnapshot(ctx context.Context, store *vault.Store, projects *ProjectService, input ProjectInput) (ResolvedContext, ArtifactPaths, error) {
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return ResolvedContext{}, ArtifactPaths{}, err
	}
	artifacts, err := existingArtifactPaths(store, projects, resolved.Project.ID, resolved.Task.ID)
	if err != nil {
		return ResolvedContext{}, ArtifactPaths{}, err
	}
	return resolved, artifacts, nil
}

func contextPR(pr domain.PR) (ContextPR, error) {
	result := ContextPR{ID: pr.ID, Status: pr.Status(), Progress: pr.Progress()}
	active, err := pr.ActiveStep()
	if err != nil {
		return ContextPR{}, WrapError(ErrorInvalidData, err, "PR %s has invalid active Step state: %v", pr.ID, err)
	}
	if active != nil {
		result.ActiveStep = &ContextActiveStep{ID: active.ID, Status: active.Status}
	}
	return result, nil
}
