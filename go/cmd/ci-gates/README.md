# ci-gates

The `ci-gates` command is the CLI front end for the CI gate registry
([#4213](https://github.com/eshu-hq/eshu/issues/4213)). It gives any local
workflow a single command to find out which credential-free CI verifiers apply
to the files you just changed.

The backing registry is `specs/ci-gates.v1.yaml`. The typed loader, selector,
validator, and glob matcher live in [`internal/cigates`](../../internal/cigates).

## Subcommands

### select

```bash
ci-gates select \
  --registry specs/ci-gates.v1.yaml \
  --tier pre-pr \
  [--base origin/main] \
  [--paths-from paths.txt | --paths-from -] \
  [--explain] [--json]
```

Prints one selected gate id per line (registry order). Changed paths default to
`git diff --name-only <base>...HEAD` + staged + unstaged, mirroring
`scripts/dev/pre-pr.sh`.

- `--explain`: annotate each line with why the gate was selected, skipped, or
  CI-only.
- `--json`: emit a structured object `{tier, base, selected, skipped, ci_only}`.
- `--paths-from <file>`: read changed paths from a file (one per line). Pass
  `-` for stdin. Use this for hermetic tests that bypass git.
- `--category <list>`: comma-separated category filter (e.g.
  `exactness,telemetry`). Gates outside the set are reported as skipped, not
  dropped. Empty means all categories.

### run

```bash
ci-gates run \
  --registry specs/ci-gates.v1.yaml \
  --tier pre-pr \
  [--base origin/main] \
  [--paths-from paths.txt] \
  [--category exactness,telemetry] \
  [--repo-root /path/to/repo]
```

`--category` filters the run to the listed categories — `make pre-pr` uses
`--category exactness,telemetry` to run only the static contract lane (#4214),
leaving the race lane to #4215 and the heavy pre-push gates (gosec, console
e2e, frontend) out of pre-pr.

Runs each selected gate's `local.command` via `/bin/sh -c`, accumulates all
results (does not stop at the first failure), and exits non-zero if any
blocking gate failed. Advisory failures are printed but do not affect the exit
code. CI-only gates are printed as `CI-ONLY` and never executed.

### validate

```bash
ci-gates validate \
  --registry specs/ci-gates.v1.yaml \
  [--repo-root /path/to/repo] \
  [--drift]
```

Checks that every script (`command` and `test_command`) and workflow file
referenced by the registry exists on disk. Exits non-zero and prints each broken
reference. Used by `scripts/verify-ci-gates-registry.sh`.

With `--drift` ([#4220](https://github.com/eshu-hq/eshu/issues/4220)) it also
runs the hook/preflight/workflow lockstep check: every local pre-commit hook
must map to a gate `hook_id` or a `hygiene_hooks` entry, every gate `hook_id`
must exist at a tier-consistent stage, and every workflow must be a gate
`ci.workflow` or a `non_gate_workflows` entry. Used by the `gate-registry-drift`
pre-commit hook and the `verify-ci-gate-registry.yml` workflow.

### uncovered

```bash
ci-gates uncovered \
  --registry specs/ci-gates.v1.yaml \
  --category race \
  --tier pre-pr \
  [--base origin/main] \
  [--paths-from paths.txt | --paths-from -]
```

Prints the changed paths that no locally-runnable gate in the requested
categories (at tier ≤ ceiling) covers via a trigger. `make pre-pr`'s scoped race
lane ([#4215](https://github.com/eshu-hq/eshu/issues/4215)) uses `--category race`
to race exactly the changed packages no race gate already runs — so it never
double-races a registry-owned package (graph-write or replay), and the exclusion
is derived from the registry rather than a hard-coded list. A CI-only gate (no
local command) does not count as covering.

## Thin shell wrappers

| Script | Purpose |
| --- | --- |
| `scripts/dev/select-gates.sh` | `select` subcommand wrapper |
| `scripts/dev/run-selected-gates.sh` | `run` subcommand wrapper |
| `scripts/verify-ci-gates-registry.sh` | `validate` subcommand wrapper (CI integrity gate) |

## Ownership boundary

This command owns only CLI parsing and output formatting. All selection,
validation, and glob-matching logic lives in `internal/cigates`. This command
does not own fact emission, graph writes, or telemetry.

## Tests

```bash
cd go && go test ./cmd/ci-gates/ -count=1
```
