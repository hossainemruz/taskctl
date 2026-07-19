# Implementation Plan: taskctl MVP

## Objective

Implement the `taskctl` MVP described in [`docs/rfc.md`](rfc.md): a Go CLI that
stores structured task state in a dedicated, Git-synchronized vault while
keeping requirements, research, plans, and final-review findings in normal
Markdown artifacts.

The implementation must keep task-state transitions deterministic, give agents
small stable JSON responses, preserve user control over project Git workflows,
and run on Linux and macOS.

## Requirements Snapshot

- **R1 — Vault bootstrap:** `taskctl init` configures a machine-local vault path
  and viewer, creates the dedicated vault layout when needed, and installs
  editable default templates without configuring Git.
- **R2 — Portable project identity:** Resolve projects from normalized Git
  remotes, use `<ownership>_<repository>` as the project ID, retain the full
  normalized remote for validation, and support explicit identity flags when no
  usable remote exists.
- **R3 — Synced task selection:** Store one fallback `current_task` in
  `project.yaml`; resolve an exactly matched PR branch before that fallback and
  never write a pointer into a project repository or Git directory.
- **R4 — Human Task IDs:** Generate configurable, project-scoped sequential Task
  IDs such as `TASKCTL-001`.
- **R5 — Canonical structured state:** Store Task, PR, and Step operational state
  in versioned YAML manifests. Derive aggregate Task and PR status instead of
  independently persisting it.
- **R6 — Task → PR → Step lifecycle:** Support Task, PR, and Step state and
  invariants from the RFC, including one active Step per PR, explicit user
  acceptance, skip reasons, reopening, and automatic aggregate reopening.
- **R7 — User-owned Git topology:** Capture a named current branch on
  `taskctl pr start`; do not create, validate, rebase, stack, or otherwise
  manage project branches, worktrees, hosted PRs, or Git commits.
- **R8 — Structured planning:** Apply the initial PR/Step hierarchy from JSON,
  validate stable Markdown headings, permit full replacement only before work
  starts, and support safe append-only plan evolution afterward.
- **R9 — Markdown projection:** Keep prose in frontmatter-free Markdown and
  render lifecycle status only into a generated, bounded Progress block in
  `plan.md`.
- **R10 — Lazy artifacts:** Create only `task.yaml` and `task.md` for a new Task;
  create research, plan, and review artifacts idempotently from templates when
  requested.
- **R11 — Agent discovery:** Provide the stable JSON contracts for
  `taskctl context` and `taskctl step get`, returning only existing artifact
  paths and the state defined by the RFC.
- **R12 — Human status:** Provide detailed Task/PR/Step status and progress with
  a concise vault Git footer.
- **R13 — Final-review workflow:** Keep automated Step review transient, retain
  only the latest final PR review in `review.md`, and allow review findings to
  append a corrective Step that reopens aggregate state.
- **R14 — Vault Git visibility:** Use the installed Git CLI to fetch and report
  vault dirty/ahead/behind state only from `status` and `vault status`; never
  stage, commit, pull, rebase, or push.
- **R15 — Integrity:** Use atomic manifest writes, strict schema validation,
  explicit ambiguity and transition errors, stable JSON, and meaningful exit
  codes.
- **R16 — Portability:** Keep filesystem and process behavior portable across
  Linux and macOS and verify both build targets.

## Scope

### In scope

- The complete MVP command surface approved in the RFC.
- Local configuration, synchronized vault configuration, templates, manifests,
  and artifact files.
- Project and current-context resolution through Git CLI inspection plus vault
  state.
- Pure lifecycle and aggregate-status rules with comprehensive tests.
- Human text output and agent JSON output.
- Documentation for installation and the intended human/agent workflows.

### Out of scope

- Vault synchronization or Git mutation beyond `git fetch` for status.
- Project branch/worktree creation, naming enforcement, stacking, rebasing, or
  cleanup.
- Actual Git commit tracking.
- Hosted pull-request creation, provider APIs, or merge tracking.
- Persistent Step-review artifacts.
- Windows support.
- Task deletion, archival, or multi-vault selection.
- Automatic migration of unknown/newer manifest schemas in the MVP.

## Assumptions and Constraints

- The current Go toolchain is Go 1.26.5, matching `go.mod`.
- Git is installed for project-aware commands and vault remote status.
- Users synchronize the vault before allocating Task IDs on another device;
  concurrent unsynchronized Task creation can otherwise choose the same
  sequential ID.
- Commands other than `init` and vault-only inspection run from a project Git
  worktree unless explicitly given enough project identity information.
- A PR must contain at least one Step before it can start.
- A plan uses exact headings `### PR-NNN: Title` and
  `#### STEP-NNN: Title` for registered items.
- `task.yaml` remains canonical if writing its generated Markdown projection
  fails. The command reports the partial failure and a later lifecycle write can
  regenerate the projection.
- Cross-process locking is not required initially. Atomic replacement prevents
  corrupt files; concurrent writers remain unsupported and should be documented.

## Selected Technical Approach

### Dependencies

- Use `github.com/spf13/cobra` for nested commands, flag validation, help, and
  consistent usage handling.
- Use the stable `go.yaml.in/yaml/v3` line for YAML. As of July 2026, v4 is still
  a release candidate; do not base the first stable storage schema on a
  prerelease dependency.
- Use the standard library for JSON, templates, embedded files, filesystem
  access, process execution, timestamps, and terminal detection where possible.
- Do not add Viper, a Markdown parser, a Git library, or a filesystem abstraction.

### Module design

Keep behavior behind a small set of deep modules:

