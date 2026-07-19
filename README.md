# taskctl

Task management CLI for a structured agent workflow.

## Build

```bash
go build ./cmd/taskctl
```

## Initialize a vault

Interactive setup:

```bash
taskctl init
```

Automation can provide every required value explicitly:

```bash
taskctl init \
  --vault "$HOME/agent-vault" \
  --viewer open \
  --viewer-arg=-a \
  --viewer-arg=Typora \
  --non-interactive
```

Initialization creates `taskctl.yaml`, `projects/`, and editable Markdown
templates in the dedicated vault. Re-running it validates the vault and installs
only missing templates; customized templates are never overwritten. It does not
initialize or invoke Git.

Machine-local configuration is stored under the platform user-config directory:
`$XDG_CONFIG_HOME/taskctl/config.yaml` (or `~/.config/taskctl/config.yaml`) on
Linux and `~/Library/Application Support/taskctl/config.yaml` on macOS.

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

Operational failures are written to standard error without Cobra usage text.
Agent-facing JSON commands added by later implementation stages reserve standard
output exclusively for JSON.
