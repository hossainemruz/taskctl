package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/process"
	"github.com/spf13/cobra"
)

// InitService is the application surface needed by the bootstrap CLI.
type InitService interface {
	Defaults(context.Context) (app.InitDefaults, error)
	Init(context.Context, app.InitInput) (app.InitResult, error)
}

type WorkflowService interface {
	NewDefaults(context.Context, app.ProjectInput) (app.NewTaskDefaults, error)
	NewTask(context.Context, app.NewTaskInput) (app.NewTaskResult, error)
	UseTask(context.Context, app.ProjectInput, string) (domain.Task, error)
	ListTasks(context.Context, app.ProjectInput) ([]app.TaskListItem, error)
	CancelTask(context.Context, app.ProjectInput, string) (domain.Task, error)
	EnsureArtifact(context.Context, app.ProjectInput, string) (app.ArtifactResult, error)
	ArtifactPath(context.Context, app.ProjectInput, string) (string, error)
	ViewArtifacts(context.Context, app.ProjectInput) (string, error)
	ApplyPlan(context.Context, app.ProjectInput, io.Reader) (app.PlanApplyResult, error)
	AddPR(context.Context, app.ProjectInput, string) (domain.PRID, error)
	ListPRs(context.Context, app.ProjectInput) ([]app.PRListItem, error)
	StartPR(context.Context, app.ProjectInput, string) (app.PRListItem, error)
	SkipPR(context.Context, app.ProjectInput, string, string) (app.PRListItem, error)
	AddStep(context.Context, app.ProjectInput, string, string) (domain.StepID, error)
	ListSteps(context.Context, app.ProjectInput) ([]app.StepListItem, error)
	GetStep(context.Context, app.ProjectInput) (app.StepGetResult, error)
	StartStep(context.Context, app.ProjectInput, string) (app.StepListItem, error)
	SubmitStep(context.Context, app.ProjectInput, string) (app.StepListItem, error)
	ReviseStep(context.Context, app.ProjectInput, string) (app.StepListItem, error)
	CompleteStep(context.Context, app.ProjectInput, string) (app.StepListItem, error)
	SkipStep(context.Context, app.ProjectInput, string, string) (app.StepListItem, error)
	ReopenStep(context.Context, app.ProjectInput, string) (app.StepListItem, error)
}

// Dependencies contains process-global facilities so command instances remain
// isolated and safe to construct repeatedly in tests.
type Dependencies struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Environment config.Environment
	Processes   process.Runner
	Initializer InitService
	Workflow    WorkflowService
	Version     string
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Environment: config.OSEnvironment{},
		Processes:   process.ExecRunner{},
		Version:     "dev",
	}
}

// NewRootCommand constructs a fresh Cobra tree without package-level flags.
func NewRootCommand(dependencies Dependencies) *cobra.Command {
	dependencies = fillDependencyDefaults(dependencies)
	initializer := dependencies.Initializer
	if initializer == nil {
		initializer = app.NewInitializer(dependencies.Environment)
	}
	workflow := dependencies.Workflow
	if workflow == nil {
		workflow = app.NewWorkflow(dependencies.Environment, dependencies.Processes)
	}

	root := &cobra.Command{
		Use:               "taskctl",
		Short:             "Manage agent task state in a synchronized vault",
		Version:           dependencies.Version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		Args:              cobra.NoArgs,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Help(); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write help: %v", err)
			}
			return nil
		},
	}
	root.SetIn(dependencies.Stdin)
	root.SetOut(dependencies.Stdout)
	root.SetErr(dependencies.Stderr)
	root.SetVersionTemplate("taskctl {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return app.WrapError(app.ErrorUsage, err, "%v", err)
	})

	root.AddCommand(newInitCommand(initializer))
	root.AddCommand(newNewCommand(workflow, dependencies.Environment))
	root.AddCommand(newUseCommand(workflow, dependencies.Environment))
	root.AddCommand(newTaskCommand(workflow, dependencies.Environment))
	root.AddCommand(newPathCommand(workflow, dependencies.Environment))
	root.AddCommand(newArtifactCommand(workflow, dependencies.Environment))
	root.AddCommand(newPlanCommand(workflow, dependencies.Environment))
	root.AddCommand(newPRCommand(workflow, dependencies.Environment))
	root.AddCommand(newStepCommand(workflow, dependencies.Environment))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the taskctl version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintf(command.OutOrStdout(), "taskctl %s\n", dependencies.Version); err != nil {
				return app.WrapError(app.ErrorInternal, err, "write version: %v", err)
			}
			return nil
		},
	})
	return root
}

// Execute runs one command tree and returns a stable process exit code.
func Execute(ctx context.Context, dependencies Dependencies, args []string) int {
	root := NewRootCommand(dependencies)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitSuccess
	}
	if _, categorized := app.ErrorKindOf(err); !categorized {
		// Errors produced by Cobra before a callback (for example, an unknown
		// command) are usage failures. Callback errors must be categorized.
		err = app.WrapError(app.ErrorUsage, err, "%v", err)
	}
	_, _ = fmt.Fprintf(root.ErrOrStderr(), "Error: %s\n", err)
	return ExitCode(err)
}

func fillDependencyDefaults(dependencies Dependencies) Dependencies {
	if dependencies.Stdin == nil {
		dependencies.Stdin = strings.NewReader("")
	}
	if dependencies.Stdout == nil {
		dependencies.Stdout = io.Discard
	}
	if dependencies.Stderr == nil {
		dependencies.Stderr = io.Discard
	}
	if dependencies.Environment == nil {
		dependencies.Environment = config.OSEnvironment{}
	}
	if dependencies.Processes == nil {
		dependencies.Processes = process.ExecRunner{}
	}
	if dependencies.Version == "" {
		dependencies.Version = "dev"
	}
	return dependencies
}
