# Evidence: #5810 — cross-scope CI-run bridge for `DERIVED_FROM` build provenance

Issue #5810: `DERIVED_FROM` gates its child on `BuildProvenanceRepositoryIDs`,
but a repository-scoped refresh never saw `ci.run`/`ci.artifact` facts (they
are written by the `ci_cd_run` collector in the CI run's own, different
scope), so the CI-run tier of `DERIVED_FROM` could never materialize outside
a same-scope unit test that bypasses real scope separation. This branch adds:

- `go/internal/reducer/container_image_identity_ci_loader.go` —
  `activeContainerImageCIFactLoader`/`loadActiveContainerImageCIFacts`, the
  cross-scope bridge, mirroring the existing `activeContainerImageSLSAFactLoader`
  pattern (#5456 PR #5707 P1-b).
- `go/internal/storage/postgres/facts_active_container_image_ci.go` +
  `migrations/079_fact_records_active_container_image_ci_idx.sql` — the
  dedicated Postgres query and partial index the loader runs, deliberately
  NOT folded into `identityFactFilterSQL`/`fact_records_identity_epoch_idx`
  because `ci.run`/`ci.artifact` is the highest-churn fact family in the
  system and would defeat that cache's drift-locked fingerprint on every CI
  run (see the migration's own header comment and
  `facts_active_container_image_slsa.go`'s prior-art doc comment for the same
  reasoning).
- `go/internal/reducer/container_image_identity_ref_parsing.go`,
  `container_image_identity_slsa_refs.go` — pure in-process helpers split out
  of `container_image_identity.go`/`container_image_identity_evidence.go` for
  the repository's 500-line-per-file cap; see the telemetry-coverage rows for
  those files (`docs/public/observability/telemetry-coverage.md`) for why they
  carry no independent metric.

This is a **Mandatory Prove-The-Theory-First** change: it adds both a hot-path
cross-scope Postgres query and a new partial index (migration 079). The
theory — that the index lets the planner switch from a full/near-full
`fact_records` scan to a targeted bitmap scan once the table is large and a
meaningful fraction of scopes are CI-run scopes — was proven with `EXPLAIN
(ANALYZE, BUFFERS)` against a throwaway, representative worst-case partition
**before** this loader was written.

## What is cited here vs. what this session reproduced

The `EXPLAIN (ANALYZE, BUFFERS)` measurement below (~2.12M `fact_records`,
500,000 active CI-run scopes) is a **prior measurement**, produced earlier in
this issue's investigation on a throwaway, non-persistent Postgres instance
that no longer exists — it is cited here, not re-run in this session, because
recreating a 2.12M-row synthetic partition is expensive and the theory-proof
step (per `docs/internal/agent-guide.md` Mandatory Prove-The-Theory-First) is
required to precede the code, not to be re-derived at evidence-writing time.
What this session DID independently verify on the actual branch and cites
below under "Local proof (this session)":

- the migration ships, is syntactically valid, and its predicate matches
  `listActiveContainerImageCIFactsFilterSQL` byte-for-byte
  (`TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL`);
- the loader's Go behavior (active-generation scoping, tombstone exclusion,
  `artifact_type` narrowing, pagination) is correct against a real Postgres
  instance;
- the accuracy fix this branch depends on (the CI-run "competing decision"
  path symmetrically populating `BuildProvenanceRepositoryIDs`, not only
  `SourceRepositoryIDs` — see the failing-then-green proof in this same PR)
  is proven with a real reducer-level regression test;
- the live B-7 golden-corpus gate (run by the orchestrator, not this session)
  already proved `rc-170: (ContainerImageIdentity)-[:DERIVED_FROM]->
  (ContainerImageIdentity) count=2, want >= 2` PASS, with `rc-165`/`rc-167`
  also PASS and `residual=0 dead_letter=0` — i.e. the loader's cross-scope
  query itself performs correctly end to end against the real 20-repo corpus
  and reducer runtime, independent of the synthetic index-scale claim below.

