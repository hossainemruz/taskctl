# taskctl

`taskctl` is a Go CLI for a structured Task → PR → Step agent workflow. It stores canonical lifecycle state as YAML in a dedicated, user-synchronized vault and keeps requirements, research, plans, and final-review findings in normal Markdown files.

`taskctl` discovers work from a repository's normalized Git `origin` and the current branch. It does not write any pointer into the project repository.

## Requirements and installation

- Go 1.26.5 or a compatible newer Go toolchain
- Git on `PATH` for project discovery and vault status
- Linux or macOS

Build or install from a source checkout:

```bash
go build -o bin/taskctl ./cmd/taskctl
# or install into GOBIN
go install ./cmd/taskctl
```

Verify the result with `taskctl version` and `taskctl --help`.

## Initialize a vault

Interactive setup prompts for a vault path and viewer executable:

```bash
taskctl init
```

For automation, provide all machine-local values explicitly. A typical Linux configuration is:

```bash
taskctl init \
  --vault "$HOME/agent-vault" \
  --viewer typora \
  --non-interactive
```

For Typora on macOS:

```bash
taskctl init \
  --vault "$HOME/agent-vault" \
  --viewer open \
  --viewer-arg=-a \
  --viewer-arg=Typora \
  --non-interactive
```

To adopt an existing non-empty directory that does not yet contain a
`taskctl.yaml` manifest, pass `--force`:

```bash
taskctl init \
  --vault "$HOME/agent-vault" \
  --viewer open \
  --viewer-arg=-a \
  --viewer-arg=Typora \
  --non-interactive \
  --force
```

Forced initialization preserves existing contents and templates. It does not
bypass validation of an existing manifest or conflicting vault paths.

The Task directory is appended as the final viewer argument. The executable and arguments are invoked directly, without shell interpretation.

Machine-local configuration is stored at:

- Linux: `$XDG_CONFIG_HOME/taskctl/config.yaml`, or `~/.config/taskctl/config.yaml` when `XDG_CONFIG_HOME` is unset.
- macOS: `~/Library/Application Support/taskctl/config.yaml`.

It contains an absolute vault path and the viewer argument vector:

```yaml
schema_version: 1
vault: /home/user/agent-vault
viewer:
  command: typora
  args: []
```

Initialization creates or validates this synchronized vault layout:

```text
<vault>/
  taskctl.yaml
  templates/
    task.md.tmpl
    research.md.tmpl
    plan.md.tmpl
    review.md.tmpl
  projects/
    <project-id>/
      project.yaml
      <task-id>/
        task.yaml
        task.md
        research.md   # lazy
        plan.md       # lazy
        review.md     # lazy
```

Re-running `init` validates an existing vault and installs only missing templates. It never overwrites customized templates, initializes Git, or configures a remote.

## Project identity and Task selection

The first `new` command registers the current project. HTTPS, SCP-style SSH, and `ssh://` forms of the same `origin` normalize to the same `host/path` identity. The project directory ID contains the ownership and repository path; for example, `git@github.com:acme/platform/api.git` becomes `acme_platform_api`.

Interactive creation suggests a Task prefix:

```bash
taskctl new "Deliver the MVP"
```

Non-interactive registration requires the prefix:

```bash
taskctl new "Deliver the MVP" --prefix API --non-interactive
```

If the repository has no usable portable remote, provide both explicit identity flags:

```bash
taskctl new "Offline project work" \
  --project-id acme_api \
  --repository example.com/acme/api \
  --prefix API \
  --non-interactive
```

The same `--project-id` and `--repository` pair is available on project-aware commands when running outside a usable Git worktree. A detected usable origin must match the explicit identity.

Task IDs are project-scoped and sequential, such as `API-001`. Synchronize the vault before running `new` on another device; concurrent unsynchronized Task creation is unsupported.

Select and inspect Tasks with:

```bash
taskctl task list
taskctl use API-001
taskctl task cancel API-002
```

