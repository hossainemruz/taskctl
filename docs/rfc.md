# Task Management CLI

## Status

Implemented.

## Background

The current task-management workflow uses a dedicated Obsidian vault for task
artifacts. A task may contain `task.md`, `research.md`, `plan.md`, and
`review.md`. OpenCode commands discover the active artifact directory through a
manually maintained `.agent-task` file in each project repository.

The workflow is effective, but it has several sources of friction:

- Creating tasks and artifacts depends on Obsidian and an Obsidian plugin.
- `.agent-task` must be maintained manually and does not work well across
  branches, worktrees, or devices.
- Agents must read several artifacts, often in full, merely to discover task
  state.
- Task state is encoded in mutable Markdown status fields and is therefore
  expensive and error-prone to inspect or update.

The artifact vault is dedicated to this workflow, viewed primarily with Typora,
and synchronized across devices using Git.

## Goals

- Remove the dependency on Obsidian.
- Remove repository-local task pointer files such as `.agent-task`.
- Make task discovery and lifecycle updates deterministic and token-efficient
  for agents.
- Keep artifacts as normal Markdown files that can be viewed with Typora or
  another configured application.
- Support one task spanning multiple PR branches.
- Support multiple incomplete tasks in the same project.
- Make current-task selection portable through the Git-synchronized vault.
- Support Linux and macOS. Windows is out of scope.

## Non-goals

`taskctl` will not:

- Create, move, remove, or prune Git worktrees.
- Create, rename, validate, rebase, or stack project branches.
- Track or manage actual Git commits.
- Create hosted pull requests or track their remote merge status.
- Synchronize the artifact vault with Git.
- Integrate with GitHub, GitLab, or another hosting-provider API.
- Persist automated Step-review findings.

## Domain Model

The planning and execution hierarchy is:

```text
Task
└── PR
    ├── Step
    ├── Step
    └── Step
```

### Task

A Task represents an overall outcome for one project. It may span multiple PRs
and branches.

Task IDs are sequential and project-scoped, for example `TASKCTL-001`. A
project has a configurable ID prefix. The default prefix is derived from the
repository name.

### PR

A PR is a planned, reviewable delivery unit. Each started PR is associated with
one user-created Git branch. `taskctl` records that branch but does not manage
it.

PR IDs are sequential within a Task, for example `PR-001`.

### Step

A Step is an atomic implementation and incremental-review unit within a PR. A
Step may result in zero, one, or multiple actual Git commits. Git commits have
no identity or lifecycle relationship with `taskctl`.

Step IDs are unique across the whole Task rather than restarting within each
PR:

```text
PR-001: STEP-001, STEP-002
PR-002: STEP-003, STEP-004
```

## Source of Truth

Each Task directory contains a machine-readable `task.yaml`. It is the
authoritative source for:

- Task identity and lifecycle metadata.
- Ordered PR and Step structure.
- PR branch associations.
- Step lifecycle state.
- Skip reasons and other operational metadata.

Markdown artifacts are authoritative for prose such as requirements, research,
implementation details, and review findings. They are not authoritative for
lifecycle state.

The project registry stores project metadata and current-task selection. It
does not duplicate task status or maintain an index of task artifact paths.
`taskctl` discovers Tasks by scanning the small number of manifests under the
current project.

## Vault Layout

The vault is dedicated to `taskctl` and uses the following layout:

```text
<vault>/
  taskctl.yaml
  templates/
    task.md.tmpl
    research.md.tmpl
    plan.md.tmpl
    review.md.tmpl
  projects/
    hossainemruz_taskctl/
      project.yaml
      TASKCTL-001/
        task.yaml
        task.md
        research.md
        plan.md
        review.md
      TASKCTL-002/
        task.yaml
        task.md
```

`taskctl.yaml` contains vault schema and vault-level configuration.
`project.yaml` contains project identity, the Task ID prefix, and the current
Task ID.

Task artifacts are created lazily:

- `taskctl new` creates `task.yaml` and `task.md`.
- Research creates `research.md` when needed.
- Planning creates `plan.md` when needed.
- Final PR review creates `review.md` when needed.

Markdown artifacts do not contain YAML frontmatter. Relevant operational
metadata already exists in `task.yaml`, and review verdicts remain ordinary
Markdown content.

## Configuration

Configuration is divided into machine-local and synchronized state.

