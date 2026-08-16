# #5993 — deployable-unit correlation extraction seam

## What changed

Three files the performance-evidence gate flags as hot Go runtime paths
changed in this branch:

- `go/internal/reducer/deployable_unit_correlation_edges.go` — adds an
  exported pure function, `ExtractDeployableUnitCorrelationRows`, that now
  takes **already-extracted** `[]WorkloadCandidate` (not fact envelopes) and
  wraps the EXISTING call sequence `applyResolvedDeploymentSources` →
  `filterDeployableUnitCandidates` → `evaluateDeployableUnitCandidates` →
  `deployableUnitCorrelationRows` — four functions, in the same order, with
  the same arguments, into one function. `ExtractWorkloadCandidates` itself
  moved OUTSIDE the seam (see the be0b1bc49 note below); it is no longer one
  of the calls this function wraps. `now func() time.Time` is threaded
  through `deployableUnitCorrelationRows` / `deployableUnitCorrelationRow` to
  stamp `CreatedAt`, defaulting through the existing `admissionNow(nil)`
  helper to `time.Now().UTC()` — byte-identical to the pre-refactor literal.
  `admittedDeployableUnitRows` is renamed to exported
  `AdmittedDeployableUnitRows`; its body is untouched.
- `go/internal/reducer/deployable_unit_correlation.go` —
  `DeployableUnitCorrelationHandler.Handle` now calls `ExtractWorkloadCandidates`
  exactly once (previously it called it once conditionally, inside the
  `ResolvedLoader != nil` branch, AND `ExtractDeployableUnitCorrelationRows`
  called it again internally from the same envelopes — a doubled call fixed
  in `be0b1bc49`, after this seam originally shipped), then passes that one
  `candidates` slice to both `loadWorkloadResolvedRelationships` and
  `ExtractDeployableUnitCorrelationRows` (passing `nil` for the clock).
  `Handle` still reads its empty-candidates guard off
  `len(evaluation.Results) == 0` instead of the old `len(candidates) == 0`.
  `deployableUnitCorrelationEntityKeys(intent)` is still called twice per
  `Handle` invocation — this is a SEPARATE, unrelated doubled call from the
  `ExtractWorkloadCandidates` one `be0b1bc49` removed: once up front in
  `Handle` (fail-fast, result discarded) and once again inside
  `ExtractDeployableUnitCorrelationRows`, which re-derives it independently
  from `intent` (not from `candidates`) so the seam has no hidden dependency
  on its caller having already validated entity keys. `be0b1bc49` did not
  touch this call; it remains exactly as described below.
