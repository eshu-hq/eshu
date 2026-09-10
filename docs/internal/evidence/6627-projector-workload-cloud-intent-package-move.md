# #6627 workload-cloud projector package move

## Scope

Base: `9d3ebc12a0bf022799103746f75cd4751f5ae4d3`

This slice moves the workload-cloud-relationship reducer-intent builder from
`internal/projector/workloadcloud` to
`internal/projector/workload/cloud`, renames
`relationship_materialization_intents.go` to `reducer_intent.go`, and shortens
the exported API from
`BuildWorkloadCloudRelationshipMaterializationReducerIntent` to
`BuildReducerIntent`. The production function body is otherwise unchanged.

The `internal/projector/workload` package is documentation-only. The cloud leaf
still imports Eshu's internal facts, projector-intent, and reducer-domain
contracts, and root projector assembly remains its only production caller.
This is a clearer ownership seam for a future repository split, not a claim
that the package is independently extractable today.

## Rebased inventory and dependency edges

The affected leaf at the rebased base contained five files: its package-doc
trio, one non-test implementation file, and one test file. The destination
leaf contains those same five responsibilities. The new parent adds only its
required package-doc trio, including a declaration-only `doc.go`; root
projector non-test file count remains 47.

Before and after the move, production imports are exactly `internal/facts`,
`internal/projector/intent`, and `internal/reducer`. The leaf test imports the
same three repository packages plus `reflect`, `testing`, and `time`. The sole
production reverse importer before and after path substitution is
`internal/projector/scope_generation_intents.go`; no test imports the leaf.
The destination adds no root-projector, sibling, storage, transport, or
runtime-implementation dependency, and no old-path forwarding package remains.

## Preserved contracts

- One intent is returned for a scope generation containing `aws_resource`.
- The earliest `aws_resource` fact in original input order remains the anchor.
- Domain, shared `aws_resource_materialization:<scope>` entity key, reason,
  fact ID, and two-tier source-system selection are unchanged.
- Root fan-out remains 44 probes. Workload cloud remains probe 10, after cloud
  inventory and before EC2 instance-node materialization.
- Root owns assembly, queueing, retries, and enqueue telemetry. The reducer
  owns workload-endpoint resolution, graph materialization, readiness, and
  runtime telemetry.
- No public HTTP or MCP API, payload, fact, reducer-domain, queue, graph,
  telemetry, IFA fixture, or golden corpus contract changes; the internal Go
  API rename is described above.

## TDD evidence

Before the move, the focused leaf guard passed 1 of 1 tests and the exact root
fan-out parity and documented-probe-count guard passed 2 of 2 tests.

The destination test was then moved first, changed to `package cloud`, and
updated to call `BuildReducerIntent`. Running
`go test ./internal/projector/workload/cloud -run '^TestBuildReducerIntent$' -count=1`
failed with three `undefined: BuildReducerIntent` compile errors and exit 1.
That compile-contract failure is the red phase for the path and API move.

## Final verification

All commands below completed with exit 0 after the final production edit:

- The focused `TestBuildReducerIntent` guard passed 1 of 1 tests.
- The exact root fan-out parity and documented-probe-count guard passed 2 of 2
  tests.
- `go test ./internal/projector/... -count=1` passed.
- `go test -race ./internal/projector/workload/cloud ./internal/projector -count=1`
  passed.
- A normalized diff of the executable function body between the base file and
  `workload/cloud/reducer_intent.go`, substituting only the exported symbol,
  was empty.
- `go doc ./internal/projector/workload` and
  `go doc ./internal/projector/workload/cloud` passed.
- `go list` reported exactly the three intended direct production imports:
  `internal/facts`, `internal/projector/intent`, and `internal/reducer`.
- Diffs against the base for `testdata/cassettes` and
  `testdata/golden/e2e-20repo-snapshot.json` were empty.

No-Regression Evidence: the base and final production trees passed the same
focused leaf and root guards under Go 1.26.6 on Linux/amd64. Leaf input shapes
cover two ordered AWS resource facts, one blank-source fallback fact, and an
empty lookup. Each positive leaf call returns exactly one in-memory
reducer-intent value and the empty lookup returns zero. Root parity intentionally
returns the complete multi-domain intent slice, where the workload-cloud domain
retains its original position and exact value. Neither proof writes a queue or
database row. No graph backend is invoked, so a backend and version do not
apply. The normalized production function-body diff is empty, and the move
changes no query, queue, lock, storage, network, allocation, or concurrency
path; therefore there is no runtime performance theory or benchmark claim.

No-Observability-Change: the leaf emits no signal directly before or after the
move. Root intent enqueue remains visible through
`eshu_dp_reducer_intents_enqueued_total`, the `reducer_intent.enqueue` span,
`eshu_dp_projector_stage_duration_seconds{stage="intent_enqueue"}`, and the
existing structured projector log fields. Shared per-domain reducer run
signals, input-invalid signals and result summaries, and storage graph-write
instrumentation remain unchanged. No metric, label, span, log field, status
surface, or telemetry ownership changes; only the existing coverage row's
source path was repointed.