### Machine-local configuration

Machine-local configuration is stored in the platform-appropriate user
configuration directory. It contains:

- The local vault path.
- The preferred Markdown viewer command and arguments.

Example:

```yaml
vault: /home/user/agent-vault
viewer:
  command: typora
  args: []
```

macOS example:

```yaml
viewer:
  command: open
  args: ["-a", "Typora"]
```

The Task directory is appended as the final viewer argument. Viewer commands
are executed directly without shell interpretation.

### Synchronized configuration

The root `taskctl.yaml`, project registries, templates, Task manifests, and
Markdown artifacts are stored in the vault and synchronized by the user.

## Templates

The binary embeds default Go templates. Initializing a new vault copies those
defaults into `<vault>/templates/`. The vault templates are then user-owned,
synchronized, and editable.

Upgrading `taskctl` must not silently overwrite customized templates.

Templates receive creation-time values such as Task ID, title, project, and
date. They contain no lifecycle frontmatter.

## Project Identity and Registration

`taskctl` derives a human-readable project ID from the Git `origin` remote by
joining the ownership path and repository name with underscores:

```text
git@github.com:hossainemruz/taskctl.git
  -> hossainemruz_taskctl
```

For nested ownership paths, all ownership segments are included:

```text
gitlab.com/org/team/project
  -> org_team_project
```

The concise project ID is used as the vault directory name. `project.yaml`
also stores the full normalized remote identity, including the host, to prevent
accidental collisions and validate that the current clone represents the same
project across devices.

SSH and HTTPS forms of the same remote normalize to the same identity. If a
repository has no usable remote, the user must provide an explicit project ID
and repository identity; machine-local filesystem paths are not portable
identities.

The first `taskctl new` in an unregistered project creates `project.yaml`,
suggests a Task ID prefix derived from the repository name, and permits the
user to override it interactively or with a flag.

## Current Task and PR Resolution

A project may have multiple incomplete Tasks, but `project.yaml` contains one
synchronized fallback `current_task`.

`taskctl` resolves context in this order:

1. Resolve the project from the current repository's normalized remote.
2. If the checked-out branch is associated with exactly one PR, use that PR's
   Task.
3. Otherwise, use the project's `current_task`.
4. If neither produces a Task, return an actionable error instructing the user
   to run `taskctl new` or `taskctl use`.

Branch association takes precedence over project-level selection so that PR
branches in different worktrees continue to resolve to their own Tasks.
Ambiguous branch associations are an integrity error and must never be resolved
silently.

`taskctl use <task-id>` updates the synchronized project-level current Task. No
pointer is written into the project repository or its Git administrative
directory.

## Lifecycle

### Task lifecycle

- `draft`: execution has not started and the Task has no plan or still has a
  pending PR. Research and planning happen while the Task is in this state,
  including after a structured plan is applied.
- `in_progress`: at least one PR has started and at least one PR remains
  incomplete.
- `completed`: the plan contains at least one PR and all PRs are completed or
  skipped.
- `cancelled`: the user explicitly abandoned the Task.

Task status is derived from PR state after execution starts. Adding or reopening
incomplete work reopens a completed Task.

### PR lifecycle

- `pending`: the PR has not started.
- `in_progress`: the PR has started and at least one Step is incomplete.
- `completed`: all Steps are completed or skipped.
- `skipped`: the user explicitly removed the PR from scope and supplied a
  reason.

`taskctl pr start <pr-id>` requires a named current branch, records that branch,
and starts the PR. It does not create the branch, choose its base, or validate
its name. A branch cannot be associated with multiple PRs in the same project.

Remote PR merge state is intentionally not represented. A PR is completed when
its planned changes have been incrementally accepted. Optional final review may
subsequently reopen it by adding a corrective Step.

### Step lifecycle

The normal lifecycle is:

```text
pending -> in_progress -> ready_for_review -> completed
                  ^              |
                  +--------------+
```

- `pending`: implementation has not started.
- `in_progress`: implementation or feedback-driven revision is underway.
- `ready_for_review`: implementation, validation, and automated review are
  complete; the Step awaits user review.
- `completed`: the user accepted the Step.
- `skipped`: the user explicitly removed the Step from scope and supplied a
  reason.

A completed or skipped Step may be reopened. There is no `blocked` state in the
initial design.

