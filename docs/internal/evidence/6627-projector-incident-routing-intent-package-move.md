# #6627 incident-routing projector package move

## Scope

Base: `15f09ccdcc45c0534b24245539aa0f82d1bc4b92`

This slice moves the PagerDuty incident-routing reducer-intent builder from
`internal/projector/incidentrouting` to
`internal/projector/incident/routing`, renames
`materialization_intents.go` to `reducer_intent.go`, and shortens the exported
API from `BuildIncidentRoutingMaterializationReducerIntent` to
`BuildReducerIntent`. The production body is otherwise unchanged.

The `internal/projector/incident` package is documentation-only. The routing
leaf still imports Eshu's internal facts, projector-intent, and reducer-domain
contracts, and root projector assembly remains its only production caller.
This is a clearer ownership seam for a future repository split, not a claim
that the package is independently extractable today.

## Rebased inventory and dependency edges

The affected leaf at the rebased base contained five files: its package-doc
trio, one non-test implementation file, and one test file. The destination
leaf contains the same five responsibilities. The new parent adds only its
required package-doc trio, including a declaration-only `doc.go`; root
projector non-test file count is unchanged.

Before and after the move, production imports are exactly `internal/facts`,
`internal/projector/intent`, and `internal/reducer`. The leaf test imports the
same three repository packages plus `reflect`, `testing`, and `time`. The sole
production reverse importer before and after path substitution is
`internal/projector/scope_generation_intents.go`; no test imports the leaf.
The destination adds no root-projector, sibling, storage, transport, or
runtime-implementation dependency, and no old-path forwarding package remains.

The base is the merged result of the preceding cloud projector slice; no
intervening merge changed the incident-routing leaf or its dependency edges.

## Preserved contracts

- One intent is emitted per scope generation containing `incident.record` or
  one of the five routing kinds returned by `facts.IncidentRoutingFactKinds`
  at the base commit.
- The earliest candidate in original input order remains the anchor.
- Domain, entity key, reason, fact ID, and two-tier source-system selection are
  unchanged.
- Root fan-out remains 44 probes; routing remains after observability coverage
  correlation and before code taint evidence.
- Root owns assembly, queueing, retries, and enqueue telemetry. The reducer
  owns comparison, graph materialization, readiness, and runtime telemetry.
- No public HTTP or MCP API, payload, fact, reducer-domain, queue, graph,
  telemetry, or golden corpus contract changes; the internal Go API rename is
  described above.

## TDD evidence

Before the move, the focused leaf guard passed 1 of 1 tests and the exact root
dispatch, parity, and documented-probe-count guard passed 5 of 5 tests.

The destination test was then moved first, changed to `package routing`, and
updated to call `BuildReducerIntent`. Running
`scripts/go-test-run-guard.sh 1 'TestBuildReducerIntent' -- ./internal/projector/incident/routing -count=1`
failed with five `undefined: BuildReducerIntent` compile errors and exit 1.
That compile-contract failure is the red phase for the path and API move.

## Final verification

All commands below completed with exit 0 after the final production edit:

- `scripts/go-test-run-guard.sh 1 '^TestBuildReducerIntent$' -- ./internal/projector/incident/routing -count=1`
  passed 1 of 1 tests.
- The exact root guard covering three dispatch tests, fan-out parity, and the
  documented probe count passed 5 of 5 tests.
- `go test ./internal/projector/... -count=1` passed.
- `go test -race ./internal/projector/incident/routing ./internal/projector -count=1`
  passed.
- A normalized diff of the executable function body between the base
  production file and `incident/routing/reducer_intent.go`, substituting only
  the exported symbol, was empty.
- `go doc ./internal/projector/incident` and
  `go doc ./internal/projector/incident/routing` passed.
- `go list` reported exactly the three intended direct production imports:
  `internal/facts`, `internal/projector/intent`, and `internal/reducer`.
- Diffs against the base for `testdata/cassettes` and
  `testdata/golden/e2e-20repo-snapshot.json` were empty.
- Package docs, telemetry coverage, payload usage, documentation citations,
  documentation references, dirgate, Markdown line-cap, and `git diff --check`
  verification passed.
- The strict MkDocs build and whole-module `go vet ./...` passed.

No-Regression Evidence: the baseline at
`15f09ccdcc45c0534b24245539aa0f82d1bc4b92` and the final production tree both
passed the focused leaf guard with the same one test and the root dispatch,
parity, and probe-count guard with the same five tests under Go 1.26.6 on
Linux/amd64. The input shapes cover two
incident records, one fact for each of the five registered routing kinds, a
cross-kind ordering pair, a blank-source fallback, and one unrelated fact. Each
positive leaf-builder invocation returns exactly one in-memory reducer-intent
value, and its negative case returns zero. Each positive root dispatch case
contains exactly one incident-routing-domain intent; the fan-out parity guard
intentionally returns the complete multi-domain intent slice. Neither baseline
nor after proof writes a queue or database row. No graph backend is invoked, so
a backend and version do not apply. The normalized production function-body
diff is empty, and the move changes no query, queue, lock, storage, network,
allocation, or concurrency path; therefore there is no runtime performance
theory or benchmark claim.

No-Observability-Change: the leaf emits no signal directly before or after the
move. Root intent enqueue remains visible through
`eshu_dp_reducer_intents_enqueued_total`, the `reducer_intent.enqueue` span,
`eshu_dp_projector_stage_duration_seconds{stage="intent_enqueue"}`, and the
existing structured projector log fields. Reducer execution remains visible
through `eshu_dp_incident_routing_evidence_total` and the shared reducer
status, queue, and processing signals. No metric, label, span, log field,
status surface, or telemetry ownership changes; only the existing coverage
row's source path was repointed.