```text
cmd/taskctl/          Process entry point only
internal/cli/         Cobra commands, argument parsing, and output formatting
internal/app/         Workflow orchestration and command result DTOs
internal/domain/      Pure Task/PR/Step model, derivation, and transitions
internal/config/      Machine-local configuration
internal/vault/       Layout, YAML persistence, scans, artifacts, and atomic writes
internal/gitcli/      Git command execution and normalized repository facts
internal/plan/        Plan JSON validation and bounded Markdown operations
internal/process/     Small external-process seam used by Git and viewer launch
```

Design rules:

- Cobra callbacks call `internal/app`; they do not read manifests or implement
  lifecycle rules directly.
- `internal/domain` has no filesystem, Git, Cobra, or rendering dependencies.
- `internal/vault` exposes task-oriented persistence operations rather than raw
  path manipulation to every caller.
- Use real temporary directories in persistence tests. Introduce interfaces only
  at the actual process-execution seam, where production and fake adapters both
  exist.
- Return typed results to the CLI; render JSON or human text only at the outer
  seam.

### Initial persisted schemas

Use explicit `schema_version: 1` in local config, `taskctl.yaml`,
`project.yaml`, and `task.yaml`. Decode with known-field validation and reject
unsupported versions.

Conceptual `project.yaml`:

```yaml
schema_version: 1
id: hossainemruz_taskctl
repository: github.com/hossainemruz/taskctl
task_prefix: TASKCTL
current_task: TASKCTL-001
```

Conceptual `task.yaml`:

```yaml
schema_version: 1
id: TASKCTL-001
title: Implement taskctl
project_id: hossainemruz_taskctl
created_at: 2026-07-19T12:00:00Z
cancelled_at: null
prs:
  - id: PR-001
    title: Add storage
    branch: feat/emruz/task-storage
    started_at: 2026-07-20T12:00:00Z
    skipped_at: null
    skip_reason: ""
    steps:
      - id: STEP-001
        title: Define schemas
        status: completed
        skip_reason: ""
```

Do not persist derived Task or PR status. Preserve PR and Step order using YAML
sequence order.

### Atomic persistence

For each YAML or generated Markdown write:

1. Encode and validate the complete new content in memory.
2. Create a temporary file in the destination directory.
3. Write, flush, and close it.
4. Preserve the intended file mode.
5. Rename it over the destination atomically.

Lifecycle workflows prepare and validate both canonical state and the progress
projection before writing. Persist `task.yaml` first because it is canonical,
then replace the generated Progress block. If projection replacement fails,
return a typed partial-update error without rolling back canonical state.

### Error and output contract

Define typed error categories and map them at the CLI seam:

- Usage/invalid arguments.
- Missing initialization or context.
- Not found.
- Invalid transition or conflicting state.
- Corrupt/unsupported persisted data.
- External command failure.
- Partial projection failure.

JSON commands write JSON only to stdout. Diagnostics and warnings go to stderr.
Operational failures should not print Cobra usage. Establish and document stable
nonzero exit codes during the CLI foundation PR.

## Proposed Package and File Layout

The exact file split may evolve while preserving module ownership:

```text
cmd/taskctl/main.go

internal/cli/root.go
internal/cli/init.go
internal/cli/task.go
internal/cli/context.go
internal/cli/artifact.go
internal/cli/plan.go
internal/cli/pr.go
internal/cli/step.go
internal/cli/vault.go
internal/cli/output.go

internal/app/app.go
internal/app/task.go
internal/app/context.go
internal/app/artifact.go
internal/app/plan.go
internal/app/execution.go
internal/app/status.go

internal/domain/model.go
internal/domain/status.go
internal/domain/transition.go
internal/domain/plan.go
internal/domain/id.go

internal/config/config.go
internal/vault/vault.go
internal/vault/layout.go
internal/vault/store.go
internal/vault/artifact.go
internal/vault/atomic.go
internal/gitcli/client.go
internal/gitcli/identity.go
internal/gitcli/status.go
internal/plan/input.go
internal/plan/headings.go
internal/plan/progress.go
internal/process/runner.go

internal/templates/defaults/task.md.tmpl
internal/templates/defaults/research.md.tmpl
internal/templates/defaults/plan.md.tmpl
internal/templates/defaults/review.md.tmpl
```

## Progress

- PR-001: Establish CLI and vault bootstrap
  - [x] **Status: Completed** — STEP-001: Add CLI entry point and error/output contract
  - [x] **Status: Completed** — STEP-002: Implement machine-local configuration
  - [x] **Status: Completed** — STEP-003: Implement vault initialization and embedded templates
- PR-002: Implement the pure domain model
  - [x] **Status: Completed** — STEP-004: Define versioned Task, PR, and Step models
  - [x] **Status: Completed** — STEP-005: Implement derived status and progress calculations
  - [x] **Status: Completed** — STEP-006: Implement lifecycle transitions and invariants
  - [x] **Status: Completed** — STEP-007: Implement ID allocation and structured-plan validation
- PR-003: Implement persistence and Git-backed context facts
  - [x] **Status: Completed** — STEP-008: Add atomic YAML stores and project scans
  - [x] **Status: Completed** — STEP-009: Add Git process adapter and remote normalization
  - [x] **Status: Completed** — STEP-010: Implement project registration and context resolution
- PR-004: Deliver Task and artifact workflows
  - [x] **Status: Completed** — STEP-011: Implement new, use, list, and cancel workflows
  - [x] **Status: Completed** — STEP-012: Implement lazy artifact ensure and path lookup
  - [x] **Status: Completed** — STEP-013: Implement configured viewer launch
  - [x] **Status: Completed** — STEP-014: Add Task/artifact CLI integration coverage
- PR-005: Deliver structured planning and Markdown projection
  - [x] **Status: Completed** — STEP-015: Implement plan input and heading validation
  - [x] **Status: Completed** — STEP-016: Implement bounded Progress rendering
  - [x] **Status: Completed** — STEP-017: Implement plan apply and safe metadata correction
  - [x] **Status: Completed** — STEP-018: Implement append-only PR/Step evolution and list commands
