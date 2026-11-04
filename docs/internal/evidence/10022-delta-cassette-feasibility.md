# #10022 Delta-Reader Golden Leg: Feasibility and Design

#10022 asked for a delta-active git cassette in `testdata/cassettes/`. A
cassette can't express a delta generation, so the golden corpus now gets its
delta from a fixture repository. eshu-collector-git collects that repository
from a git origin twice: once as a full and once as a real delta.

## Why a cassette cannot carry a delta

- `go/internal/replay/cassette/source.go` (`collectScope`) builds every
  replayed `scope.ScopeGeneration` with `IsDelta` unset. The cassette format
  has no field for it.
- `scope_generations.is_delta` is written only from `ScopeGeneration.IsDelta`
  (`go/internal/storage/postgres/ingestion_queries.go`, the
  `generation.IsDelta` bind). A `delta_generation` marker on the repository
  fact alone does not make the generation a delta.
- The rationale and sqlrelationship "delta cassettes" are Ifá family fixtures.
  They drive in-process reducer proofs, not the golden corpus run.
- Three of the affected readers (code-call symbols, the Crossplane XRD read,
  and the Terraform loaders) consume git parser facts. Hand-written
  `file`/`content_entity` payloads would assert a guess about parser output,
  and a parser shape change would make them silently inert.

## Why the golden runner produced no git delta

- Every corpus repository is collected by `eshu-bootstrap-index` in
  filesystem mode. `NativeRepositorySelector` passes `nil` deltas for
  filesystem mode (`go/internal/collector/repo/git/selection_native.go`,
  `case "filesystem"`). The changed-since fixture mutation therefore lands as
  a new full generation.
- `eshu-bootstrap-index` builds its selector without a `BaselineResolver`
  (`go/cmd/bootstrap-index/wiring.go`). Even in explicit mode its sync sees
  no active commit and takes a full snapshot (`updateRepository`,
  `case active == ""`).
- Only `eshu-collector-git` and `eshu-ingester` wire the resolver.