This doc does **not** independently re-verify the 6.5x number on this exact
branch, on this exact machine, today. What it does verify is that the shape
of the theory (the predicate, the index definition, the query the loader
issues) is unchanged from what was measured, and that the loader's
correctness is proven fresh. If a future reviewer needs the number
re-derived from scratch on a new machine, the seeding/measurement recipe
below is reproducible.

Benchmark Evidence: `EXPLAIN (ANALYZE, BUFFERS)` against a throwaway 500,000
active-CI-run-scope / ~2.12M-`fact_records` partition, cited below —
1292.650 ms (~2.27M buffers, full-table-scale scan) without
`fact_records_active_container_image_ci_idx` versus 199.588 ms (~805K
buffers, Bitmap Index Scan) with it (~6.5x), with exact-equivalence confirmed
by identical `count(*)` and ordered `md5(string_agg(fact_id))` over a
100,000-row sample. See "What is cited here vs. what this session
reproduced" above for the honest scope of this claim, and "Local proof (this
session)" below for what was independently re-verified on this branch.

## Theory (prior measurement, cited)

- `machine_profile`: prior investigation session, not re-captured with this
  evidence note (see limitation above).
- Postgres: 18, throwaway/non-persistent instance, destroyed after the run.
- `absolute_target_applicable`: false — this is a relative before/after plan
  shim gating an index-adoption decision, not a reference-profile wall-clock
  target.

### Seeded worst-case partition

~2,120,000 `fact_records` rows total, with 500,000 of the platform's
`ingestion_scopes` marked as active CI-run scopes (i.e. a corpus where a
meaningful fraction — not a rare minority — of all fact rows are `ci.run`/
`ci.artifact` rows matching `listActiveContainerImageCIFactsFilterSQL`), the
worst-case shape for this predicate: a large, established platform where CI
churn is a substantial share of total fact volume, matching the migration's
own stated risk ("the highest-churn fact family in the system").

### Query under test

The exact `listActiveContainerImageCIFactsQuery` text
(`go/internal/storage/postgres/facts_active_container_image_ci.go`), a single
unbounded page (`$1`/`$2` cursor NULL, `$3` = `listFactsByKindPageSize`).

### Baseline (no `fact_records_active_container_image_ci_idx`)

Planner used a scan across the bulk of `fact_records` to find the matching
`ci.run`/`ci.artifact` rows (no targeted partial index available), joined
against `ingestion_scopes`/`scope_generations`:

- Execution time: **1292.650 ms**
- Buffers: **~2,270,000** (shared hit+read; essentially the full table)

### With `fact_records_active_container_image_ci_idx` (migration 079)

Planner switched to a **Bitmap Index Scan** on
`fact_records_active_container_image_ci_idx`:

- Execution time: **199.588 ms**
- Buffers: **~805,000**
- Speedup: **~6.5x** (1292.650 ms → 199.588 ms); buffer reduction **~2.27M →
  ~805K** (~65% fewer buffer touches)

### Exact-equivalence (output-preserving)

Confirmed identical result sets with vs. without the index over a 100,000-row
sample of the query's output:

- `count(*)` identical with and without the index.
- Ordered `md5(string_agg(fact_id))` over the same 100,000 rows identical
  with and without the index (proves not just the count but the exact row
  identities and ordering are unchanged).

This satisfies the Mandatory Prove-The-Theory-First output-preserving proof:
the index only changes which plan Postgres chooses, never the query's result.

### Low-cardinality behavior (index correctly ignored — no harm)

At 2,000–50,000 active CI-run scopes (a normal-to-moderately-large platform,
well below the 500,000-scope worst case above), the planner correctly does
**not** select the new index — it keeps whatever plan was already reasonable
at that scale. This is the expected, safe behavior for a partial index sized
for a worst case that has not arrived yet: it costs nothing extra to carry
when the planner declines it, matching the `content_entities_k8s_select_partial_idx`
precedent (`docs/internal/evidence/5490-k8sresource-candidate-index.md`) where
an unused index is a no-op at read time.

## Why this is safe to land now, even with an uncorroborated exact machine profile

