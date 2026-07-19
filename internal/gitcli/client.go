package gitcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	processutil "github.com/hossainemruz/taskctl/internal/process"
)

const defaultExecutable = "git"

// Client hides Git command syntax behind repository-oriented operations.
type Client struct {
	runner       processutil.Runner
	executable   string
	fetchTimeout time.Duration
}

func NewClient(runner processutil.Runner) *Client {
	return &Client{runner: runner, executable: defaultExecutable, fetchTimeout: vaultFetchTimeout}
}

// Origin returns remote.origin.url exactly as configured apart from surrounding
// whitespace. Call NormalizeRemote before persisting or comparing it.
func (c *Client) Origin(ctx context.Context, directory string) (string, error) {
	result, err := c.run(ctx, directory, "config", "--get", "remote.origin.url")
	if err != nil {
		if commandExitCode(err) == 1 && len(strings.TrimSpace(string(result.Stdout))) == 0 {
			return "", ErrNoOrigin
		}
		return "", fmt.Errorf("%w: read origin: %w", ErrCommand, err)
	}
	origin := strings.TrimSpace(string(result.Stdout))
	if origin == "" || strings.ContainsAny(origin, "\x00\r\n") {
		return "", fmt.Errorf("%w: origin value is empty or malformed", ErrInvalidRemote)
	}
	return origin, nil
}

func (c *Client) Repository(ctx context.Context, directory string) (Repository, error) {
	origin, err := c.Origin(ctx, directory)
	if err != nil {
		return Repository{}, err
	}
	return NormalizeRemote(origin)
}

// CurrentBranch returns the exact short branch name or ErrDetachedHEAD.
func (c *Client) CurrentBranch(ctx context.Context, directory string) (string, error) {
	result, err := c.run(ctx, directory, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if commandExitCode(err) == 1 {
			return "", ErrDetachedHEAD
		}
		return "", fmt.Errorf("%w: read current branch: %w", ErrCommand, err)
	}
	branch := strings.TrimSpace(string(result.Stdout))
	if branch == "" || strings.ContainsAny(branch, "\x00\r\n") {
		return "", fmt.Errorf("%w: Git returned an invalid branch name", ErrCommand)
	}
	return branch, nil
}

func (c *Client) run(ctx context.Context, directory string, arguments ...string) (processutil.Result, error) {
	return c.runWithEnv(ctx, directory, nil, arguments...)
}

func (c *Client) runWithEnv(ctx context.Context, directory string, environment []string, arguments ...string) (processutil.Result, error) {
	if c == nil || c.runner == nil {
		return processutil.Result{}, fmt.Errorf("%w: process runner is not configured", ErrCommand)
	}
	executable := c.executable
	if executable == "" {
		executable = defaultExecutable
	}
	return c.runner.Run(ctx, processutil.Command{
		Name: executable,
		Args: append([]string(nil), arguments...),
		Dir:  directory,
		Env:  environment,
	})
}

func commandExitCode(err error) int {
	var commandError *processutil.CommandError
	if errors.As(err, &commandError) {
		return commandError.ExitCode
	}
	return -1
}