- PR-006: Deliver PR and Step execution lifecycle
  - [ ] **Status: Pending** — STEP-019: Implement PR start and branch association
  - [ ] **Status: Pending** — STEP-020: Implement Step selection and `step get` JSON
  - [ ] **Status: Pending** — STEP-021: Implement Step lifecycle commands
  - [ ] **Status: Pending** — STEP-022: Verify incremental review and reopening workflows
- PR-007: Deliver context, human status, and vault Git status
  - [ ] **Status: Pending** — STEP-023: Implement the `context` JSON contract
  - [ ] **Status: Pending** — STEP-024: Implement detailed human Task status
  - [ ] **Status: Pending** — STEP-025: Implement fresh vault Git status
  - [ ] **Status: Pending** — STEP-026: Verify branch precedence and cross-device fallback
- PR-008: Harden and document the MVP
  - [ ] **Status: Pending** — STEP-027: Add corruption, ambiguity, and failure-path coverage
  - [ ] **Status: Pending** — STEP-028: Add full CLI workflow tests
  - [ ] **Status: Pending** — STEP-029: Document installation and human/agent workflows
  - [ ] **Status: Pending** — STEP-030: Run final Linux/macOS validation and release review

## PR Breakdown

### PR-001: Establish CLI and vault bootstrap

- **Objective:** Produce an installable CLI with stable command wiring, local
  configuration, and safe initialization of a new or existing dedicated vault.
- **Related requirements:** R1, R9, R10, R15, R16.
- **Dependencies:** None.
- **Review scope:** CLI process behavior, config path portability, idempotent
  initialization, and generated default artifacts only.
- **Expected files/areas:** `cmd/taskctl`, `internal/cli`, `internal/config`,
  initial `internal/vault`, embedded templates, `go.mod`, and `go.sum`.
- **In scope:** Cobra root, dependency wiring, output/error conventions, local
  config, `init`, root vault schema, root directories, default templates, and
  new-vault/existing-vault behavior.
- **Out of scope:** Project registration, Task manifests, Git inspection, and all
  execution commands.
- **Risks:** Accidentally overwriting customized templates; platform-specific
  config paths; interactive prompts that make automation impossible.
- **Implementation guidance:** Keep prompts in the CLI seam and expose flags for
  all required values. Make initialization idempotent and distinguish an empty
  directory from an incompatible existing vault.
- **PR validation:** `go test ./...`, `go vet ./...`, build the binary, initialize
  fresh and pre-existing temp vaults, and verify no Git command is invoked.
- **Done when:** A user can configure a local vault and viewer, initialize the
  RFC layout, rerun initialization safely, and inspect useful help/errors.

#### STEP-001: Add CLI entry point and error/output contract

- **Status:** Completed
- **Purpose:** Establish the outer interface before adding domain behavior.
- **Related requirements:** R15, R16.
- **Changes:**
  - Add Cobra root construction and the `cmd/taskctl/main.go` entry point.
  - Inject stdin, stdout, stderr, environment, and process dependencies rather
    than reading globals throughout commands.
  - Define typed CLI error categories and stable exit-code mapping.
  - Ensure operational errors do not print usage and JSON output can remain
    uncontaminated.
- **Validation:** Unit-test error mapping and output routing; run `go build` and
  root help/version smoke tests.
- **Review notes:** Reject business logic in Cobra callbacks and avoid command
  globals that prevent parallel tests.

#### STEP-002: Implement machine-local configuration

- **Status:** Completed
- **Purpose:** Persist per-device vault and viewer settings portably.
- **Related requirements:** R1, R16.
- **Changes:**
  - Resolve the platform user-config directory, honoring normal Go/XDG behavior.
  - Define and strictly decode versioned local YAML configuration.
  - Implement atomic config load/save and actionable missing/invalid errors.
  - Represent viewer command and argument arrays without shell strings.
- **Validation:** Table-test Linux-style/XDG and macOS path resolution using
  injected environment/home values; round-trip and malformed-schema tests.
- **Review notes:** Do not add Viper or embed absolute development-machine paths.

#### STEP-003: Implement vault initialization and embedded templates

- **Status:** Completed
- **Purpose:** Replace Obsidian-based bootstrap with a safe CLI workflow.
- **Related requirements:** R1, R9, R10.
- **Changes:**
  - Embed frontmatter-free Task, research, plan, and review templates.
  - Create `taskctl.yaml`, `templates/`, and `projects/` for a new vault.
  - Copy templates only when absent; never overwrite user edits.
  - Reuse and validate an existing compatible vault on another device.
  - Add `taskctl init` flags and interactive fallbacks for vault and viewer.
- **Validation:** Golden-test installed templates and rerun initialization after
  editing a template to prove it is preserved.
- **Review notes:** Confirm `init` never runs Git or creates project state.

### PR-002: Implement the pure domain model

- **Objective:** Encode all lifecycle rules and aggregate calculations in a pure,
  filesystem-independent module.
- **Related requirements:** R4, R5, R6, R8, R13, R15.
- **Dependencies:** PR-001 for shared error conventions only.
- **Review scope:** Persisted schema shape, state-machine correctness, derived
  status semantics, and ID invariants.
- **Expected files/areas:** `internal/domain` and table-driven tests.
- **In scope:** Models, enums, validation, progress, transitions, initial plan
  structure, and ID allocation.
- **Out of scope:** YAML encoding, filesystem writes, Markdown, Git, and CLI.
- **Risks:** Persisting aggregate status accidentally; vacuous completion for
  empty plans/PRs; invalid reopening behavior; duplicate IDs.
- **Implementation guidance:** Expose intention-revealing transition functions
  that validate current state. Keep derived status as methods/results, never
  writable fields.
- **PR validation:** Exhaustive table tests for every allowed and rejected
  transition plus aggregate-state/property tests over mixed completed/skipped
  structures.