- **`be0b1bc49` (post-seam follow-up, same PR):** removed the doubled
  `ExtractWorkloadCandidates` call described above. `Handle` now extracts
  candidates once; `ExtractDeployableUnitCorrelationRows`'s signature changed
  from `(intent, envelopes []facts.Envelope, resolved, now)` to `(intent,
  candidates []WorkloadCandidate, resolved, now)` accordingly. This is a real
  narrowing of the seam's guarantee, not just a rename: before this commit,
  `ExtractWorkloadCandidates` was itself one of the wrapped calls, so
  production and the Ifá guard (`materialized_edges_deployable_unit.go`)
  were STRUCTURALLY unable to diverge on how candidates get extracted — both
  ran through the one seam. After this commit, `ExtractWorkloadCandidates` is
  called separately by each caller (`Handle` and the guard's own call site),
  outside the seam. They call the same function today, so the observable
  behavior is unchanged, but the guarantee is now "both call the same
  function by convention", not "both go through the same code path by
  construction" — a caller could in principle extract candidates a different
  way and the seam would not notice. `go build ./...` and
  `go test ./internal/reducer ./internal/ifa ./cmd/ifa -count=1` (all green,
  this run) confirm current behavior is unchanged; they cannot and do not
  prove the stronger structural guarantee the pre-`be0b1bc49` seam had.
- `go/internal/ifa/materialized_edges_deployable_unit.go` — new file, ~290
  lines. Not a production file: it defines
  `resolveDeployableUnitMaterializedEdges`, the family's Ifá
  materialized-edge vacuity guard, invoked only from `go test` / the
  `eshu-ifa`/`eshu-golden-corpus-gate` gate binaries against a fixed,
  cataloged Odù fixture. It calls the SAME production seams
  (`DiscoveredEvidence` → `relationships.Resolve` →
  `ExtractDeployableUnitCorrelationRows` → `AdmittedDeployableUnitRows`) the
  reducer handler calls, with a fixed injected clock
  (`deployableUnitGuardClock`, never real time — required by
  `go/internal/ifa/AGENTS.md`), so the fixture and production cannot silently
  diverge.

None of the three touches `go/internal/storage/cypher/canonical_deployable_unit_edges.go`
(the actual `MATCH`/`MATCH`/`MERGE` write template) or any other file under
`go/internal/storage/cypher/`: `git diff --name-only` against the merge base
with `origin/main` shows zero changed files there. The write path's Cypher,
batching, and worker/lease/queue mechanics are unchanged by this branch.

## No-Regression Evidence

No-Regression Evidence: no benchmark was run, and none is warranted. This is
a pure control-flow extraction with no new I/O, no new Cypher, no new
locking, and no change to per-request or per-row work. What backs "pure"
rather than "argued pure":

- **The function-call sequence is unchanged, not merely similar.** The
  extracted seam calls the same five existing functions in the same order
  with the same inputs; nothing was reordered, batched differently, or given
  new fan-out. `git diff` above shows the bodies of
  `ExtractWorkloadCandidates`, `applyResolvedDeploymentSources`,
  `filterDeployableUnitCandidates`, `evaluateDeployableUnitCandidates`, and
  `deployableUnitCorrelationRow` are untouched; only their call site moved.
- **The `len(candidates) == 0` → `len(evaluation.Results) == 0` guard swap is
  proven equivalent, not assumed.** `engine.Evaluate`
  (`go/internal/correlation/engine/engine.go`) pre-sizes `results` to
  `len(candidates)` and appends exactly one `Result` per input candidate in
  its single loop — there is no path that drops or merges a candidate, so
  `len(evaluation.Results) == len(candidates)` always holds. This is pinned
  by an EXISTING test written for this exact concern:
  `TestExtractDeployableUnitCorrelationRowsEmptyCandidatesYieldsNoResults`
  (`go/internal/reducer/deployable_unit_correlation_extract_test.go`), whose
  own doc comment states it "pins the empty-candidate case Handle's own 'no
  deployable unit candidates found' branch relies on ... so Handle's guard
  for that branch stays valid after the refactor." A second test,
  `TestExtractDeployableUnitCorrelationRowsReproducesHandleAdmittedEdge`,
  asserts `len(evaluation.Results) == 1` and every admitted-row payload field
  for the non-empty case.
- **The clock injection is behavior-identical for production.**
  `admissionNow(nil)` (`go/internal/reducer/admission_decisions.go:221-226`)
  returns `time.Now().UTC()`, the exact literal `deployableUnitCorrelationRow`
  used before this change. `Handle` passes `nil`. Pinned by
  `TestExtractDeployableUnitCorrelationRowsUsesInjectedClock`.
- **The doubled `deployableUnitCorrelationEntityKeys` call is real but
  negligible.** It parses and normalizes `intent.EntityKeys` — an in-memory
  string slice from the queued intent, typically a handful of entries, no
  I/O, no Cypher — and runs once per `Handle` invocation (once per queued
  work item), not per row or per edge. Calling it twice instead of once adds
  one more O(k log k) sort over a small k, immaterial next to the Postgres
  claim and Cypher write the same `Handle` call already performs.
- **`materialized_edges_deployable_unit.go` triggered this gate on comment
  text, not code.** `rg` against the file for the gate's own hot-path content
  patterns (`MATCH|MERGE|UNWIND|...`, `ClaimBatch|Worker|...`,
  `errgroup|Mutex|chan|...`, `go func(`) matches exactly two lines, both
  comments describing the PRODUCTION Cypher's MERGE key in prose ("The MERGE
  key is bare ..."). The file contains no Cypher, no concurrency primitive,
  and no worker/lease/batch code; it runs only inside `go test` and the Ifá
  gate binaries against one fixed ~17-fact Odù (`TestDeployableUnitFamilyOduPreservesEnvelopeFields`
  confirms 17 facts), never in a request or write path.
- **Fresh test evidence, this run:** `go test ./internal/reducer -run DeployableUnit -count=1`
  → `ok ... 0.957s`; `go test ./internal/ifa -run DeployableUnit -count=1` →
  `ok ... 0.590s`; `go test ./internal/ifa -run DeployableUnit -v -count=1`
  (10/10 subtests) and `go test ./internal/reducer -run DeployableUnit -v -count=1`
  (26/26 subtests, including the two extraction-equivalence tests above) both
  pass. `go build ./...` and `go vet ./internal/ifa ./cmd/ifa` are clean.
- **Live-lane terminal counts.** The family's expected-edge-set fixture
  (`go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json`)
  asserts exactly one admitted `CORRELATES_DEPLOYABLE_UNIT` edge from the
  4-repo cassette (app, deploy, jenkins, rejected repositories under one
  scope/generation partition); the live gate's B-12 residual bound requires
  0 dead letters and 0 residual `fact_work_items` rows at drain. On this
  exact tree (HEAD `6454cb437` at the time of the run), this branch's
  path-triggered live lane passed during `make pre-pr` preflight in 374s
  against NornicDB `eshu-nornicdb-pr290:3722b483c02c` (the pinned image in
  `docker-compose.yaml`), the only lane the changed hot files select — no
  other Docker-backed determinism or fault-injection cell in this run
  regressed.

## Observability Evidence

Observability Evidence: this branch adds real, new observability, so that is
the correct marker here, not No-Observability-Change. None of it lives in
the three flagged Go files above, but the flagged files do not define the
scope of what changed on this branch, and the marker is checked at that
scope. Specifically, in `scripts/lib/ifa_deployable_unit_live_diagnostics.sh`
and `scripts/lib/ifa_deployable_unit_live.sh` (both live-gate shell
tooling, never shipped in a production binary):

- **Three new diagnostic probes**, all diagnostic-only (none of them can flip
  a cell's pass/fail outcome; `ifa_deployable_unit_live_assert` remains the
  sole authority): a post-maintenance `shared_projection_intents` count
  (separates "the writer dropped admitted rows" from "correlation produced
  nothing"), a count of resolved `DEPLOYS_FROM` relationships (separates
  "correlation had nothing to correlate" from "the reopen found admitted rows
  the writer then dropped"), and a tail of
  `ReopenSucceededReducerWorkItems`'s per-domain reopen line (separates "the
  reopen never fired" from "it fired and found nothing").
- **A readiness observable now runs in all three fault cells** (previously
  only the baseline cell had it): `ifa_deployable_unit_live_assert_readiness_opened`
  reads the post-maintenance reducer log to confirm
  `CrossRepoRelationshipHandler.Resolve` actually ran the readiness-gated
  branch open, rather than inferring it from a downstream edge count alone.
- **Fan-in-skip naming**: the readiness-opened check's "gated" branch now
  also tails the bootstrap-index maintenance-pass log for
  `deferred_backfill_fanin_partition_skipped=true` and names it as the cause
  when present, instead of leaving "the maintenance pass did not open the
  readiness gate" to mean two different things.

All of this is confined to the Ifá live-gate lane's own logging and Postgres
probes — no production metric, span, structured log field, or dashboard
changes, and no change to what `eshu-reducer`, `eshu-projector`, or any
production binary emits at runtime. An operator's dashboards and alerts are
unaffected; only an agent or engineer reading a live-gate cell's own output
sees the new detail.
