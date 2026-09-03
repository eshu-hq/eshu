# AGENTS.md — code-function-summary projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the code-interproc-evidence probe and before the IAM CAN_ASSUME
   probe.
5. `go/internal/reducer/code_function_summary_materialization.go` for what the
   reducer does with the intent this package enqueues, including typed
   decode, quarantine, and durable summary/source/graph-id persistence.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCodeFunctionSummaryReducerIntent` fires on a `code_function_summary`
  finding, else on the `code_dataflow_scanned` marker. A finding always
  outranks the marker regardless of input order — the two kinds are looked up
  independently via `FirstOfKind`, and there is deliberately no cross-kind
  original-order merge. Do not "fix" this to `FirstAcrossKinds`: it changes
  `FactID`/`Reason` provenance for generations carrying both kinds.
- The payload's `repo_id` fallback (marker's `repo_id` when the winning
  trigger's own decode resolves to `""` and both facts are present) is a
  deliberate two-step lookup, not a bug. `codeFunctionSummaryTriggerRepoID`
  is the single seam that must stay in sync with both branches; do not inline
  a duplicate decode elsewhere.
- `full_snapshot` is keyed on marker *presence*, not on which fact won
  provenance. Do not gate it on `reason` or on `trigger.FactKind`.
- Keep the two reason strings and the `code_function_summary:<scope>` entity
  key byte-identical. The reducer claims one intent per scope generation and
  reloads the generation's facts itself; the root fan-out parity fixture pins
  the marker-path and summary-path values, including the payload.
- `SourceSystem` is the trimmed `CollectorKind` alone — a single tier. Do NOT
  substitute the two-tier `projectorintent.SourceSystem`: it prefers
  `SourceRef.SourceSystem` when set and silently relabels the intent. The
  package test `TestBuildCodeFunctionSummaryReducerIntentTrimsCollectorKind`
  fails on exactly that substitution.
- This family DOES decode a payload, unlike `codeinterprocevidence` and
  `codetaintevidence`. `factschema_decode_codedataflow.go` is the seam;
  `scripts/verify-payload-usage-manifest.sh` AST-scans it for the
  `factschema.FactKindCodeFunctionSummary` /
  `factschema.FactKindCodeDataflowScanned` references, so keep those
  references in the decode function bodies even if the surrounding code is
  refactored.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing a reason string or the entity key.** Both are asserted verbatim by
  the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them together.
- **Changing the repo_id or full_snapshot payload rule.** Update
  `TestBuildCodeFunctionSummaryReducerIntentFallsBackToMarkerRepoIDWhenSummaryRepoIDUnresolvable`
  and the root fan-out parity fixture's `DomainCodeFunctionSummary` payload
  expectation together — both pin the same two-step derivation.
- **Adding a trigger kind.** Decide explicitly whether it joins the finding
  tier or the marker tier, keep the finding-over-marker rule, and update the
  child tests plus the root dispatcher fixtures.

## Failure modes

- **Route-serves-data registry path citations.** The registry in
  `go/internal/mcp/route_serves_data_registry_routes*.go` cites projector
  source files by path and greps them for a marker, so a rename breaks a test
  two packages away with a `read ...: no such file` error that never names
  this package. No registry entry cites `code_function_summary_intents.go`
  today (checked with `rg -n 'code_function_summary_intents' go/internal/mcp/*.go`,
  zero matches, run before this extraction landed) — the mandatory
  `go test ./internal/mcp/ -run TestRouteServesDataRegistry -count=1` gate
  re-verifies this on every change here.
- **A trigger fact with no collector kind.** The single-tier label yields an
  empty `SourceSystem` rather than falling back to `SourceRef.SourceSystem`
  or a literal default. That is the preserved pre-extraction behavior, not a
  bug to patch in passing.
- **Root dispatcher tests live outside this directory.** The fan-out order
  and payload-parity cases for this domain are at root in
  `../scope_generation_intents_fanout_test.go` and
  `../scope_generation_intents_fanout_parity_test.go`. A change here can
  break them without touching any file in this directory.

## Anti-patterns

- Do not add a package-local source-system helper that forwards to
  `projectorintent.SourceSystem`; the two-tier fallback is a behavior change
  here, not a cleanup.
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past `BuildCodeFunctionSummaryReducerIntent`.
  Every sibling family in this series exports exactly one builder and no
  exported types.
- Do not drop `repoIDFromFunctionID` down to a bare `strings.Cut`/`Split`
  without the trailing `strings.TrimSpace`: the function_id encoding uses a
  `\x1f` unit separator, not a printable delimiter, and the prefix is not
  guaranteed pre-trimmed by every producer.
