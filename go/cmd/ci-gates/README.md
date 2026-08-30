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
  [--self-tests all|changed] [--blocking-only] \
  [--report-file /path/to/report.json] \
  [--repo-root /path/to/repo]
```

`--category` filters the run to the listed categories. `make pre-pr` uses
`--category exactness,telemetry,hygiene,docs` for credential-free contract,
policy, and documentation gates (#4214), leaving the race lane to #4215 and
the heavy pre-push gates (gosec, console e2e, frontend) out of this step.

Runs each selected gate's `local.command` and then its non-empty
`local.test_command` via `/bin/sh -c`. Byte-identical command/test-command
pairs run once. Separate selected rows also reuse a byte-identical command when
both rows declare the same `ci.workflow` and `ci.job`; the runner prints
`REUSE`, attributes the shared result to each gate, and still applies each
gate's own blocking or advisory disposition. Commands with different hosted
owners remain independent. The runner accumulates all results (including
running a test command after its gate command fails) and exits non-zero if any
blocking gate failed. CI-only gates are printed as `CI-ONLY` and never
executed.

`--self-tests changed` uses a gate's optional `self_test_triggers` to keep
tests of the verifier itself out of a product-only change. A gate without that
field keeps the safe legacy behavior and always runs its distinct
`test_command`. `--blocking-only` leaves advisory gates outside the promotion
path. `--report-file` writes an atomic mode-0600 JSON record with command
hashes, durations, reuse, skip reasons, and failure counts. `make pre-pr` uses
all three flags; `make pre-pr-full` includes advisory gates.

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

### audit-scripts

```bash
ci-gates audit-scripts \
  --registry specs/ci-gates.v1.yaml \
  --repo-root . \
  [--unreferenced-only] [--json]
```

Inventories every regular Git-tracked `.sh` file present in the work tree and
reports typed evidence from gate commands and triggers, workflow run blocks,
literal shell-source edges, and other exact repo-relative path mentions. Gate
triggers show selection coverage; they do not count as usage. Results are
deterministic and credential-free. `--unreferenced-only` narrows the rows while
retaining totals for the full inventory.

`unreferenced` is an investigation signal, not a deletion verdict. It means no
supported in-repository usage reference was observed; maintainers, community
users, or external automation may still invoke the script directly.

### review-attest

```bash
ci-gates review-attest capture \
  --base origin/main \
  --claims-file /path/to/exact-pr-title-and-body \
  --review-packet /path/to/reviewed-diff-packet \
  --verdict /path/to/review-verdict \
  --receipt /path/outside-the-worktree/review.json