- **Done when:** All RFC lifecycle behavior can be exercised through the domain
  interface without a vault or Git repository.

#### STEP-004: Define versioned Task, PR, and Step models

- **Status:** Completed
- **Purpose:** Freeze a minimal v1 storage contract before implementing stores.
- **Related requirements:** R5, R6, R15.
- **Changes:**
  - Define model fields, status enums, timestamps, branch associations, and skip
    reasons.
  - Keep PR/Step order in slices and document required/optional fields.
  - Add structural validation for identity, title, project, nonempty started PRs,
    branch presence, and status-specific metadata.
- **Validation:** Valid/invalid fixture tests and enum parse/string tests.
- **Review notes:** Derived Task/PR status must not appear in persisted structs.

#### STEP-005: Implement derived status and progress calculations

- **Status:** Completed
- **Purpose:** Make aggregate state impossible to drift from Step state.
- **Related requirements:** R5, R6, R11, R12.
- **Changes:**
  - Derive Task draft/in-progress/completed/cancelled status.
  - Derive PR pending/in-progress/completed/skipped status.
  - Calculate separate completed, skipped, and total counts at Task and PR scope.
  - Identify the single active Step and detect invalid multiple-active state.
- **Validation:** Table-test empty draft, applied draft, mixed PRs, all-skipped,
  reopened, cancelled, and inconsistent structures.
- **Review notes:** Explicitly test the non-vacuous completion rules.

#### STEP-006: Implement lifecycle transitions and invariants

- **Status:** Completed
- **Purpose:** Centralize every state mutation behind validated operations.
- **Related requirements:** R6, R13, R15.
- **Changes:**
  - Add PR start/skip and Task cancel transitions.
  - Add Step start, submit, revise, complete, skip, and reopen transitions.
  - Enforce one active Step per PR, started-PR requirements, skip reasons, and
    user-acceptance ordering.
  - Ensure adding/reopening work naturally changes derived aggregate state.
- **Validation:** A transition matrix covering every source status, target
  operation, and expected typed error.
- **Review notes:** Avoid a generic unrestricted `SetStatus` function.

#### STEP-007: Implement ID allocation and structured-plan validation

- **Status:** Completed
- **Purpose:** Make initial and appended IDs deterministic and unambiguous.
- **Related requirements:** R4, R8, R15.
- **Changes:**
  - Parse and format project Task prefixes and sequential Task IDs.
  - Validate and allocate Task-local `PR-NNN` and globally Task-local
    `STEP-NNN` IDs.
  - Validate initial plan input for nonempty PRs, nonempty Steps, unique IDs,
    legal formats, global Step uniqueness, and order.
- **Validation:** Gaps, malformed IDs, overflow, duplicate IDs across PRs, and
  review-fix append allocation tests.
- **Review notes:** Do not infer IDs from titles or branch names.

### PR-003: Implement persistence and Git-backed context facts

- **Objective:** Reliably load/save vault state and derive portable project and
  branch facts through the installed Git CLI.
- **Related requirements:** R2, R3, R5, R7, R15, R16.
- **Dependencies:** PR-001 and PR-002.
- **Review scope:** Atomic storage, schema handling, URL normalization, command
  execution, ambiguity detection, and context precedence.
- **Expected files/areas:** `internal/vault`, `internal/gitcli`,
  `internal/process`, and integration tests with temp repositories.
- **In scope:** YAML stores, scans, normalized origin/current branch, project
  registration primitives, and Task/PR resolution.
- **Out of scope:** User commands and vault remote-status fetch.
- **Risks:** URL variants resolving differently, traversal through malformed
  IDs, partial writes, silently selecting duplicate branch matches.
- **Implementation guidance:** Keep path construction inside the vault module;
  validate every path segment before joining. Use a fake process adapter for
  command tests and real local repositories for integration confidence.
- **PR validation:** Unit tests plus temp Git repositories covering HTTPS, SCP
  SSH, `ssh://`, nested groups, no origin, detached HEAD, and duplicate branch
  associations.
- **Done when:** Application workflows can ask for validated project/task state
  without knowing YAML paths or Git command syntax.

#### STEP-008: Add atomic YAML stores and project scans

- **Status:** Completed
- **Purpose:** Provide the canonical storage implementation.
- **Related requirements:** R3, R5, R15.
- **Changes:**
  - Implement versioned root, project, and Task load/save operations.
  - Use strict known-field decoding and schema-version checks.
  - Implement same-directory atomic replacement and deterministic YAML output.
  - Scan only direct Task directories under the resolved project and return
    explicit duplicate/corruption errors.
- **Validation:** Round-trip, unknown field, unsupported version, interrupted
  temporary file, missing file, path-safety, and scan-order tests.
- **Review notes:** Do not create a separate task index or persist absolute Task
  artifact paths.

#### STEP-009: Add Git process adapter and remote normalization

- **Status:** Completed
- **Purpose:** Hide Git command syntax behind a small, testable interface.
- **Related requirements:** R2, R7, R16.
- **Changes:**
  - Add a context-aware process runner with captured stdout/stderr and typed
    failures.
  - Read `remote.origin.url` and the current named branch using Git CLI commands.
  - Normalize HTTPS, SCP-style SSH, and `ssh://` remotes to `host/path`.
  - Derive a sanitized project ID from all ownership/repository path segments.
  - Reject local/file remotes for automatic portable identity.
- **Validation:** Table-test remote variants, `.git` suffixes, case/whitespace,
  nested groups, malformed inputs, detached HEAD, and missing Git.
- **Review notes:** Preserve enough normalized identity to distinguish hosts;
  avoid logging credentials embedded in malformed remotes.

#### STEP-010: Implement project registration and context resolution

