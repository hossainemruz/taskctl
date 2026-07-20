package cli

import (
	"bytes"
	"testing"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/domain"
)

func TestWriteTaskStatusJSON(t *testing.T) {
	t.Parallel()
	result := app.StatusResult{
		ProjectID:  "org_repo",
		TaskID:     "TASK-001",
		Title:      "Lifecycle",
		Status:     domain.TaskInProgress,
		Progress:   domain.Progress{Total: 1},
		CurrentPR:  "PR-001",
		ActiveStep: "STEP-002",
		PRs: []app.StatusPR{{
			ID: "PR-001", Title: "Delivery", Status: domain.PRInProgress,
			Progress: domain.Progress{Completed: 1, Total: 2}, Branch: "feature/work", Current: true,
			Steps: []app.StatusStep{
				{ID: "STEP-001", Title: "Accepted", Status: domain.StepCompleted},
				{ID: "STEP-002", Title: "Correction", Status: domain.StepSkipped, SkipReason: "superseded", Active: true},
			},
		}},
		Artifacts: app.ArtifactPaths{Task: "/vault/task.md"},
		Vault:     app.VaultStatusResult{State: app.VaultStatusOK, Dirty: 1, Ahead: 2, Behind: 3},
	}

	var output bytes.Buffer
	if err := writeJSON(&output, result, "Task status"); err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"project_id\": \"org_repo\",\n" +
		"  \"task_id\": \"TASK-001\",\n" +
		"  \"title\": \"Lifecycle\",\n" +
		"  \"status\": \"in_progress\",\n" +
		"  \"progress\": {\n    \"completed\": 0,\n    \"skipped\": 0,\n    \"total\": 1\n  },\n" +
		"  \"current_pr\": \"PR-001\",\n" +
		"  \"active_step\": \"STEP-002\",\n" +
		"  \"prs\": [\n    {\n      \"id\": \"PR-001\",\n      \"title\": \"Delivery\",\n      \"status\": \"in_progress\",\n      \"progress\": {\n        \"completed\": 1,\n        \"skipped\": 0,\n        \"total\": 2\n      },\n      \"branch\": \"feature/work\",\n      \"current\": true,\n      \"steps\": [\n        {\n          \"id\": \"STEP-001\",\n          \"title\": \"Accepted\",\n          \"status\": \"completed\",\n          \"active\": false\n        },\n        {\n          \"id\": \"STEP-002\",\n          \"title\": \"Correction\",\n          \"status\": \"skipped\",\n          \"skip_reason\": \"superseded\",\n          \"active\": true\n        }\n      ]\n    }\n  ],\n" +
		"  \"artifacts\": {\n    \"task\": \"/vault/task.md\"\n  },\n" +
		"  \"vault\": {\n    \"state\": \"ok\",\n    \"dirty\": 1,\n    \"ahead\": 2,\n    \"behind\": 3\n  }\n}\n"
	if output.String() != want {
		t.Fatalf("Task status JSON = %q, want %q", output.String(), want)
	}
}