- The index mirrors the already-shipped, already-proven-safe
  `fact_records_active_container_image_slsa_idx` (migration 075) shape
  exactly: a plain `(observed_at, fact_id)` partial index with a
  `fact_kind`/`source_system` predicate, not a novel index design.
- `CREATE INDEX CONCURRENTLY IF NOT EXISTS` (migration 079) is the same
  non-blocking, idempotent-on-reapply DDL pattern every other partial index
  in this codebase uses (see `069`/`075`/`076`/`077` for identical
  first-application/reapplication/rollback proof shape).
- The predicate is locked byte-for-byte to the Go query's own filter via
  `TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL`
  (verified fresh this session, below), so the index and the query cannot
  silently drift apart.
- The loader is read-only (a `SELECT`, no write path), so there is no
  write-amplification concern analogous to the K8sResource candidate index's
  `INCLUDE` proof — this migration adds no `INCLUDE` columns at all.

## Local proof (this session)

```
cd go && go test ./internal/storage/postgres/ -run 'ContainerImageCI|ActiveContainerImageCI' -v -count=1
--- PASS: TestFactStoreListActiveContainerImageCIFactsNarrowsArtifactType
--- PASS: TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL
--- PASS: TestFactStoreListActiveContainerImageCIFactsExcludesTombstones
--- PASS: TestFactStoreListActiveContainerImageCIFactsUsesActiveGenerations
--- PASS: TestFactStoreListActiveContainerImageCIFactsPaginates
PASS
```

`TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL` reads
the actual migration file
(`migrations/079_fact_records_active_container_image_ci_idx.sql`) and asserts
its `WHERE` clause is identical to `listActiveContainerImageCIFactsFilterSQL`,
so the index-to-query lockstep this evidence note relies on is enforced by
the build, not only documented here.

`go test ./internal/storage/postgres -count=1` (full package): PASS.

Live B-7 golden-corpus gate (run by the orchestrator on this branch, cited
for the loader's end-to-end correctness, not for this index-scale claim):

```
rc-170: (ContainerImageIdentity)-[:DERIVED_FROM]->(ContainerImageIdentity) count=2, want >= 2  PASS
rc-165, rc-167 PASS
fact_work_items_residual: residual=0 (dead_letter=0)
```

## Owner gate: cross-scope CI facts are admitted per owning repository

Adjudication follow-up on this branch (after the rc-170 proof run above): the
unfiltered loader regressed the B-12 `mcp:list_supply_chain_impact_findings`
pin `findings[].repository_id = repository:r_217415d9` to the CI builder
(`repository:r_69256c06`) — first as a blank (pre-symmetrize), then as a
flipped value. Root cause: every identity intent in every scope admitted every
active `ci.run`/`ci.artifact`, so `applyCIRunDigestRevision` folded the
foreign builder into all ten of the deploying repository's rows for
`sha256:abcdef…` (demoting them out of anchor tier A,
`supplyChainImageIdentityAnchorTier`), and `addCICDArtifactImageReference`
minted bare-digest rows for the builder's digest in unrelated scopes.

The verdict (per #5817's merged adjudication of this exact digest, which this
situation was predicted by): the finding's `repository_id` is the
runtime-joinable impact anchor — the deploying repository — and flipping it to
the builder is a semantic redefinition needing owner sign-off, not a loader
side effect. The fix is `loadActiveContainerImageCIFacts`'
owner gate + `filterContainerImageCIFactsForOwner`
(`container_image_identity_ci_loader.go`): a repository-scoped intent admits
only CI facts whose run names its OWN repository (exactly what the
owner-scoped DERIVED_FROM child gate can consume — rc-170's
container-ci-lineage shape, where run repository == owning repository, is
unaffected), and a non-repository scope admits none cross-scope (its own CI
facts arrive scope-local).

No-Regression Evidence: the gate is strictly narrowing — non-repository
scopes now skip the loader's Postgres query entirely (the highest-churn fact
family stops being loaded on every CI/OCI-scope refresh), and repository
scopes admit a subset of the previously admitted rows, so the measured 079
index path above is exercised no more than before. Correctness is pinned by
failing-then-green reducer tests
(`TestContainerImageIdentityHandlerForeignCIRunStaysOutOfRepositoryScope`,
`TestSupplyChainImpactHandlerEndToEndCrossScopeCIRunDoesNotBlankRepositoryID`
— both reproduce the live gate failure shapes and now assert the corrected
deploying-repository anchor — plus
`TestContainerImageIdentityHandlerOwnerMatchedCIRunProvenanceReachesRepositoryScope`
and the pre-existing rc-170 DERIVED_FROM Handle test for the kept path).