ci-gates review-attest verify  # repeat the same flags
```

`capture` records the exact review inputs on a clean named feature branch.
`verify` recomputes them after `make pre-pr`. It checks the base and head
commits and trees, merge base, binary and raw diffs, commit range, worktree,
submodules, PR claims, review packet, and verdict. A match permits a mechanical
final attestation instead of a second semantic review. A mismatch names the
first changed field and requires a full rereview. Keep the receipt outside the
worktree, normally under the shared Git common directory, so the receipt does
not make the tree dirty.

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
until they pass. Failed, neutral, missing, and timed-out selected checks fail
closed, and so does a gate GitHub skipped for its own reasons -- see the skip
rule below. A selected check that never produced a verdict fails closed too,
but is not called a gate failure
([#6189](https://github.com/eshu-hq/eshu/issues/6189)): that is infrastructure
state, not a gate result, and calling it "A required gate failed" is what
teaches people to read a red required status as noise. Once every selected gate
is terminal and at least one never produced a verdict, await exits 13 and the
publisher posts `error` naming the re-run.

Four shapes qualify, and they cost different things to recognise:

- **Cancelled**, detected from either `state=CANCELLED` or `bucket=cancel`,
  because the runner's `gh` version is not pinned and older releases folded
  cancellations into the `fail` bucket.
- **Stale**, detected from `state=STALE` -- a conclusion only GitHub sets, on a
  check run it orphaned. gh reports it in the `pending` bucket, so the
  aggregate has to test for it *before* the pending bucket; otherwise it waits
  out the full timeout on a check that will never complete and publishes
  nothing, stranding the status on `pending` with no red check to act on.
- **Skipped because the run that owned the job was cancelled.** This one is
  ambiguous and is the only case that costs an API call: GitHub reports the
  same `SKIPPED` conclusion whether a `needs:` dependency was cancelled or the
  job's own `if:` excluded it. The aggregate reads the owning workflow run's
  conclusion (`actions: read`, already granted) and treats the skip as a
  cancellation artifact only when that run was cancelled. A run that exists but
  has **not concluded** is neither: that is the window the `gh run rerun`
  repair passes through, where the replacement run is executing but its check
  runs have not yet replaced the cancelled run's in the rollup. Calling it
  "cancelled" would publish `error` against a run that may still pass, and
  calling it "not cancelled" would publish "A required gate failed" against a
  run that has not failed, so the aggregate keeps waiting instead. That wait
  terminates: the replacement run's own completion re-triggers the aggregate
  with a rollup it can decide. A workflow with no run on this head at all is
  unknown, not in flight, and keeps failing closed.
- **Missing entirely because the run was cancelled before the job existed.**
  Cancel a run early enough and GitHub never creates the job's check run, so
  `gh pr checks` reports nothing for it and the gate matches no row at all.
  Waiting cannot terminate -- nothing will ever create that check -- so before
  this the aggregate timed out, exited 11, published nothing, and left the
  status on `pending` with no red to act on. It is resolved from the same
  owning-run conclusion the skipped shape uses. **A check missing for any
  other reason keeps the aggregate waiting**, never resolves: a run that
  finished without producing the job is a real disagreement between the
  registry and the workflow, a workflow with no run on this head is unknown,
  and a failed lookup is both. Only a `cancelled` owning run turns a missing
  check into a verdict.

  Its lookup is deferred while any selected check is genuinely still running,
  which keeps it off the common path: early in a head's life most selected
  gates have no row yet, and asking then would put the call on nearly every
  poll of nearly every aggregate. Deferring is free -- the aggregate waits
  either way until those checks finish -- and cannot strand the head, because
  the next poll after they finish asks. `STALE` is excluded from "still
  running" for this purpose, since it arrives in gh's `pending` bucket while
  being entirely terminal.

**A gate skipped for its own reasons still publishes `failure`.** The registry
selected it for these paths, so a skip the workflow chose is a real
disagreement about whether the gate should have run. The lookup is made only
when a selected check is `SKIPPED`, and if it fails the gate stays a failure --
it degrades to the older behaviour, never to a pass. `NEUTRAL` shares gh's
`skipping` bucket but is a conclusion a job reached, so it fails closed
unconditionally.

A cancellation alongside a still-running gate keeps waiting, so a gate
that goes genuinely red still publishes `failure`. Renames select against both
the old and new path, so moving a file out of a gated tree cannot bypass its
verifier. It verifies the head again before returning success. Pending reads
back off from 30 seconds to five minutes. Per-head workflow concurrency keeps
one aggregate running and retains only the latest pending run. The retained run
starts after the active poller, posts pending before setup, and recomputes the
current check set. This avoids a high steady-state API polling rate. The
polling component can add up to five minutes before observing a newly completed
check; Actions scheduling, runner allocation, and serialized pending-run
startup can add further delay.

The exit code is the contract with the `case "${AGGREGATE_CODE}"` arms in
`.github/workflows/required-gates.yml`, and only `10` may publish `failure`:

| Exit | Meaning | Published status |
| ---: | --- | --- |
| `0` | every selected blocking gate passed | `success` |
| `10` | a selected gate concluded failure | `failure` |
| `11` | selected gates still running (timeout or superseded run) | nothing; the status stays pending for the next run |
| `12` | aggregation could not reach a verdict (API error, bad token, unreadable registry) | `error` |
| `13` | every selected gate is terminal and at least one never produced a verdict (cancelled, stale, skipped by its run's cancellation, or missing because that run was cancelled before the job existed) | `error` |

Codes start at `10` so a `go build` failure (`1`) or a usage error (`2`) cannot
be mistaken for a gate result. `internal/cigates` re-declares `11` and `13` to
validate those workflow arms statically; `TestStillRunningCodeMatchesAwaitContract`
and `TestGateCancelledCodeMatchesAwaitContract` pin the mirrors against this
package's constants.

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