- **Status:** Completed
- **Purpose:** Resolve the same Task correctly across devices and worktrees.
- **Related requirements:** R2, R3, R7, R15.
- **Changes:**
  - Match the current repository against full normalized project identity.
  - Create project-registration inputs for derived or explicit identities and
    configurable Task prefixes.
  - Resolve an exact current-branch PR match before `current_task`.
  - Return typed errors for no current Task, stale selection, mismatched remote,
    or multiple branch matches.
- **Validation:** Multi-Task/project fixtures proving branch precedence,
  synchronized fallback, stale current Task behavior, and ambiguity failure.
- **Review notes:** Never fall back to filesystem location or choose the first
  ambiguous manifest.

### PR-004: Deliver Task and artifact workflows

- **Objective:** Let users create/select Tasks and create/view their Markdown
  artifacts without Obsidian.
- **Related requirements:** R1, R2, R3, R4, R9, R10, R15, R16.
- **Dependencies:** PR-001 through PR-003.
- **Review scope:** End-to-end Task creation, template safety, portable paths,
  and external viewer invocation.
- **Expected files/areas:** `internal/app` Task/artifact workflows and related
  `internal/cli` commands.
- **In scope:** `new`, `use`, `task list`, `task cancel`, `artifact ensure`,
  `path`, and `artifact view`.
- **Out of scope:** Plans, PRs, Steps, context JSON, and detailed status.
- **Risks:** Sequential ID collisions, current-task updates without Task creation,
  template execution errors, and shell injection in viewer configuration.
- **Implementation guidance:** Orchestrate writes so a new Task is fully created
  before changing `current_task`. Execute viewer command/args directly and append
  the Task directory as one argument.
- **PR validation:** CLI integration tests with isolated config/vault/repository
  directories and a fake viewer executable.
- **Done when:** The complete pre-planning human workflow works without manual
  file copying or repository-local state.

#### STEP-011: Implement new, use, list, and cancel workflows

- **Status:** Completed
- **Purpose:** Deliver the core Task catalog and synced selection behavior.
- **Related requirements:** R2, R3, R4, R5.
- **Changes:**
  - Auto-register a missing project during `new`, with interactive prefix
    confirmation and noninteractive flags.
  - Scan and allocate the next Task ID, render Task metadata, then set current.
  - Implement validated `use`, project-scoped listing, and Task cancellation.
  - Keep cancellation explicit and prevent normal execution mutations afterward.
- **Validation:** First/subsequent Task, prefix override, failed render rollback,
  stale current, cancelled Task, and two-project tests.
- **Review notes:** Document the sync-before-new assumption rather than hiding
  conflicts with opaque IDs.

#### STEP-012: Implement lazy artifact ensure and path lookup

- **Status:** Completed
- **Purpose:** Give agents safe, deterministic artifact creation and discovery.
- **Related requirements:** R9, R10.
- **Changes:**
  - Render research, plan, and review templates with Task creation metadata.
  - Make `artifact ensure` idempotent and never overwrite existing content.
  - Make `path` read-only and fail for missing artifacts.
  - Return absolute normalized paths while persisting no absolute paths.
- **Validation:** Each artifact type, repeated ensure, customized template,
  template error, missing path, and no-frontmatter golden tests.
- **Review notes:** `task.md` is created only by `new` and must not be accepted by
  `artifact ensure`.

#### STEP-013: Implement configured viewer launch

- **Status:** Completed
- **Purpose:** Open a Task directory in Typora or another per-device application.
- **Related requirements:** R1, R16.
- **Changes:**
  - Validate configured executable and preserve each configured argument.
  - Append the Task directory as the final argument.
  - Launch without shell interpretation or waiting for GUI termination.
  - Return concise process-start failures.
- **Validation:** Fake process assertions for Linux and macOS examples, spaces in
  paths/args, missing executable, and nonblocking behavior.
- **Review notes:** Never join command/args into a shell string.

#### STEP-014: Add Task/artifact CLI integration coverage

- **Status:** Completed
- **Purpose:** Verify Cobra wiring and persisted effects as one user workflow.
- **Related requirements:** R1, R3, R4, R10, R15.
- **Changes:**
  - Exercise init → new → ensure → path → use/list/cancel in temp environments.
  - Assert stdout/stderr separation and exit codes.
  - Verify reruns preserve customized Markdown and templates.
- **Validation:** `go test ./...` with golden CLI outputs and no dependency on the
  developer's real config, vault, Git config, or viewer.
- **Review notes:** Prefer in-process command construction unless a real process
  is required to validate exit behavior.

### PR-005: Deliver structured planning and Markdown projection

- **Objective:** Register plans safely and keep a human-readable progress view
  synchronized with canonical state.
- **Related requirements:** R5, R8, R9, R13, R15.
- **Dependencies:** PR-002 through PR-004.
- **Review scope:** JSON plan contract, Markdown marker safety, replacement versus
  append semantics, and plan/manifest consistency.
- **Expected files/areas:** `internal/plan`, domain plan operations, app/CLI plan,
  PR-list, and Step-list/add commands.
- **In scope:** `plan apply`, bounded progress rendering, `pr add/list/skip`,
  `step add/list`, and safe title correction.
- **Out of scope:** Starting PRs and Step execution transitions.
- **Risks:** Corrupting user prose, accepting IDs not represented in Markdown,
  accidental topology replacement after work starts, and canonical/projection
  partial updates.
- **Implementation guidance:** Use exact line-oriented heading and marker rules;
  do not introduce a general Markdown parser or derive plan hierarchy from prose.
- **PR validation:** Golden Markdown tests plus integration tests for initial
  apply, draft replacement, post-start rejection fixtures, append operations,
  missing/duplicate markers, and projection write failure.
- **Done when:** A planner can create a detailed Markdown plan, register its
  structure, and see a generated Progress block without duplicated mutable state.

#### STEP-015: Implement plan input and heading validation