## No-Observability-Change

No-Observability-Change: `ListActiveContainerImageCIFacts` issues a plain
paginated Postgres `SELECT` with no new metric, span, or log field of its
own. It runs inside the same `container_image_identity` reducer execution
already covered by `eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_reducer_executions_total`, and `eshu_dp_reducer_run_duration_seconds`
(see `docs/public/observability/telemetry-coverage.md`, the new rows added
for `container_image_identity_ci_loader.go`,
`container_image_identity_ref_parsing.go`, and
`container_image_identity_slsa_refs.go` in this same change).

## SLSA bare-digest synthesis restricted to the owning scope (#5810 P1 follow-up)

Codex review flagged that `activeContainerImageSLSAFactLoader`
(`loadActiveContainerImageSLSAFacts`) was left unfiltered while the CI loader
above was owner-gated: every container-image-identity intent loaded every
active, verified SLSA attestation platform-wide, and `addSLSADigestRefs`
(`container_image_identity_slsa_refs.go`) synthesized a bare-digest ref for
EVERY anchor — including one naming a repository unrelated to the currently
refreshing intent — mirroring exactly the CI anchor-flip class this branch
already fixed once.

Fix scope, deliberately narrow: only the SYNTHESIS of a brand-new bare-digest
ref (`addContainerImageDigestRef`, when no existing ref already names that
digest) is gated to require the anchor's `sourceRepositoryIDs` contain the
calling intent's own owning repository (or be empty — an anchor with no
repository attribution carries no cross-scope claim, so it is always safe).
`attachSLSADigestAnchorToExistingRefs` (enrichment of a ref the intent's OWN
evidence already raised) stays unconditional: this is the legitimate,
pre-existing #5456 PR #5707 P1-b cross-scope enrichment path — a non-repository
scope (e.g. an OCI-registry-triggered refresh) legitimately needs cross-scope
SLSA revision/tier data for a digest it already references, and this path
carries no foreign-repository-ID leak (verified: none of the existing SLSA
tests exercising this path assert `BuildProvenanceRepositoryIDs`, and
`TestContainerImageIdentityHandlerAppliesSLSATierFromCrossScopeActiveFacts`,
the specific regression proving this design, stays green after the fix).

Failing-then-green:
`TestContainerImageIdentityHandlerForeignSLSAAnchorStaysOutOfRepositoryScope`
(`container_image_identity_slsa_owner_test.go`) — red with a durable
canonical decision (`CanonicalWrites=1`) carrying the foreign repository's
`BuildProvenanceRepositoryIDs` for a digest the owning repository has zero
other evidence about; green with no decision at all for that digest.

No-Regression Evidence:
`TestContainerImageIdentityHandlerProjectsDerivedFromEdgeFromSLSAOnlyEvidenceNoRefSmuggling`
(the owner-matched positive case) and
`TestContainerImageIdentityHandlerAppliesSLSATierFromCrossScopeActiveFacts`
(the non-repository-scope enrichment case) both stay green — confirmed by
running the full `internal/reducer` suite, not just the new test in
isolation.

## CI run-artifact join key includes the fact's own scope (#5810 P1 follow-up)

Codex review flagged that `filterContainerImageCIFactsForOwner`'s join key
(`cicdRunKeyFromParts`: `provider:run_id:run_attempt`) had no scope
component, so two independent CI installations (github.com vs a self-hosted
GHES instance, or two separate self-hosted runners) whose own run-id
counters coincidentally reach the same number could join across scopes: the
owner-gate's first pass correctly restricts `ownedRunKeys` to runs naming the
owner (checked against `run.RepositoryID`), but the second pass re-derives
the same bare tuple for every candidate and admits any match — so a foreign
scope's run and artifact, sharing only the tuple, both slipped into the
owner's kept set. Downstream, `containerImageCIRuns` indexes runs by that
same bare tuple in a plain map, so the foreign run could even overwrite the
owner's own run anchor.

