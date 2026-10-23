# Pull-request CI scheduling

`Build Test` makes one trusted execution plan for each pull-request head. Its
`pr-plan` job checks out the default branch and reads the gate registry and the
frozen synchronize-workflow inventory in `specs/ci-pr-workflows.d/`. The plan
names the applicable workflows, their local jobs, and selected registry gate
IDs. The plan job fails if the CLI, schema, or head binding cannot be validated.

The remaining synchronize workflows are called from `Build Test`. Their
original base-branch and changed-path filters are in the inventory. A callable
job runs only when the plan selects its local job ID. The caller retains each
workflow's token scope and passes the private cassette-author identifiers only
to the static-contract workflow. Each called workflow has its own concurrency
group so it cannot cancel the caller. Opened-only Discord and labeled-only
cassette refresh still use their own PR triggers.

| Plan mode | Hosted work on the PR head |
| --- | --- |
| Full | Applicable leaf jobs run, including path-selected blocking and advisory checks. |
| Reuse | No leaf job runs; trusted previously green verdicts transfer to the identical tree. |
| Smoke | Selected fast gate commands and the required build run; other full-backed verdicts transfer. |

The `go-core-complete` and `go-race-complete` jobs remain required. They reject
a failed or malformed plan and verify the source/head binding before accepting
a transferred verdict. `required-gates-complete` is still published by trusted
default-branch policy and treats the planner check as blocking. A missing
selected check is a failure, not a pass from an empty job list.

Main pushes, scheduled runs, and merge-group events still run their existing
independent paths. The callable workflows use the same event context as their
caller, so non-PR event guards continue to apply. See [Pre-PR execution
ownership](pre-pr-execution.md) for the local proof layer.
