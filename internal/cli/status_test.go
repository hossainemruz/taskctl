package cli

import (
	"testing"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/domain"
)

func TestRenderTaskStatusLifecycleGoldens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result app.StatusResult
		want   string
	}{
		{
			name:   "draft",
			result: statusGoldenBase(domain.TaskDraft, domain.Progress{}),
			want:   "Task: TASK-001 — Lifecycle\nProject: org_repo\nStatus: Draft\nProgress: 0/0 done (0 completed, 0 skipped)\n\nPRs:\n  none\n\nArtifacts:\n  none\n\nVault: not a Git repository\n",
		},
		{
			name: "skipped",
			result: func() app.StatusResult {
				result := statusGoldenBase(domain.TaskCompleted, domain.Progress{Skipped: 1, Total: 1})
				result.PRs = []app.StatusPR{{
					ID: "PR-001", Title: "Deferred", Status: domain.PRSkipped,
					Progress: domain.Progress{Total: 1}, SkipReason: "out of scope",
					Steps: []app.StatusStep{{ID: "STEP-001", Title: "Unused", Status: domain.StepPending}},
				}}
				return result
			}(),
			want: "Task: TASK-001 — Lifecycle\nProject: org_repo\nStatus: Completed\nProgress: 1/1 done (0 completed, 1 skipped)\n\nPRs:\n- PR-001: Deferred — Skipped\n  Progress: 0/1 done (0 completed, 0 skipped)\n  Skip reason: out of scope\n  - STEP-001: Unused — Pending\n\nArtifacts:\n  none\n\nVault: not a Git repository\n",
		},
		{
			name: "completed",
			result: func() app.StatusResult {
				result := statusGoldenBase(domain.TaskCompleted, domain.Progress{Completed: 1, Total: 1})
				result.PRs = []app.StatusPR{{
					ID: "PR-001", Title: "Delivery", Status: domain.PRCompleted,
					Progress: domain.Progress{Completed: 1, Skipped: 1, Total: 2}, Branch: "feature/done",
					Steps: []app.StatusStep{
						{ID: "STEP-001", Title: "Accepted", Status: domain.StepCompleted},
						{ID: "STEP-002", Title: "Removed", Status: domain.StepSkipped, SkipReason: "superseded"},
					},
				}}
				return result
			}(),
			want: "Task: TASK-001 — Lifecycle\nProject: org_repo\nStatus: Completed\nProgress: 1/1 done (1 completed, 0 skipped)\n\nPRs:\n- PR-001: Delivery — Completed\n  Progress: 2/2 done (1 completed, 1 skipped)\n  Branch: feature/done\n  - STEP-001: Accepted — Completed\n  - STEP-002: Removed — Skipped — reason: superseded\n\nArtifacts:\n  none\n\nVault: not a Git repository\n",
		},
		{
			name: "cancelled",
			result: func() app.StatusResult {
				result := statusGoldenBase(domain.TaskCancelled, domain.Progress{Total: 1})
				result.PRs = []app.StatusPR{{
					ID: "PR-001", Title: "Abandoned", Status: domain.PRPending,
					Progress: domain.Progress{Total: 1},
					Steps:    []app.StatusStep{{ID: "STEP-001", Title: "Never started", Status: domain.StepPending}},
				}}
				return result
			}(),
			want: "Task: TASK-001 — Lifecycle\nProject: org_repo\nStatus: Cancelled\nProgress: 0/1 done (0 completed, 0 skipped)\n\nPRs:\n- PR-001: Abandoned — Pending\n  Progress: 0/1 done (0 completed, 0 skipped)\n  - STEP-001: Never started — Pending\n\nArtifacts:\n  none\n\nVault: not a Git repository\n",
		},
		{
			name: "reopened",
			result: func() app.StatusResult {
				result := statusGoldenBase(domain.TaskInProgress, domain.Progress{Total: 1})
				result.CurrentPR = "PR-001"
				result.PRs = []app.StatusPR{{
					ID: "PR-001", Title: "Delivery", Status: domain.PRInProgress,
					Progress: domain.Progress{Completed: 1, Total: 2}, Branch: "feature/reopened", Current: true,
					Steps: []app.StatusStep{
						{ID: "STEP-001", Title: "Accepted", Status: domain.StepCompleted},
						{ID: "STEP-002", Title: "Correction", Status: domain.StepPending},
					},
				}}
				return result
			}(),
			want: "Task: TASK-001 — Lifecycle\nProject: org_repo\nStatus: In Progress\nProgress: 0/1 done (0 completed, 0 skipped)\nCurrent PR: PR-001\n\nPRs:\n* PR-001: Delivery — In Progress\n  Progress: 1/2 done (1 completed, 0 skipped)\n  Branch: feature/reopened\n  - STEP-001: Accepted — Completed\n  - STEP-002: Correction — Pending\n\nArtifacts:\n  none\n\nVault: not a Git repository\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := renderTaskStatus(test.result); got != test.want {
				t.Fatalf("renderTaskStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func statusGoldenBase(status domain.TaskStatus, progress domain.Progress) app.StatusResult {
	return app.StatusResult{
		ProjectID: "org_repo", TaskID: "TASK-001", Title: "Lifecycle", Status: status, Progress: progress,
		Vault: app.VaultStatusResult{State: app.VaultStatusNotRepository},
	}
}
