package cli

import (
	"fmt"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/spf13/cobra"
)

func newPathCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "path <task|research|plan|review>",
		Short: "Print an existing artifact path",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			path, err := workflow.ArtifactPath(command.Context(), project, args[0])
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), path); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write artifact path: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}

func newArtifactCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	command := &cobra.Command{Use: "artifact", Short: "Create and view Task artifacts", Args: cobra.NoArgs}
	command.AddCommand(newArtifactEnsureCommand(workflow, environment), newArtifactViewCommand(workflow, environment))
	return command
}

func newArtifactEnsureCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "ensure <research|plan|review>",
		Short: "Create a missing artifact from its vault template",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			result, err := workflow.EnsureArtifact(command.Context(), project, args[0])
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), result.Path); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write artifact result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}

func newArtifactViewCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "view",
		Short: "Open the current Task directory in the configured viewer",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			directory, err := workflow.ViewArtifacts(command.Context(), project)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Opened Task artifacts: %s\n", directory); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write viewer result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}