- **Status:** Completed
- **Purpose:** Validate the machine hierarchy against user-readable prose.
- **Related requirements:** R8, R9, R15.
- **Changes:**
  - Decode plan JSON from stdin or an explicit file with unknown-field rejection.
  - Scan exact PR/Step headings, preserving titles and source order.
  - Require every registered ID exactly once, enforce parent/order
    correspondence, and reject unregistered structured headings.
  - Return errors with the relevant ID and Markdown line.
- **Validation:** Valid plan, duplicate/missing heading, wrong nesting/order,
  title mismatch, malformed JSON, and unknown field tests.
- **Review notes:** Heading validation is not permission to infer status or
  hierarchy from arbitrary Markdown.

#### STEP-016: Implement bounded Progress rendering

- **Status:** Completed
- **Purpose:** Project canonical state into Typora-visible Markdown safely.
- **Related requirements:** R5, R9, R15.
- **Changes:**
  - Render Task-wide PR/Step progress with stable human status labels.
  - Replace only content strictly between one start and end marker.
  - Preserve bytes outside the bounded block.
  - Reject missing, duplicate, reversed, or nested markers.
- **Validation:** Golden states including draft, active review, skipped items,
  completed, reopened, Unicode titles, CRLF input, and malformed markers.
- **Review notes:** Keep both markers at root indentation so the generated block
  is not accidentally nested under a Markdown list.

#### STEP-017: Implement plan apply and safe metadata correction

- **Status:** Completed
- **Purpose:** Support draft iteration without allowing execution history loss.
- **Related requirements:** R8, R9, R15.
- **Changes:**
  - Replace the hierarchy when no PR has started.
  - After execution starts, permit only title corrections when IDs, order, and
    parent relationships are unchanged; reject topology replacement.
  - Prepare validation and projection before atomically saving canonical state.
  - Refresh Progress and report typed partial projection failures.
- **Validation:** Multiple draft revisions, post-start title-only update,
  deletion/reparent/reorder rejection, and unchanged-state idempotence.
- **Review notes:** Preserve Step statuses, branches, timestamps, and skip reasons
  during metadata-only correction.

#### STEP-018: Implement append-only PR/Step evolution and list commands

- **Status:** Completed
- **Purpose:** Support implementation discoveries and final-review correction
  Steps without reopening bulk plan replacement.
- **Related requirements:** R8, R13.
- **Changes:**
  - Add `pr add` with next-ID allocation and prevent start until it has a Step and
    matching plan section.
  - Add Task-wide `step add --pr ... --title ...` with next global Step ID.
  - Add PR/Step list outputs suitable for humans and optional JSON consumers.
  - Implement PR/Step skip with required reason and projection refresh.
- **Validation:** Add to active/completed Tasks, ID continuation, unknown parent,
  skip behavior, aggregate reopening, and list ordering tests.
- **Review notes:** The command returns allocated IDs so the agent can append the
  corresponding detailed Markdown section; it must not synthesize prose.

### PR-006: Deliver PR and Step execution lifecycle

- **Objective:** Support the complete incremental implementation and user-review
  loop on user-managed branches.
- **Related requirements:** R3, R6, R7, R10, R11, R13, R15.
- **Dependencies:** PR-003 through PR-005.
- **Review scope:** Branch capture only, context safety, transition commands,
  agent JSON minimality, and projection consistency.
- **Expected files/areas:** Domain/app execution operations and CLI PR/Step
  commands.
- **In scope:** `pr start`, `step get`, and all Step lifecycle commands.
- **Out of scope:** Detailed human status and vault remote status.
- **Risks:** Starting on detached/wrong branch, associating one branch twice,
  selecting a pending Step while one awaits review, or allowing implementation
  on a different PR branch.
- **Implementation guidance:** Resolve Task/PR once per operation, validate all
  preconditions, mutate a copied domain value, then persist and project.
- **PR validation:** Full integration workflow from pending PR through review,
  revision, completion, optional corrective Step, reopening, and recompletion.
- **Done when:** Agent workflows can safely drive execution using only commands
  and artifact paths while users retain branch and review control.

#### STEP-019: Implement PR start and branch association

- **Status:** Pending
- **Purpose:** Bind a planned PR to the user's already-created current branch.
- **Related requirements:** R3, R6, R7.
- **Changes:**
  - Require a current Task and named branch.
  - Validate a pending, non-skipped PR with at least one registered Step and
    matching plan headings.
  - Reject branches associated with another PR in the project.
  - Record branch and start time without validating naming or topology.
- **Validation:** Normal start, detached HEAD, duplicate branch, already-started
  PR, empty PR, missing plan section, and branch names with slashes.
- **Review notes:** No checkout, branch creation, merge-base, fetch, or naming
  policy should enter this command.

#### STEP-020: Implement Step selection and `step get` JSON

- **Status:** Pending
- **Purpose:** Give implementation agents the smallest safe state-discovery
  response.
- **Related requirements:** R6, R11.
- **Changes:**
  - Require a branch-associated current PR.
  - Select its sole in-progress/ready-for-review Step, otherwise first pending.
  - Return Task, PR, Step IDs, Step status, and only existing absolute artifact
    paths.
  - Return no titles, action recommendation, counts, or plan prose.
- **Validation:** Pending/resume/review selection, no work, multiple-active
  corruption, missing optional artifacts, wrong branch, and exact JSON golden.
- **Review notes:** JSON stdout must remain clean; diagnostics go to stderr.

#### STEP-021: Implement Step lifecycle commands

- **Status:** Pending
- **Purpose:** Record implementation, automated review, user feedback, and
  acceptance through explicit transitions.
- **Related requirements:** R6, R9, R13, R15.
- **Changes:**
  - Wire start, submit, revise, complete, skip, and reopen to domain transitions.
  - Default omitted IDs only when selection is unambiguous.
  - Require skip reasons and prevent a second active Step in the PR.
  - Regenerate Progress after every successful transition.
