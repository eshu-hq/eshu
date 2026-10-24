# Pull-request CI scheduling

`Build Test` makes one trusted execution plan for each pull-request head. Its
`pr-plan` job checks out the default branch and reads the gate registry and the
frozen synchronize-workflow inventory in `specs/ci-pr-workflows.d/`. The plan
names the applicable workflows, their local jobs, and selected registry gate
IDs. Schema 2 binds the current run and attempt to an immutable tested merge;
transfer modes also carry their direct full-backed source. The plan job fails
if the CLI, schema, or head binding cannot be validated.
Its successful mode step records the exact full, reuse, or smoke selection for
the trusted publisher to compare with the required verdict.

The remaining synchronize workflows are called from `Build Test`. Their
original base-branch and changed-path filters are in the inventory. A callable
job runs only when the plan selects its local job ID. The caller retains each
workflow's token scope and passes the private cassette-author identifiers only
to the static-contract workflow. Each called workflow has its own concurrency
group so it cannot cancel the caller. Opened-only Discord and labeled-only
cassette refresh still use their own PR triggers.
Scheduled PostgreSQL proofs use PR-conditional cancellation in their event/ref
group; scheduled and manual runs retain noncancelling behavior.

| Plan mode | Repository-controlled work on the PR head |
| --- | --- |
| Full | Applicable leaf jobs run, including path-selected blocking and advisory checks. |
| Reuse | No repository-controlled validation leaf job runs; trusted previously green verdicts transfer to the identical tree. |
| Smoke | Selected fast gate commands and the required build run; other full-backed verdicts transfer. |

This guarantee covers `Build Test` and its frozen callable-workflow inventory,
including blocking and advisory leaves. Only the explicitly enumerated planning
and status-publication jobs execute in that controlled set during reuse.
GitHub-managed CodeQL default setup remains enabled outside this scheduler.
Report its actual jobs and runner occupancy separately and include them in
whole-PR and daily totals. Reuse does not promise zero jobs or zero runner cost
across the whole PR. Compare whole-PR costs only when both samples include the
same managed workflows; a repository-workflow subtotal is not a whole-PR total.

The disabled-by-default `CI_SMOKE_GO_BUILD_FAIL_CANARY` repository variable is
passed through the `Build Test` caller step for a reviewed live negative canary.
Its JSON must name the exact
repository, PR number, 40-hex head SHA, run attempt, `mode: smoke`,
`gate: go-build`, and absolute UTC `expires_at` timestamp. The selected Go build
runs first; only a successful build can reach the fixed deliberate failure.
Missing, malformed, mismatched, or expired settings leave normal CI behavior.
The value cannot supply a command, path, or exit code. Activate it only for a
reviewed target and remove it afterward; deploying this hook activates nothing.

The `go-core-complete` and `go-race-complete` jobs remain required. They reject
a failed or malformed plan and verify the source/head binding before accepting
a transferred verdict. `required-gates-complete` is still published by trusted
default-branch policy and treats the planner check as blocking. A missing
selected check is a failure, not a pass from an empty job list.

Main pushes, scheduled runs, and merge-group events still run their existing
independent paths. The callable workflows use the same event context as their
caller, so non-PR event guards continue to apply. See [Pre-PR execution
ownership](pre-pr-execution.md) for the local proof layer.