At most one Step per PR may be `in_progress` or `ready_for_review`. A new Step
cannot start until the previous active Step is completed or skipped. Different
PRs may be in progress simultaneously. Step lifecycle commands require their PR
to have started; an unneeded pending PR is skipped at PR level.

Agents normally execute lifecycle commands. The user communicates acceptance or
feedback to the agent, and the agent records the corresponding transition.

## Planning

The planner writes detailed prose to `plan.md` and registers the operational PR
and Step hierarchy in one JSON operation. The structured input contains only
IDs, titles, ordering, and parent-child relationships:

```json
{
  "prs": [
    {
      "id": "PR-001",
      "title": "Implement manifest storage",
      "steps": [
        { "id": "STEP-001", "title": "Define the manifest schema" },
        { "id": "STEP-002", "title": "Implement persistence" }
      ]
    }
  ]
}
```

`taskctl` does not infer hierarchy or status from arbitrary Markdown. The plan
uses stable IDs in headings so agents can correlate structured state with prose:

```markdown
### PR-001: Implement manifest storage

#### STEP-001: Define the manifest schema
```

Applying a plan requires at least one PR and at least one Step per PR. It
validates ID uniqueness and correspondence with `plan.md` but does not move the
Task out of `draft`.

While the Task remains `draft`, `taskctl plan apply` may replace the entire
structured hierarchy. After any PR starts:

- Bulk replacement is rejected.
- New PRs and Steps may be appended explicitly.
- Titles and prose may be corrected.
- Started or completed items cannot be deleted; they may be skipped with a
  reason.
- Adding or reopening incomplete work recalculates aggregate status.

Step numbering remains Task-wide when later Steps are appended.

## Markdown Progress Projection

`task.yaml` is the only source of lifecycle state. To keep progress visible in
Typora, `taskctl` maintains one generated block in `plan.md`:

```markdown
## Progress

<!-- taskctl:progress:start -->

- PR-001: Manifest storage — In Progress
  - STEP-001: Define schema — Completed
  - STEP-002: Implement persistence — Ready for Review

<!-- taskctl:progress:end -->
```

Lifecycle changes refresh this block. Content inside the markers is generated
and may be overwritten. Detailed PR and Step sections do not contain separate
mutable status fields.

## Review Workflow

Step review is incremental:

1. The implementation agent implements and validates one Step.
2. It performs self-review or invokes an automated reviewer and addresses
   accepted findings.
3. It marks the Step `ready_for_review`.
4. The user reviews the change and provides feedback directly to the agent,
   usually with file and line references.
5. Feedback returns the Step to `in_progress`; acceptance completes it.

Automated Step reviews and user feedback are not persisted as review artifacts.

After all Steps are accepted, the PR completes automatically. The user may
optionally invoke an expert final review. The latest final PR review is written
to a single, lazily created task-level `review.md`, replacing the previous final
review content and identifying the reviewed PR and branch.

If final review produces actionable findings, the reviewer:

1. Writes `review.md`.
2. Adds a new Step such as "Address final review findings" to the reviewed PR.
3. Appends the corresponding detailed Step section to `plan.md`, referencing
   `review.md`.

The new pending Step automatically reopens the PR and Task. If final review has
no actionable findings, lifecycle state remains unchanged.

## Agent-facing Output

### `taskctl context`

`context` returns pretty-printed JSON suitable for agents and still readable by
users:

```json
{
  "project_id": "hossainemruz_taskctl",
  "task_id": "TASKCTL-001",
  "status": "in_progress",
  "progress": {
    "completed": 2,
    "skipped": 0,
    "total": 5
  },
  "current_pr": {
    "id": "PR-001",
    "status": "in_progress",
    "progress": {
      "completed": 1,
      "skipped": 0,
      "total": 3
    },
    "active_step": {
      "id": "STEP-002",
      "status": "ready_for_review"
    }
  },
  "artifacts": {
    "task": "/vault/projects/.../task.md",
    "research": "/vault/projects/.../research.md",
    "plan": "/vault/projects/.../plan.md",
    "review": "/vault/projects/.../review.md"
  }
}
```

Only existing artifacts are included. `current_pr` appears only when the
current branch is associated with a PR. `active_step` appears only when a Step
is `in_progress` or `ready_for_review`.