- **Validation:** CLI transition matrix, explicit/default IDs, invalid order,
  repeated command, projection failure, and output/exit-code tests.
- **Review notes:** `complete` means user acceptance, not implementation-agent
  completion before review.

#### STEP-022: Verify incremental review and reopening workflows

- **Status:** Pending
- **Purpose:** Prove aggregate behavior across the workflow's highest-risk state
  sequences.
- **Related requirements:** R5, R6, R13.
- **Changes:**
  - Add integration scenarios for submit → revise → resubmit → complete.
  - Complete all Steps and assert automatic PR/Task completion.
  - Add a final-review correction Step and assert automatic reopening.
  - Complete the correction and assert recompletion without remote merge state.
- **Validation:** Scenario tests against real temp vault files and generated
  `plan.md` content.
- **Review notes:** Do not create or inspect actual Git commits in these tests.

### PR-007: Deliver context, human status, and vault Git status

- **Objective:** Expose concise agent context and useful human progress with
  fresh, non-authoritative vault Git visibility.
- **Related requirements:** R3, R5, R11, R12, R14, R15, R16.
- **Dependencies:** PR-003 through PR-006.
- **Review scope:** JSON schema, human formatting, branch/current semantics,
  external Git timeout/failure behavior, and warning isolation.
- **Expected files/areas:** App/CLI context and status modules plus Git vault
  status implementation.
- **In scope:** `context`, detailed `status`, and `vault status`.
- **Out of scope:** Any vault Git write/sync command.
- **Risks:** Fetch latency or credential prompts, stale/misleading ahead-behind
  output, JSON schema drift, and confusing current versus merely active PRs.
- **Implementation guidance:** Run remote checks only for the two approved status
  commands. Use context cancellation, a finite timeout, and noninteractive Git
  terminal prompting; preserve credential-helper support where possible.
- **PR validation:** JSON goldens, human-output goldens, local bare-remotes for
  ahead/behind tests, fetch failures, no-upstream vaults, and branch-precedence
  scenarios.
- **Done when:** Agents and humans can inspect accurate task state without
  reading every artifact, and status never mutates vault content or history.

#### STEP-023: Implement the `context` JSON contract

- **Status:** Pending
- **Purpose:** Provide stable current Task/PR/Step progress and artifact discovery.
- **Related requirements:** R3, R5, R11, R15.
- **Changes:**
  - Return project/Task IDs, derived Task status, and Task PR progress.
  - Include `current_pr` only for the branch-associated PR, with its Step progress.
  - Include `active_step` only for in-progress or ready-for-review state.
  - Report completed/skipped/total as numbers and include only existing artifacts.
- **Validation:** Draft fallback, active branch, completed PR branch, skipped
  counts, no active Step, ambiguity, and exact JSON schema tests.
- **Review notes:** Do not label a merely in-progress PR on another branch as
  `current_pr`.

#### STEP-024: Implement detailed human Task status

- **Status:** Pending
- **Purpose:** Replace artifact scanning with a concise human dashboard.
- **Related requirements:** R5, R12.
- **Changes:**
  - Render Task identity/status/progress and ordered PR/Step rows.
  - Mark current PR, active Step, skips/reasons, and existing artifact paths.
  - Keep formatting stable and terminal-width tolerant without adding a TUI.
  - Reserve a footer area for vault Git status/warnings.
- **Validation:** Golden outputs for draft, mixed, skipped, completed, cancelled,
  and reopened Tasks.
- **Review notes:** Human output may be rich text, but underlying calculations
  must reuse domain results rather than recalculate in the CLI.

#### STEP-025: Implement fresh vault Git status

- **Status:** Pending
- **Purpose:** Surface synchronization risk while leaving synchronization to the
  user.
- **Related requirements:** R14, R16.
- **Changes:**
  - Run `git fetch --quiet` only for `status` and `vault status`.
  - Inspect porcelain dirty count and upstream left/right commit counts.
  - Report clean/dirty, ahead, behind, no repository/upstream, and unavailable
    remote state concisely.
  - Treat fetch failure as a warning for Task status and never show stale remote
    counts as fresh.
- **Validation:** Local bare remote scenarios for clean, dirty, ahead, behind,
  diverged, missing upstream, fetch timeout, and command failure.
- **Review notes:** Assert no add/commit/pull/rebase/push commands are issued.

#### STEP-026: Verify branch precedence and cross-device fallback

- **Status:** Pending
- **Purpose:** Validate the key replacement for `.agent-task` under realistic
  multi-Task use.
- **Related requirements:** R2, R3, R7, R11, R12.
- **Changes:**
  - Create two project clones/worktree-like temp checkouts sharing copied vault
    state.
  - Prove branch association overrides a changed project current Task.
  - Prove an unassociated branch uses synchronized `current_task`.
  - Prove stale or ambiguous state fails explicitly.
- **Validation:** End-to-end context/status commands using real local Git repos
  and copied vault directories.
- **Review notes:** Keep the test provider-neutral and offline.

### PR-008: Harden and document the MVP

- **Objective:** Close integrity gaps, verify the complete command surface, and
  make the tool usable without relying on design-conversation context.
- **Related requirements:** R1–R16.
- **Dependencies:** PR-001 through PR-007.
- **Review scope:** Failure recovery, schema compatibility, CLI consistency,
  documentation accuracy, and cross-platform build readiness.
- **Expected files/areas:** Cross-cutting tests, README/docs, and small fixes only.
- **In scope:** Corruption/failure coverage, complete CLI scenarios, user and
  agent documentation, Linux/macOS validation, and final expert review.
- **Out of scope:** New features, provider integrations, synchronization, or
  architectural rewrites.
- **Risks:** Late inconsistency across command outputs, untested partial writes,
  docs diverging from actual flags, and accidental scope expansion.
