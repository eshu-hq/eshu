# ci-gates

The `ci-gates` command is the CLI front end for the CI gate registry
([#4213](https://github.com/eshu-hq/eshu/issues/4213)). It gives any local
workflow a single command to find out which local verifiers apply and which
blocking GitHub checks a pull request must pass.

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
  [--category exactness,telemetry,hygiene,docs] \
  [--repo-root /path/to/repo]
```

`--category` filters the run to the listed categories. `make pre-pr` uses
`--category exactness,telemetry,hygiene,docs` for credential-free contract,
policy, and documentation gates (#4214), leaving the race lane to #4215 and
the heavy pre-push gates (gosec, console e2e, frontend) out of this step.

Runs each selected gate's `local.command` and then its non-empty
`local.test_command` via `/bin/sh -c`. Byte-identical command/test-command
pairs run once. The runner accumulates all results (including running a test
command after its gate command fails) and exits non-zero if any blocking gate
failed. Advisory failures are printed but do not affect the exit code. CI-only
gates are printed as `CI-ONLY` and never executed.

For a `command` shape of `bash scripts/verify-*.sh`, the inner `bash` token
resolves via PATH, and on macOS that finds the system `/bin/bash` (3.2.57)
ahead of any Homebrew install. Bash 3.2 lacks bash 4.0+ features
(`declare -A`) that some `verify-*.sh` gates use, which used to false-fail a
blocking gate like `docs-catalog-metadata` on a developer's Mac even though
the script passes under bash >= 4.4 and CI's Linux `bash` is already >= 4.x
([#5050](https://github.com/eshu-hq/eshu/issues/5050)). `run` now resolves a
qualifying bash (PATH, then `/opt/homebrew/bin/bash`, then
`/usr/local/bin/bash`) and prepends its directory to the subprocess PATH, so
the common case passes outright; each such script also carries its own
bash >= 4.4 precondition guard as defense-in-depth.

`run` also strips `GOROOT` from every gate subprocess's environment.
`scripts/dev/run-selected-gates.sh` launches this binary via `go -C go run`,
and when `go/go.mod` requests a newer Go than the host toolchain, the
`GOTOOLCHAIN=auto` switch exports the downloaded toolchain's `GOROOT` to the
runner process. Handed on to gates, that `GOROOT` makes any gate running `go`
in a module the host toolchain already satisfies (`sdk/go/collector`,
`examples/collector-extensions/scorecard`) pair the host `go` driver with the
switched toolchain's tools — every package then fails with
`compile: version "go1.X" does not match go tool version "go1.Y"`. With
`GOROOT` cleared, each gate's `go` command derives its root from its own
binary and re-switches per its own `go.mod`, so both module families stay
self-consistent. This is the runner-wide form of the per-`go install`
isolation `scripts/dev/precommit-go.sh` got in
[#6113](https://github.com/eshu-hq/eshu/pull/6113).

### await

```bash
ci-gates await \
  --registry specs/ci-gates.v1.yaml \
  --repo-root /trusted/default-branch/checkout \
  --repo eshu-hq/eshu \
  --pr 42 \
  --head-sha <exact-pr-head>
```

Used by the trusted `required-gates-complete` publisher. It verifies the pull
request head, fetches every changed file, selects all matching blocking gates,
resolves exact workflow/check identities from the trusted checkout, and polls
until they pass. Failed, skipped, neutral, missing, and timed-out selected
checks fail closed. Renames select against both the old and new path, so moving
a file out of a gated tree cannot bypass its verifier. It verifies the head
again before returning success. Pending reads back off from 30 seconds to five
minutes. Per-head workflow concurrency keeps one aggregate running and retains
only the latest pending run. The retained run starts after the active poller,
posts pending before setup, and recomputes the current check set. This avoids a
high steady-state API polling rate. The polling component can add up to five
minutes before observing a newly completed check; Actions scheduling, runner
allocation, and serialized pending-run startup can add further delay.

### contexts

```bash
ci-gates contexts --registry specs/ci-gates.v1.yaml [--json]
```

Prints the required-status manifest. JSON includes each context's pinned GitHub
App integration ID and is consumed by the live effective-rules verifier.

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

This command owns CLI parsing, Git/GitHub boundary calls, polling, and output
formatting. Selection, required-gate evaluation, validation, and glob matching
live in `internal/cigates`. Only `await` requires network access and GitHub
credentials; the other subcommands remain hermetic. This command does not own
fact emission, graph writes, or telemetry.

## Tests

```bash
cd go && go test ./cmd/ci-gates/ -count=1
```