`project.yaml` stores one synchronized `current_task` fallback. An exact PR branch association takes precedence over that fallback, so separate branches and worktrees continue to resolve their own Tasks. Stale or ambiguous state is an error; `taskctl` never chooses the first match.

On another device, synchronize or clone the vault yourself, run `taskctl init` with that device's vault path and viewer, and use a clone with the same normalized project remote. An unassociated branch resolves the synchronized `current_task`; an associated branch still takes precedence even when the checkout and vault use different filesystem paths.

## Human and agent workflow

### 1. Human: create the Task

`new` creates only `task.yaml` and `task.md` and makes the Task current:

```bash
taskctl new "Deliver the MVP" --prefix TASKCTL --non-interactive
taskctl artifact view
```

When research is needed, the research agent creates the artifact before writing its findings:

```bash
taskctl artifact ensure research
```

`artifact ensure` accepts `research`, `plan`, or `review`, prints the absolute path, and is idempotent. Existing prose is never overwritten. Use the read-only `path` command to locate an artifact that already exists:

```bash
taskctl path task
taskctl path research
```

### 2. Agent: research, write, and apply a structured plan

Create `plan.md`, then write detailed sections using exact headings:

```markdown
### PR-001: Implement storage

#### STEP-001: Define the schema

#### STEP-002: Add atomic persistence
```

```bash
taskctl artifact ensure plan
```

Register the matching hierarchy through standard input or `--file`:

```json
{
  "prs": [
    {
      "id": "PR-001",
      "title": "Implement storage",
      "steps": [
        {"id": "STEP-001", "title": "Define the schema"},
        {"id": "STEP-002", "title": "Add atomic persistence"}
      ]
    }
  ]
}
```

```bash
taskctl plan apply --file plan.json
# or: taskctl plan apply < plan.json
taskctl pr list
taskctl step list
```

Before execution starts, `plan apply` may replace the draft hierarchy. After a PR starts, only title corrections with unchanged IDs, order, and parentage are accepted. Append newly discovered work explicitly:

```bash
taskctl pr add --title "Document the workflow"       # prints the new PR ID
taskctl step add --pr PR-002 --title "Write examples" # prints the new Step ID
```

Add the returned heading and detailed prose to `plan.md`. Step IDs remain unique across the whole Task.

The generated block between `<!-- taskctl:progress:start -->` and `<!-- taskctl:progress:end -->` is replaced after lifecycle changes. Content outside those two root-level markers is preserved; do not put user-authored prose inside the generated block.

### 3. Human and agent: execute and review a PR

Create or check out the branch with your normal Git workflow, then record it:

```bash
git switch -c feature/storage
taskctl pr start PR-001
```

`pr start` only reads and records the current named branch. It does not create, rename, validate, rebase, or otherwise manage branches.

The normal Step review loop is:

```bash
taskctl step get          # agent-facing JSON for the selected Step
taskctl step start
# implement, validate, and perform automated review
taskctl step submit       # ready for user review
taskctl step revise       # after user feedback; returns to implementation
taskctl step submit
taskctl step complete     # only after explicit user acceptance
```

An explicit Step ID may be supplied to lifecycle commands. When omitted, the current PR's sole active Step or first pending Step is selected. At most one Step per PR may be `in_progress` or `ready_for_review`.

Remove unneeded work with a required reason:

```bash
taskctl step skip STEP-002 --reason "superseded"
taskctl pr skip PR-002 --reason "deferred"
taskctl step reopen STEP-002
```

Task and PR status is derived from PR/Step state. Completing or skipping all work completes the aggregate; adding or reopening incomplete work reopens it.

### 4. Agent: record optional final review

Only the latest final PR review is retained in the Task-level `review.md`:

```bash
taskctl artifact ensure review
taskctl path review
```

Replace its prose with the latest final review and identify the reviewed PR and branch. If the review has actionable findings, add a corrective Step and append its detailed heading to `plan.md`:

```bash
taskctl step add --pr PR-001 --title "Address final review findings"
```

