package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/spf13/cobra"
)

func newPlanCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	command := &cobra.Command{Use: "plan", Short: "Register and validate structured plans", Args: cobra.NoArgs}
	command.AddCommand(newPlanApplyCommand(workflow, environment))
	return command
}

func newPlanApplyCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var file string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a structured JSON hierarchy to plan.md",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			reader := io.Reader(command.InOrStdin())
			if strings.TrimSpace(file) != "" {
				opened, openErr := os.Open(file)
				if openErr != nil {
					return app.WrapError(app.ErrorUsage, openErr, "open plan input %q: %v", file, openErr)
				}
				defer opened.Close()
				reader = opened
			}
			result, err := workflow.ApplyPlan(command.Context(), project, reader)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Applied plan to %s: %d PRs, %d Steps\n", result.Task.ID, result.PRCount, result.StepCount); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write plan result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&file, "file", "", "read structured plan JSON from this file instead of standard input")
	return command
}

func newPRCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	command := &cobra.Command{Use: "pr", Short: "Manage Task PRs", Args: cobra.NoArgs}
	command.AddCommand(
		newPRAddCommand(workflow, environment),
		newPRListCommand(workflow, environment),
		newPRSkipCommand(workflow, environment),
	)
	return command
}

func newPRAddCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var titleFlag string
	command := &cobra.Command{
		Use:   "add [title]",
		Short: "Append a PR and allocate its ID",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			title, err := planningTitle(args, titleFlag)
			if err != nil {
				return err
			}
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			id, err := workflow.AddPR(command.Context(), project, title)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), id); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write allocated PR ID: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&titleFlag, "title", "", "PR title")
	return command
}

func newPRListCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List PRs in the current Task",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			items, err := workflow.ListPRs(command.Context(), project)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeJSON(command.OutOrStdout(), items, "PR list")
			}
			if len(items) == 0 {
				_, err = fmt.Fprintln(command.OutOrStdout(), "No PRs.")
			} else {
				for _, item := range items {
					if _, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s  %-11s  %s  %s\n",
						item.ID, item.Status, humanProgress(item.Progress), item.Title); writeErr != nil {
						err = writeErr
						break
					}
				}
			}
			if err != nil {
				return app.WrapError(app.ErrorInternal, err, "write PR list: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().BoolVar(&jsonOutput, "json", false, "write the ordered list as JSON")
	return command
}

func newPRSkipCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var reason string
	command := &cobra.Command{
		Use:   "skip <pr-id>",
		Short: "Remove a PR from scope with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return app.NewError(app.ErrorUsage, "--reason is required")
			}
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			item, err := workflow.SkipPR(command.Context(), project, args[0], reason)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Skipped PR: %s\n", item.ID); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write PR skip result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&reason, "reason", "", "reason the PR is no longer in scope")
	return command
}

func newStepCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	command := &cobra.Command{Use: "step", Short: "Manage Task Steps", Args: cobra.NoArgs}
	command.AddCommand(
		newStepAddCommand(workflow, environment),
		newStepListCommand(workflow, environment),
		newStepSkipCommand(workflow, environment),
	)
	return command
}

func newStepAddCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var prID, title string
	command := &cobra.Command{
		Use:   "add",
		Short: "Append a Step and allocate its Task-wide ID",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(prID) == "" {
				return app.NewError(app.ErrorUsage, "--pr is required")
			}
			if strings.TrimSpace(title) == "" {
				return app.NewError(app.ErrorUsage, "--title is required")
			}
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			id, err := workflow.AddStep(command.Context(), project, prID, title)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(command.OutOrStdout(), id); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write allocated Step ID: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&prID, "pr", "", "parent PR ID")
	command.Flags().StringVar(&title, "title", "", "Step title")
	return command
}

func newStepListCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List Steps in the current Task",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			items, err := workflow.ListSteps(command.Context(), project)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeJSON(command.OutOrStdout(), items, "Step list")
			}
			if len(items) == 0 {
				_, err = fmt.Fprintln(command.OutOrStdout(), "No Steps.")
			} else {
				for _, item := range items {
					if _, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s  %s  %-16s  %s\n", item.ID, item.PRID, item.Status, item.Title); writeErr != nil {
						err = writeErr
						break
					}
				}
			}
			if err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Step list: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().BoolVar(&jsonOutput, "json", false, "write the ordered list as JSON")
	return command
}

func newStepSkipCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var reason string
	command := &cobra.Command{
		Use:   "skip <step-id>",
		Short: "Remove a Step from scope with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return app.NewError(app.ErrorUsage, "--reason is required")
			}
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			item, err := workflow.SkipStep(command.Context(), project, args[0], reason)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Skipped Step: %s\n", item.ID); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Step skip result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&reason, "reason", "", "reason the Step is no longer in scope")
	return command
}

func planningTitle(args []string, flagValue string) (string, error) {
	if len(args) == 1 && flagValue != "" {
		return "", app.NewError(app.ErrorUsage, "provide the PR title either as an argument or with --title, not both")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	if strings.TrimSpace(flagValue) == "" {
		return "", app.NewError(app.ErrorUsage, "a PR title argument or --title is required")
	}
	return flagValue, nil
}

func humanProgress(progress domain.Progress) string {
	return fmt.Sprintf("%d/%d complete, %d skipped", progress.Completed+progress.Skipped, progress.Total, progress.Skipped)
}

func writeJSON(writer io.Writer, value any, label string) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return app.WrapError(app.ErrorInternal, err, "write %s JSON: %v", label, err)
	}
	return nil
}