So the `delta-union` phase skipped every scope ("active generation is not a
delta") and passed vacuously, as its own doc comment says.

## Shim proof (before any gate code)

Disposable PostgreSQL 18 (`postgres:18`, private container on port 35471,
schema from `eshu-bootstrap-data-plane`). A bare origin was served through
`GIT_CONFIG_GLOBAL` with `[url "file://<dir>/origins/"] insteadOf =
https://github.com/`, the #7389 remote-run transport. `eshu-collector-git` ran
with `ESHU_REPO_SOURCE_MODE=explicit`,
`ESHU_REPOSITORY_RULES_JSON=[{"exact":"acme/delta-repo"}]`, and
`ESHU_GIT_AUTH_METHOD=none`:

1. The first sync committed a full generation (`is_delta=f`,
   `source_commit_sha` = commit A).
2. The base was marked active (`status='active'`, `activated_at`, and
   `ingestion_scopes.active_generation_id`), the work the projector does
   live. One file was then changed in commit B.
3. The second sync committed `is_delta=t` with `delta_baseline_commit_sha`
   and `delta_union_base_commit_sha` = A. Its repository fact carried
   `delta_generation=true` and `delta_relative_paths=["cmd/foo.go"]`, plus
   facts for that file only.

The same run against the committed fixture
(`tests/fixtures/golden_delta_union`) through the shipped lib gave scope
`git-repository-scope:repository:r_0c077ab3`, base commit `f94adaec`, delta
commit `c370dd26`, and `delta_relative_paths=["cmd/main.go"]` (the first
fixture layout; see the fixture section for today's). The scope id
is deterministic because `CanonicalRepositoryID` hashes the synthesized remote
`https://github.com/acme/golden-delta-reader`, not the mktemp checkout path.

## Design

- **Fixture.** `base/` holds `app/main.go` (`deltaReaderRun` calls a
  same-file helper and two helpers in `app/ledger.go` and `app/report.go`), a
  Crossplane XRD and a matching claim under `deploy/`, and a `go.mod` with no
  `go` directive and no requires, plus `app/moved.go`, whose caller the
  delta moves. The delta appends a comment to `app/main.go`, so no function
  moves there, inserts lines above the `app/moved.go` caller, and changes one
  spec value in `deploy/claim.yaml`. The caller is not named `main`: that name collides
  with the existing `code/search` and `resolve_entity` shapes that query
  `main`.
- **Orchestration.** `scripts/lib/golden-corpus-delta-reader.sh` collects the
  full after the first drain and before the maintenance passes. The first
  live run collected it before the first drain, and that drain wedged at a
  constant residual of 46 work items. The fixture scope's `deployment_mapping` was
  retrying `cross_repo_backward_evidence_not_ready`. The reducer log showed
  why: `activation obligation held: maintenance refused: catalog_changed:
  partition-scoped maintenance refused: repository catalog changed since
  active backward evidence was committed`. A brand-new repository changes the
  catalog, so only a corpus-wide pass (bootstrap-index's maintenance pass)
  publishes its backward evidence. After the maintenance drains, inside its own
  `graph_query` exclusion bracket, it asserts stage `full`, commits `delta/`,
  syncs the delta, waits for the projector to activate it, and drains once.
  The final gate invocation asserts stage `delta`.
- **Assertions.** `graph.delta_reader_assertions` in the B-12 snapshot,
  evaluated by `goldengate.EvaluateDeltaReaderAssertions`. An
  `expected_failure` edge must be absent at stage `delta`; a surviving one is
  a required XPASS failure. Stage `full` requires every edge, so a marker
  can't pass an edge that was never produced.

## What the live runs found

Each run is the Neo4j golden gate on this branch. The stage `full` check
passed in every run that reached it.

| Run | Fixture shape | Stage `delta` result |
| --- | --- | --- |
| 1 | full collected before the first drain | not reached: the first drain wedged (`catalog_changed`, above) |
| 2 | caller `main` in `cmd/main.go`, delta adds a comment above it | all three CALLS edges from `main` lost, including the same-file control; SATISFIED_BY survived (XPASS); `main` collided with the `code/search` and `resolve_entity` shapes; Workload 11 > 10 |
| 3 | caller `deltaReaderRun`, trailing-comment delta, claim re-emitted | all three CALLS edges survived (dr-2 and dr-3 XPASS); SATISFIED_BY lost; Workload 11 > 10 |

Run 3's database explains both code-call results. The delta generation had no
`code_call_materialization` work item and no `code_calls` intent, while the
full generation had both. `fact_builder.go` returns before it emits the
code-call follow-up for a delta, and `CanonicalEntityID` hashes the line
number. So a delta that moves a function re-keys its node and loses its edges
with no rewrite (filed as #10049), and a delta that moves nothing leaves the
full generation's edges in place. The #10003 row 1 symbol read is not reachable
through a git delta at all. The fixture therefore keeps dr-1 to dr-3 as plain
guards and adds dr-5, a moved caller, as an expected failure for #10049.

## Readers not expressed, and why

| Reader | Defect | Reason |
| --- | --- | --- |
| code-call symbol read | #10003 | Not reachable through a git delta: no code-call work runs for a delta generation (run 3, above). |
| manifest dependency read | #9991 | Fixed by #10018 (merged 2026-11-04) and proven by its live Postgres regressions. The handoff's premise ("until #10018 lands") no longer holds. Its graph observables are pinned by existing shapes: package-consumption `DEPENDS_ON` to lib-common is pinned by `get_code_relationship_story` (max 1) and `find_blast_radius` (max 10), and CVE-2026-00000 supply-chain findings by their own shapes. A second consumer would move unrelated assertions. |
| Crossplane claim-side retract | #10005 | Needs a delta that adds a claim beside an unchanged one; this fixture has one claim, which the delta re-emits. |
| Terraform config/backend loaders | #10006 | The observable is config-state drift, which needs a `terraform_state` cassette scope anchored to this repository's backend. |
| JVM reachability | #10003 | No JVM package in the fixture. |
| owned-package targets | #10007 | Collector target planning, not graph truth; lane 2's zone. |
