# #8875 C6: Claim-Path Truth-Preservation Scope + C4 Fact-Kind Census Refresh

Lane: `drive/8875-truth-census` (test + census only; no production change).
Status: scoped proof committed; promotion stays with the coordinator.

## C6 verdict: the claim path cannot affect reducer/graph/API/MCP fact truth

Established by code inspection at `origin/main` (`561a1b369b`) with `git grep`
as the census authority (host `rg` is below the 15.2.0 census floor, so every
count below is a `git grep` result; the committed guard test re-checks the
same properties with `go/parser` at test time).

1. The moving leaf emits no facts. Zero production imports of any
   fact-definition, decode, or fact-persistence package from
   `go/internal/workflow/` or `go/cmd/workflow-coordinator/`:
   `git grep -n 'eshu/go/internal/facts"|eshu/go/internal/storage/postgres/facts|factschema"'`
   over both trees returns nothing outside `_test.go` (the single in-leaf
   hit is `parity/commit_fence_test.go`, test-only).
2. Coordinator planners use facts as an identity function only.
   Production code under `go/internal/coordinator/` references `facts.*`
   solely as `facts.StableID(...)` to mint workflow generation-ID strings;
   zero `facts.Envelope` / `facts.Ref` constructions outside `_test.go`
   (the envelope hits are confined to
   `vulnerability/intelligence_derivation_sbt_test.go`). No coordinator
   file writes `fact_records` or imports a fact-persistence package.
3. No projection reads workflow state. `go/internal/graph/` has zero
   references to `internal/workflow` or any `workflow_*` table.
   `go/internal/reducer/` has one non-test hit:
   `retirement/census.go`, a disposition census (not a projection input)
   recording that `workflow_work_items` is kept because a later claim for
   a retired repo is refused at commit.
4. The remaining touches are transport, not truth content:
   - `go/internal/query/` (4 files): read-only collection-state
     explanation SQL / status text
     (`security_alert_collection_state_postgres.go`,
     `container_image_candidate_explanation.go`,
     `openapi/components_active_work_source.go`,
     `supply/chain/impact/readiness_postgres_query_targets.go`).
   - `go/cmd/api/metrics_source.go`: parses `DesiredCollectorInstance`
     JSON for metrics-source selection (config shape only).
   - `go/internal/mcp/`: zero references.
   - `go/internal/workflow/progress.go` reads reducer-owned counts
     (`CollectorRunProgress`) for completeness derivation — workflow reads
     reducer truth; the arrow points away from the move.
5. The one write coupling is a commit gate, not content. The ingestion
   transaction heartbeats the claim in-transaction
   (`storage/postgres/ingestion.go`, `NewWorkflowControlStore(tx)` +
   `HeartbeatClaim`): it can refuse a stale commit but cannot alter
   admitted fact content. Its cross-service replacement
   (`ValidateAndPinClaimForCommit`) is proven under #9376, not here.

Committed proof: `go/internal/workflow/claim_truth_scope_test.go` —
`TestClaimPathEmitsNoFacts`, `TestCoordinatorFactsUsageIsIdentityOnly`,
`TestCoordinatorImportsNoFactPersistence`,
`TestTruthSurfacesReadNoWorkflowState` (six-file known-reader allowlist,
one per item above). Each test was mutation-proven: a planted facts
import, a planted `facts.Envelope` construction, a planted
fact-persistence import in the coordinator, and a planted
`workflow_runs` reference each fail exactly their guard, then were
removed. Any future change outside the documented reader set that
introduces such a coupling fails closed and must update the test and
this note together (couplings added inside an allowlisted file are
reviewed, not auto-failed).

## C4 census refresh: 187 → 188 fact Kinds

`rg -c '\{Kind:' go/internal/facts/fact_kind_registry.generated.go` = 188.
The drift is commit `5958727005` (2026-11-04, `#9777`):
`vulnerability.changed_name` v1.0.0 — one fact per changed
(ecosystem, package name) pair from the OSV `modified_id.csv` feed, no
emitter or consumer yet. Unrelated to the workflow lane.
`go/cmd/workflow-coordinator/EXTRACTION_PREP.md` §§4/11 refreshed to 188
in this lane. Importer census NOT refreshed here (owned by the census
lane): re-measured at this HEAD with the census method
(`git grep -l -F 'eshu/go/internal/workflow"' -- 'go/**/*.go' ':!*_test.go'`;
169 at `8ec6d11ddf`, 202 at this HEAD, +33 from merged lanes since
2026-10-30) — `EXTRACTION_PREP.md` §5 still records 169 and needs its own
refresh pass.

## Proof (this worktree, post-final-edit)

- `go test ./internal/workflow/ -run 'TestClaimPath|TestCoordinatorFacts|TestTruthSurfaces' -count=1`: PASS (3/3).
- Mutation RED check per guard above: each planted coupling FAILED its
  guard; probes removed afterwards (`git status` clean except the three
  lane files).
- `go vet ./internal/workflow/`: clean. `gofmt` applied to the new test.
- Full package suite + selected static gates run before handoff; see the
  lane report.