The new pending Step automatically reopens the PR and Task. Automated incremental Step reviews and user feedback are intentionally not persisted as separate artifacts.

### 5. Human: inspect status and synchronize the vault

```bash
taskctl status
taskctl vault status
```

`status` returns pretty-printed JSON containing the selected Task, ordered PRs and Steps, active work, skip reasons, existing artifacts, and vault Git state. These are the only two commands that run `git fetch --quiet` in the vault. They then report dirty, ahead, and behind state. Fetch failure is represented by the vault state and does not make Task status fail.

`taskctl` never stages, commits, pulls, rebases, pushes, initializes, or configures the vault repository. The user owns vault synchronization and must resolve Git conflicts.

## Agent commands and JSON contracts

`taskctl context` returns the selected Task and sparse artifact paths:

```json
{
  "project_id": "acme_api",
  "task_id": "API-001",
  "title": "Add API authentication",
  "status": "in_progress",
  "progress": {
    "completed": 0,
    "skipped": 0,
    "total": 2
  },
  "current_pr": {
    "id": "PR-001",
    "status": "in_progress",
    "progress": {
      "completed": 1,
      "skipped": 0,
      "total": 2
    },
    "active_step": {
      "id": "STEP-002",
      "status": "ready_for_review"
    }
  },
  "artifacts": {
    "task": "/vault/projects/acme_api/API-001/task.md",
    "plan": "/vault/projects/acme_api/API-001/plan.md"
  }
}
```

Top-level progress counts PRs; `current_pr.progress` counts Steps. `current_pr` appears only when the checked-out branch is associated by `pr start`, and `active_step` appears only for `in_progress` or `ready_for_review`. Missing optional artifacts are omitted. `taskctl status` expands this context with all PRs and Steps plus a structured `vault` object.

`taskctl step get` requires a branch-associated current PR and returns only the selected Step and existing artifacts:

```json
{
  "task_id": "API-001",
  "pr_id": "PR-001",
  "step_id": "STEP-002",
  "status": "pending",
  "artifacts": {
    "task": "/vault/projects/acme_api/API-001/task.md",
    "research": "/vault/projects/acme_api/API-001/research.md",
    "plan": "/vault/projects/acme_api/API-001/plan.md"
  }
}
```

Agent-facing JSON is pretty-printed to standard output only. Diagnostics go to standard error. `context` and `status` always return JSON; `pr list --json` and `step list --json` provide ordered list forms for automation.

## Templates and state integrity

Vault templates are standard Go `text/template` files. They receive `.TaskID`, `.Title`, `.ProjectID`, and `.CreatedAt`. Markdown artifacts contain no YAML frontmatter.

`task.yaml` is canonical for lifecycle state. YAML manifests are strictly decoded, schema-versioned, and atomically replaced. Unknown fields, unsupported versions, malformed state, duplicate identities, and ambiguous associations are rejected without guessing or rewriting the invalid source.

Lifecycle operations prepare the generated `plan.md` projection before saving. If canonical state is saved but the final Markdown replacement fails, the command returns exit code 8 with remediation text; a later lifecycle write can regenerate the projection. Cross-process concurrent writers are unsupported.

## Exit codes

| Code | Category |
| ---: | --- |
| 0 | Success |
| 1 | Internal or local I/O failure |
| 2 | Invalid command usage or arguments |
| 3 | Missing initialization or current context |
| 4 | Requested state not found |
| 5 | Invalid transition or conflicting state |
| 6 | Corrupt, incompatible, or unsupported persisted data |
| 7 | External command failure |
| 8 | Canonical state saved but Markdown projection failed |

Operational failures do not print Cobra usage text.

## Non-goals

`taskctl` does not manage project branches, worktrees, commits, hosted pull requests, provider APIs, remote merge state, or vault synchronization. It does not persist automated Step-review artifacts. Windows support, Task deletion, archival, multi-vault selection, and automatic migration of newer schemas are outside the MVP.

The full design and invariants are documented in [`docs/rfc.md`](docs/rfc.md).
