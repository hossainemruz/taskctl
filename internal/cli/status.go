package cli

import (
	"fmt"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
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
		Short: "Print detailed current Task and vault status as JSON",
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
			return writeJSON(command.OutOrStdout(), result, "Task status")
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
