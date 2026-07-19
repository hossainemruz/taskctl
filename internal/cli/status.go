package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/spf13/cobra"
)

func newContextCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "context",
		Short: "Print the current Task context as JSON",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			result, err := workflow.Context(command.Context(), project)
			if err != nil {
				return err
			}
			return writeJSON(command.OutOrStdout(), result, "context")
		},
	}
	flags.bind(command)
	return command
}

func newStatusCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "status",
		Short: "Show detailed current Task and vault status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			result, err := workflow.Status(command.Context(), project)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(command.OutOrStdout(), renderTaskStatus(result)); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Task status: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}

func newVaultCommand(workflow WorkflowService) *cobra.Command {
	command := &cobra.Command{Use: "vault", Short: "Inspect the configured vault", Args: cobra.NoArgs}
	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Fetch and show vault Git status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			status, err := workflow.VaultStatus(command.Context())
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), renderVaultStatus(status)); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write vault status: %v", err)
			}
			return nil
		},
	})
	return command
}

func renderTaskStatus(result app.StatusResult) string {
	var output strings.Builder
	fmt.Fprintf(&output, "Task: %s — %s\n", result.TaskID, result.Title)
	fmt.Fprintf(&output, "Project: %s\n", result.ProjectID)
	fmt.Fprintf(&output, "Status: %s\n", humanStatus(string(result.Status)))
	fmt.Fprintf(&output, "Progress: %s\n", detailedProgress(result.Progress))
	if result.CurrentPR != "" {
		fmt.Fprintf(&output, "Current PR: %s\n", result.CurrentPR)
	}
	if result.ActiveStep != "" {
		fmt.Fprintf(&output, "Active Step: %s\n", result.ActiveStep)
	}

	output.WriteString("\nPRs:\n")
	if len(result.PRs) == 0 {
		output.WriteString("  none\n")
	}
	for _, pr := range result.PRs {
		marker := "-"
		if pr.Current {
			marker = "*"
		}
		fmt.Fprintf(&output, "%s %s: %s — %s\n", marker, pr.ID, pr.Title, humanStatus(string(pr.Status)))
		fmt.Fprintf(&output, "  Progress: %s\n", detailedProgress(pr.Progress))
		if pr.Branch != "" {
			fmt.Fprintf(&output, "  Branch: %s\n", pr.Branch)
		}
		if pr.SkipReason != "" {
			fmt.Fprintf(&output, "  Skip reason: %s\n", pr.SkipReason)
		}
		for _, step := range pr.Steps {
			stepMarker := "-"
			if step.Active {
				stepMarker = ">"
			}
			fmt.Fprintf(&output, "  %s %s: %s — %s", stepMarker, step.ID, step.Title, humanStatus(string(step.Status)))
			if step.SkipReason != "" {
				fmt.Fprintf(&output, " — reason: %s", step.SkipReason)
			}
			output.WriteByte('\n')
		}
	}

	output.WriteString("\nArtifacts:\n")
	artifactCount := writeArtifactStatus(&output, result.Artifacts)
	if artifactCount == 0 {
		output.WriteString("  none\n")
	}
	fmt.Fprintf(&output, "\n%s\n", renderVaultStatus(result.Vault))
	return output.String()
}

func writeArtifactStatus(output *strings.Builder, artifacts app.ArtifactPaths) int {
	values := []struct {
		name string
		path string
	}{
		{name: "task", path: artifacts.Task},
		{name: "research", path: artifacts.Research},
		{name: "plan", path: artifacts.Plan},
		{name: "review", path: artifacts.Review},
	}
	count := 0
	for _, value := range values {
		if value.path == "" {
			continue
		}
		fmt.Fprintf(output, "  %s: %s\n", value.name, value.path)
		count++
	}
	return count
}

func renderVaultStatus(status app.VaultStatusResult) string {
	switch status.State {
	case app.VaultStatusNotRepository:
		return "Vault: not a Git repository"
	case app.VaultStatusUnavailable:
		return "Vault: Git status unavailable"
	}
	local := "clean"
	if status.Dirty == 1 {
		local = "1 uncommitted file"
	} else if status.Dirty > 1 {
		local = fmt.Sprintf("%d uncommitted files", status.Dirty)
	}
	switch status.State {
	case app.VaultStatusNoUpstream:
		return "Vault: " + local + " · no upstream"
	case app.VaultStatusRemoteUnavailable:
		return "Vault: " + local + " · remote status unavailable"
	case app.VaultStatusOK:
		return fmt.Sprintf("Vault: %s · ahead %d · behind %d", local, status.Ahead, status.Behind)
	default:
		return "Vault: Git status unavailable"
	}
}

func detailedProgress(progress domain.Progress) string {
	return fmt.Sprintf("%d/%d done (%d completed, %d skipped)",
		progress.Completed+progress.Skipped, progress.Total, progress.Completed, progress.Skipped)
}

func humanStatus(status string) string {
	switch status {
	case "in_progress":
		return "In Progress"
	case "ready_for_review":
		return "Ready for Review"
	}
	words := strings.Split(status, "_")
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
