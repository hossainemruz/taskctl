package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/spf13/cobra"
)

type projectFlags struct {
	projectID  string
	repository string
}

func (f *projectFlags) bind(command *cobra.Command) {
	command.Flags().StringVar(&f.projectID, "project-id", "", "explicit portable project ID (requires --repository)")
	command.Flags().StringVar(&f.repository, "repository", "", "explicit normalized repository identity (requires --project-id)")
}

func (f projectFlags) input(environment config.Environment) (app.ProjectInput, error) {
	directory, err := environment.WorkingDirectory()
	if err != nil {
		return app.ProjectInput{}, app.WrapError(app.ErrorMissingContext, err, "resolve current directory: %v", err)
	}
	return app.ProjectInput{Directory: directory, ProjectID: f.projectID, Repository: f.repository}, nil
}

func newNewCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	var prefix string
	var nonInteractive bool
	command := &cobra.Command{
		Use:   "new <title>",
		Short: "Create and select a new Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			defaults, err := workflow.NewDefaults(command.Context(), project)
			if err != nil {
				return err
			}
			selectedPrefix := strings.TrimSpace(prefix)
			if !defaults.ProjectRegistered && selectedPrefix == "" {
				if nonInteractive {
					return app.NewError(app.ErrorUsage, "--prefix is required when registering a project non-interactively (suggested: %s)", defaults.TaskPrefix)
				}
				selectedPrefix, err = prompt(bufio.NewReader(command.InOrStdin()), command.ErrOrStderr(), "Task prefix", string(defaults.TaskPrefix))
				if err != nil {
					return err
				}
			}
			result, err := workflow.NewTask(command.Context(), app.NewTaskInput{
				ProjectInput: project, Title: args[0], TaskPrefix: selectedPrefix,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Created %s: %s\nCurrent Task: %s\n", result.Task.ID, result.Task.Title, result.Task.ID); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Task creation result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	command.Flags().StringVar(&prefix, "prefix", "", "Task ID prefix used when registering this project")
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "fail instead of prompting for a new project prefix")
	return command
}

func newUseCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "use <task-id>",
		Short: "Select the project fallback Task",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			task, err := workflow.UseTask(command.Context(), project, args[0])
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Current Task: %s\n", task.ID); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Task selection result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}

func newTaskCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	command := &cobra.Command{Use: "task", Short: "Manage project Tasks", Args: cobra.NoArgs}
	command.AddCommand(newTaskListCommand(workflow, environment), newTaskCancelCommand(workflow, environment))
	return command
}

func newTaskListCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "list",
		Short: "List Tasks registered for the current project",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			tasks, err := workflow.ListTasks(command.Context(), project)
			if err != nil {
				return err
			}
			if len(tasks) == 0 {
				_, err = fmt.Fprintln(command.OutOrStdout(), "No Tasks.")
			} else {
				for _, task := range tasks {
					marker := " "
					if task.Current {
						marker = "*"
					}
					if _, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s %s  %-11s  %s\n", marker, task.ID, task.Status, task.Title); writeErr != nil {
						err = writeErr
						break
					}
				}
			}
			if err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Task list: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}

func newTaskCancelCommand(workflow WorkflowService, environment config.Environment) *cobra.Command {
	var flags projectFlags
	command := &cobra.Command{
		Use:   "cancel [task-id]",
		Short: "Cancel a Task explicitly",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			project, err := flags.input(environment)
			if err != nil {
				return err
			}
			var id string
			if len(args) == 1 {
				id = args[0]
			}
			task, err := workflow.CancelTask(command.Context(), project, id)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(command.OutOrStdout(), "Cancelled Task: %s\n", task.ID); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write Task cancellation result: %v", err)
			}
			return nil
		},
	}
	flags.bind(command)
	return command
}