Fix: `cicdRunScopeKey` folds `envelope.ScopeID` into the join key, used only
inside `filterContainerImageCIFactsForOwner` (the one place cross-scope
envelopes from potentially different scopes are compared) — every other
`cicdRunKeyFromParts` caller only ever processes one scope's own envelopes at
a time, so the shared key stays untouched there.

Failing-then-green:
`TestFilterContainerImageCIFactsForOwnerRunKeyIncludesFactScope`
(`container_image_identity_ci_scope_key_test.go`) — a direct unit test on
`filterContainerImageCIFactsForOwner` with two runs sharing the same
`provider`/`run_id`/`run_attempt` tuple in two different scopes; red with the
foreign scope's artifact digest admitted into the owner's kept set, green
with only the owner's own digest kept and the foreign run excluded entirely.

## Double decode removed from filterContainerImageCIFactsForOwner (P2)

Owner review (linuxdynasty) noted `filterContainerImageCIFactsForOwner`
decoded each `ci.run` envelope twice: once in the first pass building
`ownedRunKeys`, again in the second pass checking membership.
`decodeCICDRun` is a pure function of the immutable envelope, so the second
decode always reproduced the identical result. Fixed by caching the first
pass's successful decode in a `map[string]cicdrunv1.Run` keyed by `FactID`
and reusing it in the second pass. Behavior-preserving (no test assertion
depends on decode call count), verified by the full `internal/reducer` suite
staying green, including the existing CI owner-gate regression tests and the
new scope-key test above, which exercise this function's decode path
directly.

## Owner predicate pushed into SQL (#5810 P1 follow-up)

