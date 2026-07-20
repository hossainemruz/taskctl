package gitcli

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

const vaultFetchTimeout = 15 * time.Second

type VaultStatusState string

const (
	VaultStatusOK                VaultStatusState = "ok"
	VaultStatusNotRepository     VaultStatusState = "not_repository"
	VaultStatusNoUpstream        VaultStatusState = "no_upstream"
	VaultStatusRemoteUnavailable VaultStatusState = "remote_unavailable"
	VaultStatusUnavailable       VaultStatusState = "unavailable"
)

// VaultStatus is a fresh, read-only summary of the vault worktree and its
// upstream. Ahead/behind counts are populated only after a successful fetch.
type VaultStatus struct {
	State  VaultStatusState `json:"state"`
	Dirty  int              `json:"dirty"`
	Ahead  int              `json:"ahead"`
	Behind int              `json:"behind"`
}

// InspectVault fetches remote-tracking state, then inspects local changes and
// upstream divergence. Operational Git failures are represented in State so a
// Task status can remain useful when synchronization facts are unavailable.
func (c *Client) InspectVault(ctx context.Context, directory string) VaultStatus {
	repository, err := c.run(ctx, directory, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if commandExitCode(err) >= 0 {
			return VaultStatus{State: VaultStatusNotRepository}
		}
		return VaultStatus{State: VaultStatusUnavailable}
	}
	if strings.TrimSpace(string(repository.Stdout)) != "true" {
		return VaultStatus{State: VaultStatusNotRepository}
	}

	timeout := c.fetchTimeout
	if timeout <= 0 {
		timeout = vaultFetchTimeout
	}
	fetchContext, cancelFetch := context.WithTimeout(ctx, timeout)
	_, fetchErr := c.runWithEnv(fetchContext, directory, gitFetchEnvironment(), "fetch", "--quiet")
	cancelFetch()
	dirtyResult, err := c.run(ctx, directory, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return VaultStatus{State: VaultStatusUnavailable}
	}
	status := VaultStatus{Dirty: countPorcelainEntries(dirtyResult.Stdout)}

	_, upstreamErr := c.run(ctx, directory, "rev-parse", "--verify", "--quiet", "@{upstream}")
	if upstreamErr != nil {
		if code := commandExitCode(upstreamErr); code == 1 || code == 128 {
			status.State = VaultStatusNoUpstream
			return status
		}
		status.State = VaultStatusRemoteUnavailable
		return status
	}
	if fetchErr != nil {
		status.State = VaultStatusRemoteUnavailable
		return status
	}

	counts, err := c.run(ctx, directory, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		status.State = VaultStatusRemoteUnavailable
		return status
	}
	ahead, behind, ok := parseAheadBehind(counts.Stdout)
	if !ok {
		status.State = VaultStatusRemoteUnavailable
		return status
	}
	status.State = VaultStatusOK
	status.Ahead = ahead
	status.Behind = behind
	return status
}

func countPorcelainEntries(output []byte) int {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func parseAheadBehind(output []byte) (int, int, bool) {
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return 0, 0, false
	}
	ahead, aheadErr := strconv.Atoi(fields[0])
	behind, behindErr := strconv.Atoi(fields[1])
	if aheadErr != nil || behindErr != nil || ahead < 0 || behind < 0 {
		return 0, 0, false
	}
	return ahead, behind, true
}

func gitFetchEnvironment() []string {
	environment := os.Environ()
	const setting = "GIT_TERMINAL_PROMPT=0"
	for index, entry := range environment {
		if strings.HasPrefix(entry, "GIT_TERMINAL_PROMPT=") {
			environment[index] = setting
			return environment
		}
	}
	return append(environment, setting)
}