- **Implementation guidance:** Treat failures found here as fixes to established
  behavior. Defer new capabilities to follow-up RFCs.
- **PR validation:** Full test/vet/race/build matrix, manual smoke workflow, docs
  command verification, and expert review against the RFC.
- **Done when:** Every RFC success criterion has an automated or documented
  verification path and no unresolved blocking review finding remains.

#### STEP-027: Add corruption, ambiguity, and failure-path coverage

- **Status:** Pending
- **Purpose:** Ensure unsafe state never produces a silent guess or destructive
  rewrite.
- **Related requirements:** R15.
- **Changes:**
  - Cover unknown schemas/fields, malformed YAML/JSON, duplicate IDs/branches,
    stale current Task, invalid markers, and unsupported transitions.
  - Inject write, rename, template, Git, and viewer failures.
  - Assert canonical manifest preservation and partial-projection diagnostics.
- **Validation:** Targeted tests plus `go test -race ./...`.
- **Review notes:** Error text should identify remediation without dumping noisy
  command output or credentials.

#### STEP-028: Add full CLI workflow tests

- **Status:** Pending
- **Purpose:** Exercise the command interface exactly as humans and agents use it.
- **Related requirements:** R1–R15.
- **Changes:**
  - Cover init → new → research/plan ensure → plan apply → branch/PR start → Step
    review loop → optional final-review Step → completion.
  - Cover multiple Tasks, `use`, branch precedence, cancel, skips, and reopened
    work.
  - Assert all agent JSON and important human outputs against stable fixtures.
- **Validation:** Run against isolated HOME/XDG, vault, project, and local Git
  remote fixtures with no external network.
- **Review notes:** Keep fixtures readable enough to diagnose state-machine
  failures.

#### STEP-029: Document installation and human/agent workflows

- **Status:** Pending
- **Purpose:** Make the MVP self-explanatory and replace obsolete Obsidian command
  assumptions.
- **Related requirements:** R1, R3, R7–R14, R16.
- **Changes:**
  - Expand README with build/install, init, vault layout, project registration,
    Task creation, planning, execution, review, and Git-status workflows.
  - Document JSON contracts, exit-code categories, template customization, and
    synchronization responsibilities.
  - Provide concise agent-command examples and explicit non-goals.
  - Cross-check all names and flags against built help output.
- **Validation:** Execute documented happy-path commands in a temp environment and
  run Markdown/link checks if configured.
- **Review notes:** Do not promise remote merge tracking, branch management, or
  vault synchronization.

#### STEP-030: Run final Linux/macOS validation and release review

- **Status:** Pending
- **Purpose:** Establish release readiness for the two supported platforms.
- **Related requirements:** R15, R16.
- **Changes:**
  - Run format, unit/integration, race, vet, and build checks.
  - Cross-build for Linux and macOS architectures supported by the project.
  - Manually smoke-test viewer defaults/configuration assumptions where available.
  - Request final expert review against the RFC and resolve blocking findings.
- **Validation:** Commands listed in Final Integration & Verification below.
- **Review notes:** Record platform limitations rather than adding unplanned
  abstractions during release hardening.

## Final Integration & Verification

Run at minimum:

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/taskctl
GOOS=linux go build ./cmd/taskctl
GOOS=darwin go build ./cmd/taskctl
```

Manual acceptance scenarios:

1. Initialize a new vault, customize a template, and rerun init without losing
   the customization.
2. Register a project from both SSH and HTTPS forms of the same origin and
   confirm they resolve to the same normalized identity.
3. Create two Tasks, switch the project current Task, and confirm no project
   repository pointer file is created.
4. Apply a multi-PR plan, start a PR on a user-created branch, complete the Step
   review/revision loop, and verify generated Markdown progress.
5. Complete a PR, add a final-review correction Step, and verify automatic
   PR/Task reopening and recompletion.
6. Check out branches associated with different Tasks and confirm branch context
   overrides the synchronized fallback.
7. Copy/synchronize the vault to a second machine-like temp environment and
   confirm the current Task resolves using a clone at a different filesystem
   path.
8. Create a local bare vault remote and verify clean, dirty, ahead, behind,
   diverged, unavailable, and no-upstream summaries without any synchronization
   mutation.
9. Open the Task directory through fake Linux and macOS viewer configurations and
   verify argument preservation.
10. Corrupt each schema/marker/association class in turn and confirm commands fail
    explicitly without rewriting the source file.

## Key Risks and Mitigations

- **Sequential ID collision across unsynchronized devices:** Document sync before
  `new`; detect directory/manifest collision and fail instead of overwriting.
- **Synced `current_task` conflicts:** Keep it as the only mutable project-level
  preference, let Git surface concurrent edits, and use branch association when
  available.
- **Markdown corruption:** Replace only a uniquely bounded generated block and
  golden-test byte preservation outside it.
- **Manifest/projection partial update:** Keep manifest canonical, pre-render both
  outputs, use atomic replacement, and report projection failure explicitly.
- **Remote normalization mistakes:** Table-test common forms and require explicit
  identity rather than guessing unsupported remotes.
- **Git fetch hangs/authentication:** Use context timeout and noninteractive
  terminal prompting; downgrade failure to a concise status warning.
- **CLI/domain coupling:** Keep transitions and derivation in the pure domain
  module and test CLI callbacks as adapters.
- **Schema lock-in:** Version every persisted file, reject unknown versions, and
  keep v1 minimal; introduce migrations only through a later explicit design.
- **Title/prose drift:** Validate exact headings during apply/start and permit
  post-start title corrections only when topology is unchanged.

## Open Questions

No product-design questions currently block implementation. Exact dependency
versions, help wording, timestamp formatting details, and terminal table styling
can be selected during the relevant PR while preserving the interfaces and
invariants above.