Codex review flagged that even with the owner gate above and migration 079's
(now 081's) index, `ListActiveContainerImageCIFacts` still paginated through
**every** active `ci.run`/`ci.artifact` fact platform-wide before
`filterContainerImageCIFactsForOwner` threw nearly all of it away in Go: the
079/081 index bounds the *scan* to the CI fact family, never the *result
size* to one repository. At the documented 500,000-scope worst case that is
still an unbounded transfer-and-decode cost on every repository refresh.

### Theory 1 (REJECTED): flat WHERE + extra owner AND-clause, unindexed

First candidate: keep the existing single-scan query shape and simply AND in
`fact.payload->>'repository_id' = $1` (correlated against a materialized
`owned_runs` CTE for the `ci.artifact` arm). Measured with `EXPLAIN (ANALYZE,
BUFFERS)` against a throwaway seeded partition (below): **5261.360 ms** — 5x
**worse** than the unfiltered baseline. Cause: with no index on
`repository_id`, the correlated `EXISTS` subplan against `owned_runs`
re-executed once per `ci.artifact` row scanned (200,000 loops), each doing
its own `ingestion_scopes`/`scope_generations`/`fact_records` lookup.
Rejected — recorded here per Prove-The-Theory-First rather than silently
discarded.

### Theory 2 (REJECTED): same shape, `MATERIALIZED` CTE, no new index

Forcing `owned_runs` to `MATERIALIZED` fixed the correlated-subplan blowup
(955.698 ms, roughly the unfiltered baseline) but delivered **no real
speedup**: the planner still drove the whole query from a nested loop over
every active scope (`Gather` → `Parallel Hash Join` on
`ingestion_scopes`/`scope_generations`, 100,000 rows), because the ci.run and
ci.artifact arms stayed combined in one scan with no repository-keyed access
path to jump to. Rejected.

### Theory 3 (LANDED): two-branch `UNION ALL`, `MATERIALIZED` owned_runs, new repository_id index

Splitting the query into two independently-optimizable `UNION ALL` branches
(the `ci.run` branch driven by a new expression index on
`payload->>'repository_id'`; the `ci.artifact` branch joined back to the
tiny materialized `owned_runs` set by `scope_id` + `provider`/`run_id`/
`run_attempt`) let the planner pick a real access path for each arm. See
`listActiveContainerImageCIFactsQuery`
(`go/internal/storage/postgres/facts_active_container_image_ci.go`) and
migration 082
(`fact_records_active_container_image_ci_run_repository_idx`).

**Seeded worst-case partition** (this session, throwaway Postgres 18,
`COMPOSE_PROJECT_NAME=dbg5843`, non-default ports 15544/7476/7789 — never the
orchestrator's 15533/7475/7788):

- 100,000 active `ci_cd_run` scopes, each with one `ci.run` + two
  container-image `ci.artifact` facts (300,000 CI facts), `repository_id`
  drawn from a 20,000-repository pool so most repositories own a handful of
  runs — the realistic fan-out shape, not one row per repository.
- 700,000 padding `file` facts spread across the same scopes, for
  ~1,000,000 total `fact_records` rows (smaller than the prior 2.12M-row
  worst case, cited above, but the same order of magnitude and sufficient to
  reproduce the "unbounded scan" shape this fix targets — recorded honestly
  as a fresh, smaller-scale measurement, not a re-run of the prior number).
- One repository (`repository:r_owner_bench`) pinned to exactly one run.

**`EXPLAIN (ANALYZE, BUFFERS)`, single page (`LIMIT 500`, the real
`listFactsByKindPageSize`):**

| shape | execution time | buffers | rows returned |
| --- | ---: | ---: | ---: |
| OLD (unfiltered, current production query, one page) | 1055.371 ms | ~1.30M | 500 (of 300,000 total CI facts, 3 belong to the owner) |
| OLD (unfiltered, full unbounded scan — the cumulative cost paginating through all 300,000 CI facts) | 1063.522 ms | ~1.30M | 300,000 |
| Theory 1 (flat WHERE, no index) | 5261.360 ms | ~3.70M | 3 |
| Theory 2 (flat WHERE, `MATERIALIZED` CTE, no index) | 955.698 ms | ~1.30M | 3 |
| **Theory 3 (`UNION ALL`, `MATERIALIZED` CTE, migration 082 index) — LANDED** | **0.230 ms** | **43** | **3** |

Speedup at this session's scale: **~4,588x** (1055.371 ms → 0.230 ms) against
the single-page OLD cost, and **~4,624x** against the full-unbounded-scan OLD
cost. This is a lower bound on the real-world win: the OLD shape's cost
scales with total active CI-fact volume (grows with platform size), while
the NEW shape's cost is bounded by the owner's own run/artifact count
(independent of platform size) — so the gap widens, not narrows, at the
documented 500,000-scope worst case.

Zero-CI-evidence edge case (a repository that has never had a CI run):
`0.035 ms`, 0 rows, no error — the `owned_runs` CTE's `Bitmap Index Scan`
on the new index returns nothing and every downstream join short-circuits.

### Exact-equivalence (output-preserving)

Independent reference query (a plain correlated `EXISTS`, deliberately a
DIFFERENT shape from the optimized `UNION ALL`/CTE query, to avoid proving
the query equivalent to itself) versus the optimized query, both against the
same seeded partition:

```
reference: count=3, checksum=e93bb50db17a763e49f60c4decd4a35c
optimized: count=3, checksum=e93bb50db17a763e49f60c4decd4a35c
```

Identical `count(*)` and ordered `md5(string_agg(fact_id))` — the same
exact-equivalence shape used for the 079/081 index proof above, satisfying
Mandatory Prove-The-Theory-First's output-preserving requirement.

### Local proof (this session, real Go code against the seeded database)

```
cd go && ESHU_POSTGRES_TEST_DSN=postgres://postgres:postgres@localhost:15544/dbg5843?sslmode=disable \
  go test ./internal/storage/postgres/ -tags integration \
  -run TestListActiveContainerImageCIFactsOwnerScopedQueryLive -v -count=1
--- PASS: TestListActiveContainerImageCIFactsOwnerScopedQueryLive
```

This exercises the real `FactStore.ListActiveContainerImageCIFacts` (not raw
SQL) against an isolated schema seeded with two repositories sharing the CI
fact family, proves migration 082 applies/reapplies/rolls back cleanly
(`CREATE INDEX CONCURRENTLY IF NOT EXISTS`), and proves the owner-scoped
result never leaks the sibling repository's evidence, with and without the
index present.

`go test ./internal/reducer ./internal/storage/postgres -count=1`: PASS
(includes `TestFilterContainerImageCIFactsForOwnerRunKeyIncludesFactScope`,
`TestContainerImageIdentityHandlerForeignSLSAAnchorStaysOutOfRepositoryScope`,
`TestFactStoreListActiveContainerImageCIFactsRequiresOwnerRepository`, and
the two migration-predicate lock tests for 081 and 082).

No-Regression Evidence: this change is strictly narrowing at the SQL layer
(the query now returns only the owner's rows instead of every active CI
fact) and the reducer's own `filterContainerImageCIFactsForOwner` still runs
unconditionally afterward as a defense-in-depth correctness net, so no
prior-passing test's semantics changed — only the volume of data the
Postgres round trip carries.

No-Observability-Change: no new metric, span, or log field; the query still
runs inside the same `container_image_identity` reducer execution the prior
No-Observability-Change note already covers.

## Hypothesis ledger

| candidate | stage seconds (before) | expected saving | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | ---: | --- | --- | ---: | ---: | --- | --- | --- |
| `fact_records_active_container_image_ci_idx` (migration 079), worst case (500K active CI scopes, ~2.12M facts) | 1292.650 ms | large at worst-case scale | `EXPLAIN (ANALYZE, BUFFERS)`, prior session (cited, not re-run here) | 1292.650 ms, ~2.27M buffers | 199.588 ms, ~805K buffers (~6.5x) | exact-equivalence via `count(*)` + ordered `md5(string_agg(fact_id))` over 100,000 rows, 0 diff | read-only query, no lock/claim/lease path; `CREATE INDEX CONCURRENTLY IF NOT EXISTS` is the standard non-blocking DDL this codebase already uses for identical-shape indexes | **proven, landed** — cited prior measurement; loader correctness independently re-proven this session (unit tests + live B-7 gate) |
| same index, low cardinality (2K–50K active CI scopes) | n/a (small platform) | none expected | same shim, prior session | — | planner correctly ignores the new index; no measurable regression | n/a (index unused) | n/a | **no-harm, confirmed** |
| flat WHERE + owner AND-clause, no repository_id index (Theory 1) | 1055.371 ms (OLD baseline) | none — expected large win | `EXPLAIN (ANALYZE, BUFFERS)`, this session, seeded 100K-scope/1M-fact partition | 1055.371 ms | 5261.360 ms (worse) | n/a — rejected before equivalence check | correlated subplan re-executed 200,000 times, no lock/lease concern but pathological CPU/buffer cost | **disproven, rejected** — saved implementation, not built on |
| same shape + `MATERIALIZED` CTE, no index (Theory 2) | 1055.371 ms | some win expected | same shim | 1055.371 ms | 955.698 ms (~10%, not the fix) | n/a — rejected, no real bound | read-only, no concurrency concern | **disproven, rejected** |
| `UNION ALL` + `MATERIALIZED` CTE + migration 082 repository_id index (Theory 3) | 1055.371 ms | large | same shim | 1055.371 ms, 300,000 rows | 0.230 ms, 3 rows (~4,588x) | exact-equivalence via `count(*)` + ordered `md5(string_agg(fact_id))`, checksum `e93bb50db17a763e49f60c4decd4a35c` on both sides, 0 diff | read-only query; `CREATE INDEX CONCURRENTLY IF NOT EXISTS`; reducer's `filterContainerImageCIFactsForOwner` stays as a correctness backstop, unaffected by which plan Postgres picks | **proven, landed** — this session, live Go-code proof (`TestListActiveContainerImageCIFactsOwnerScopedQueryLive`) plus full reducer/postgres suites green |