Top-level progress counts PRs, while `current_pr.progress` counts that PR's
Steps. Completed and skipped counts are reported separately. Aggregate
completion uses their sum, so a completed Task with skipped work does not
misleadingly appear incomplete.

### `taskctl step get`

`step get` is an agent-oriented JSON command. Within the current PR, it returns
the single `in_progress` or `ready_for_review` Step if one exists; otherwise it
returns the first pending Step.

The command requires a branch-associated current PR. It fails explicitly when
the current branch has not been registered with `taskctl pr start`.

It returns only stable identifiers, status, and existing artifact paths:

```json
{
  "task_id": "TASKCTL-001",
  "pr_id": "PR-001",
  "step_id": "STEP-002",
  "status": "pending",
  "artifacts": {
    "task": "/vault/projects/.../task.md",
    "research": "/vault/projects/.../research.md",
    "plan": "/vault/projects/.../plan.md",
    "review": "/vault/projects/.../review.md"
  }
}
```

It does not return implementation prose or a recommended action. The agent reads
the artifacts it needs and uses the complete plan to understand dependencies,
later-stage assumptions, and already completed work.

## Vault Git Status

Vault synchronization is the user's responsibility. `taskctl` provides status
only.

`taskctl vault status` and the human-facing `taskctl status` command invoke the
installed Git CLI to:

1. Run `git fetch --quiet` in the vault.
2. Inspect uncommitted changes.
3. Compare `HEAD` with its configured upstream.
4. Report a compact clean/dirty and ahead/behind summary.

No other command fetches remote state. If fetch or remote inspection fails, the
main Task status still succeeds and displays a minimal warning such as:

```text
Vault: 2 uncommitted files · remote status unavailable
```

`taskctl` does not stage, commit, rebase, pull, push, or configure vault Git
state.

## Recommended Commands

### Setup and Task selection

```text
taskctl init
taskctl new "Task title"
taskctl use <task-id>
taskctl task list
taskctl task cancel [task-id]
```

`init` selects or creates a vault, installs default templates for a new vault,
and configures the machine-local vault path and viewer. It does not initialize
Git or configure remotes.

`new` resolves or registers the current project, allocates the next Task ID,
creates `task.yaml` and `task.md`, and makes the Task current.

### Context and artifacts

```text
taskctl context
taskctl status
taskctl path <task|research|plan|review>
taskctl artifact ensure <research|plan|review>
taskctl artifact view
```

`artifact ensure` is idempotent: it renders a missing artifact from its template
and never overwrites an existing file. `path` is read-only and fails if the
requested artifact does not exist. `artifact view` opens the current Task
directory with the configured viewer.

### Planning and PRs

```text
taskctl plan apply
taskctl pr list
taskctl pr start <pr-id>
taskctl pr add
taskctl pr skip <pr-id> --reason "..."
```

`plan apply` accepts the structured JSON hierarchy through a file or standard
input. `pr add` supports explicit plan evolution after work has started.

### Steps

```text
taskctl step list
taskctl step get
taskctl step add --pr <pr-id> --title "..."
taskctl step start [step-id]
taskctl step submit [step-id]
taskctl step revise [step-id]
taskctl step complete [step-id]
taskctl step skip [step-id] --reason "..."
taskctl step reopen [step-id]
```

Lifecycle commands validate allowed transitions and refresh the generated plan
progress block.

### Vault

```text
taskctl vault status
```

## Integrity and Error Handling

- Manifest and registry writes must be atomic.
- Invalid or ambiguous project, Task, branch, PR, or Step resolution fails
  explicitly; `taskctl` never guesses.
- Aggregate PR and Task state is calculated rather than independently updated.
- Agent-facing commands use stable JSON and meaningful nonzero exit codes.
- User-facing failures are concise and actionable.
- A vault remote-status failure is a warning and does not fail an otherwise
  successful Task status command.
- Newer unsupported schema versions are rejected rather than modified.

## Success Criteria

The design succeeds when a user can:

1. Create and manage task artifacts without Obsidian.
2. Select a Task once in the synchronized vault and resolve it on another
   device after vault synchronization.
3. Use separate PR branches and worktrees without repository-local pointer
   files.
4. Let agents discover current state and artifacts through small JSON responses.
5. Review all prose and generated progress with Typora.
6. Preserve control of Git branches, worktrees, commits, remote PRs, and vault
   synchronization outside `taskctl`.
