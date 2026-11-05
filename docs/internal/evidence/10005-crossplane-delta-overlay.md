# #10005 and #10003 Row 3: Crossplane SATISFIED_BY Reads Overlay a Delta

When a git delta became a scope's active generation, the Crossplane
SATISFIED_BY materialization deleted correct edges. The handler retracts
every edge of its scope and then writes the edges it can resolve, so whatever
its load misses is lost. Two reads missed rows that only the delta's base
held:

- **Claim side (#10005).** The handler loaded the intent generation's own
  content_entity rows. A delta carries the changed files only, so a claim
  beside a changed, added, or deleted one was missing.
- **XRD side (#10003 row 3).** `ListActiveCrossplaneXRDFacts` joined each
  scope's active generation. A delta-active scope whose XRD sits in a file
  the delta never touched returned no XRD, so every claim resolving against
  it lost its edge, in that scope and in any other. The golden fixture's dr-4
  assertion (#10022) records this case.

## Wiring at the time of the fix

- `PriorGenerationCheck` is wired (`cmd/reducer/main.go:330`, into
  `DefaultHandlers`; `defaults_additive_domains_crossplane.go:18` passes it to
  the handler). `shouldSkipRetract` skips the retract only for a first
  attempt with no prior generation. A delta always has a prior generation,
  so the retract always ran.
- `EdgeExistenceReader` is wired (`cmd/reducer/wiring_handlers.go:225`,
  `CrossplaneSatisfiedByEdgeExistenceReader`). It only gates the redrive
  ledger write after the edge write. It never prevents the retract.
- `Runtime.execute` (`internal/reducer/runtime.go:347`) runs the handler only
  while the intent's generation is active. `cmd/reducer/main.go:238` builds
  the production executor with `NewDefaultRuntime`, which sets
  `GenerationCheck` (`cmd/reducer/main.go:329`). A pending generation is
  deferred and a superseded one is acknowledged as superseded. So the scope's
  active generation is the intent's own generation when the load runs.

## Change

One mechanism, the #10003 PR0 overlay helper, at the reader. The handler's
retract and write are unchanged.

- `go/internal/storage/postgres/facts_active_crossplane_xrd.go`:
  - `ListActiveCrossplaneXRDFacts` reads on one snapshot and overlays each
    delta-active scope on its base. Its base text is
    `deriveDeltaOverlayBaseQuery(listActiveCrossplaneXRDFactsQuery, 4)`.
  - New `listActiveCrossplaneClaimCandidateFactsQuery`: one scope's active
    K8sResource content entities (`fact.scope_id = $1`). Its base text is
    derived at `$5`.
  - New `ListActiveCrossplaneSatisfiedByFacts(ctx, scopeID)`: both legs on
    one read-only repeatable-read snapshot with one target lookup. The claim
    leg reads a base only when the intent scope itself is delta-active with a
    baseline.
  - The sink: `eshu_dp_crossplane_satisfied_by_read_delta_union_total` by
    `read` (`claim`, `xrd`) and `outcome`, the
    `crossplane_satisfied_by_read.delta_union` span event, and WARN lines
    starting "crossplane satisfied-by read".
- `go/internal/reducer/crossplane/satisfied_by_load.go` (split out of
  `satisfied_by_materialization.go`, which would have passed 500 lines):
  `loadEdgeFacts` prefers the new optional `crossplaneSatisfiedByFactLoader`.
  A loader without it keeps the generation-keyed path. A failed read fails
  the intent before any retract.
- `go/internal/telemetry/instruments.go`: the new counter.

The claim leg drops two kinds of row the old own-scope load returned:
content entities other than K8sResource, which `ExtractCrossplaneSatisfiedByEdgeRows`
ignores, and same-scope XRDs, which the XRD leg returns for every scope. The
edges written for a full generation are unchanged (the full-generation case
below).

## Regression: RED, then GREEN

Disposable PostgreSQL 18.6 (`ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN`,
`postgresproof.OpenDisposableDatabase`). These tests run the production
handler over the production store with a recording edge writer:
`TestListActiveCrossplaneXRDFactsDeltaActiveScopeLive` and
`TestCrossplaneSatisfiedByDeltaKeepsUnchangedEdgesLive`
(`satisfied_by_read_delta_live_test.go`). The hermetic handler tests
are in `satisfied_by_load_test.go`. For RED, the production files were set
back to the base commit and the tests ran unchanged:

```text
reducer/crossplane (pre-fix handler):
--- FAIL: TestCrossplaneSatisfiedByHandlerPrefersTheSnapshotRead
--- FAIL: TestCrossplaneSatisfiedByHandlerFailsClosedOnSnapshotReadError
--- PASS: TestCrossplaneSatisfiedByHandlerKeepsTheGenerationReadFallback
reducer exit=1
storage/postgres (pre-fix reader and handler):
loaded XRD facts = [], want [reader-platform-full-xrd]
same-scope claim rewritten, XRD untouched: edges = [], want [case0-claim-1->case0-xrd-1]
claim added beside an unchanged claim: edges = [case1-claim-2->...], want claim-1 and claim-2
claim changed beside an unchanged claim: edges = [case2-claim-2->...], want claim-1 and claim-2
claim deleted beside an unchanged claim: edges = [], want [case3-claim-1->case3-xrd-1]
delta touches only the XRD: edges = [], want claim-1 and claim-2
full generation is unchanged: PASS (the control)
missing base: claim leg outcomes = map[], want one union_base_missing
postgres exit=1
```

GREEN on the fix: every case above passes, `exit=0`.

What the cases prove:

| Case | Edges after the scope retract |
| --- | --- |
| same-scope claim rewritten, XRD untouched (dr-4 shape) | the claim's edge, from the base XRD |
| claim added beside an unchanged claim | both claims |
| claim changed beside an unchanged claim | both claims; attempt 2 (always retracts) and a replay write the same set |
| claim deleted (tombstone and deleted path) | the unchanged claim only |
| delta touches only the XRD | both base claims, against the delta's XRD |
| full generation | both claims; the claim leg makes no overlay decision |
| base missing | the delta's claim only, counted `union_base_missing` with a WARN |

Every case asserts that the retract set is exactly `[intent scope]`, under a
delta and under a full generation.

## Concurrency, retry, and replay

- The load is a read on one repeatable-read snapshot, so a scope that
  activates mid-call cannot tear the two legs apart. The write path is the
  existing retract-then-MERGE, which is idempotent per
  `(claim uid, SATISFIED_BY, xrd uid)`. Nothing is serialized and no worker
  count changed.
- A retry and a replay of the same intent read the same state and write the
  same edges (the replay case).
- Two intents for one scope that run at once each retract the scope and
  write the state their own snapshot read. That interleaving is the same as
  before this change. The fix changes only what each run reads.

## Theory first: EXPLAIN shim

Performance Evidence: the shim below (baseline: the pre-fix reads on the same
corpus; after: the overlay reads, then migration 280), and the live
regressions above for the row-level results.

Decision rule, set before measuring: no Seq Scan on `fact_records`; base p50
at most 1.5x its active p50, or else (active + base) at most 1.5x the all-full
counterfactual with at most 25 ms added; no base statement when nothing is
delta-active.

Corpus: `lane3_10005_explain`, PostgreSQL 18.6. It has 420 git scopes, 40 of
them delta-active. One has a 50,000-file base and a 5,000-file delta; four
have 5,000 files; the rest have 200. There are 472,600 `fact_records` rows
(478 MB). Literal parameters; p50 of five runs after a warm-up; whole reads
(LIMIT 100,000). The host was shared and noisy: single runs reached 5.8 s and
7.6 s.

| Read | Delta state p50 ms | All-full counterfactual p50 ms | Verdict |
| --- | --- | --- | --- |
| target lookup | 0.24 | none | the only added statement with nothing delta-active |
| XRD active | 70.9 to 190.6 | 242.0 to 248.9 | today's read, already slow |
| XRD base (40 scopes) | 167.7 to 312.7, 156,108 buffers | none | fails: 1.6x active, (active + base) about 2x |
| claim active, 50k scope | 7.9 | 18.5 | `fact_records_active_k8s_claim_redrive_idx` |
| claim base, 50k scope | 16.0 | none | passes: (7.9 + 16.0) / 18.5 = 1.29x |
| claim active / base, 200-file scope | 0.18 / 0.80 | 0.52 | 1.9x by ratio, +0.46 ms |
| today's own-scope load, 50k scope | 19.0 | 161.0 | replaced by the claim leg |

No plan has a Seq Scan on `fact_records`. Both XRD texts scan every fact row of
the generations they reach through `fact_records_scope_generation_idx` plus a
heap filter, because no index serves the XRD predicate. The claim leg is
cheaper than the own-scope load it replaces: 18.5 ms against 161 ms on the
50k scope's full generation. The XRD leg fails the rule. The coordinator
ruled to add the partial index in this change; its proof is the next
section, and it lands in its own commit.

Plans: `EXPLAIN (ANALYZE, BUFFERS)` text for every row is in the shim's
`plans10005/` directory (scratch, not committed).

## Migration 280: the XRD partial index

Coordinator ruling: add the index in this change, in its own commit, so it
can be reverted alone.

- **Precedent.** Migrations 273 and 276 are partial indexes on
  `fact_records`. Each is one `CREATE INDEX CONCURRENTLY IF NOT EXISTS` per
  file, with a Go DDL constant joined into `factRecordSchemaSQL`, a name in
  `orderedBootstrapDefinitionNamesLate`, a row in
  `migrationShippedChecksums`, and a re-pinned `goldenBootstrapDefinitionsDigest`
  and count. Migration 280 follows that shape. The Go DDL lives in the new
  `schema_fact_records_crossplane.go`, because `schema_fact_records.go` is at
  491 lines.
- **Predicate.** `fact_kind = 'content_entity' AND source_system = 'git'
  AND is_tombstone = FALSE AND (payload->>'entity_type' = 'CrossplaneXRD' OR
  payload->>'entity_kind' = 'CrossplaneXRD')`. That is the XRD reader's
  filter, modulo the alias. The derived base text keeps it unchanged, so the
  planner proves the implication for both.
  `TestCrossplaneXRDIndexPredicateMatchesTheReader` checks each term against
  both texts, and checks migration 280 against the Go DDL.
- **Plans, before and after** (shim above, the same texts):

  | Text | Before | After |
  | --- | --- | --- |
  | XRD active, delta state | 70.9 to 190.6 ms, `fact_records_scope_generation_idx` plus heap filter | 1.2 to 2.5 ms, 910 buffers, `fact_records_crossplane_xrd_entity_idx` |
  | XRD derived base, 40 delta scopes | 167.7 to 312.7 ms, 156,108 buffers | 0.8 to 3.3 ms, 280 buffers, the same index |
  | XRD active, all-full | 242.0 to 248.9 ms | 2.2 to 2.5 ms |

  With the index, base/active is 1.3x, which passes the decision rule
  directly. No plan has a Seq Scan on `fact_records`. The two before/after
  runs differ only by host noise.
- **Plan guard.** `TestCrossplaneXRDReadsUseThePartialIndexLive` builds
  40 scopes with 500 non-XRD entities each, 10 of them delta-active. It
  requires the index in both plans and no Seq Scan on `fact_records`. A
  seeded violation (migration 280 narrowed to `entity_type` only, so its
  predicate no longer implies the reader's) fails both guards: the live one
  with `active XRD read does not use fact_records_crossplane_xrd_entity_idx`,
  the hermetic one with `migration 278 and the Go DDL differ` (the run predates the renumbers from 278 to 279 and then 280, which open PRs took); `exit=1`. The
  real file passes both; `exit=0`.
- **Lock behavior.** `CREATE INDEX CONCURRENTLY` takes SHARE UPDATE
  EXCLUSIVE on `fact_records`, which does not conflict with INSERT, UPDATE,
  or DELETE. On the shim (297 MB heap, 478 MB total) the build took 1.0 s.
  Three inserts committed during it (47 to 239 ms on the loaded host), and
  `pg_locks` showed the build's lock granted beside them. VACUUM, ANALYZE,
  and other DDL on `fact_records` wait for the build. The build also waits
  for every transaction older than its snapshot, which includes in-flight
  repeatable-read reader calls.
- **Extrapolation, not a measurement.** The QA storage audit
  (`7932-storage-audit.md`, 2026-10-16) reports a `fact_records` heap of
  342 GiB plus 138 GiB of TOAST, about 245 million rows. Scaling the shim's
  1.0 s per 297 MB of heap linearly gives about 20 minutes of heap scan. The
  predicate reads `payload`, so a TOASTed payload is detoasted, which could
  add up to about 8 minutes. The total depends on storage throughput and
  concurrent load. 273 and 276 read `payload` the same way and shipped
  under the same procedure.
- **Size.** One entry per git XRD content entity across retained
  generations. On the shim, 189 entries fill 32 kB. An entry is about 200
  bytes (two ids, a timestamp, a fact id), so 100,000 retained XRD entities
  would be about 20 MB. The QA XRD count was not measured.
- **Rollback.** `DROP INDEX CONCURRENTLY IF EXISTS
  fact_records_crossplane_xrd_entity_idx` is safe. The index carries no
  constraint, so the readers return the same rows, at the pre-index cost.
  Per the migrations README, an operator drop also needs the recorded
  migration removed before the tracked migrator rebuilds it.
- **Write cost.** One predicate evaluation per inserted fact, and a B-tree
  insert only for an XRD content entity.

## Self-heal and already-wrong data

A scope whose edges were deleted before this fix is repaired by its next full
generation: the reconcile sweep (default 24 h; `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`
disables it), the growth valve, or the reindex watermark. It is also repaired
by the next delta that fires the domain, which now reads the overlay. The
fix changes no stored data.

## Not checked

- pgx's cached-statement plans for the new texts. The shim used custom plans
  with literal parameters; the readers run on the pool default.
- Cold cache and production scale.
- The golden delta-reader gate (#10022) was not run here.

## Observability Evidence

Observability Evidence:
`eshu_dp_crossplane_satisfied_by_read_delta_union_total{read,outcome}` counts
one decision per delta-active scope a leg reached. The live cases assert
`claim/union_base_found`, `xrd/union_base_found`, and
`claim/union_base_missing`, and the missing-base case asserts the WARN line.
The existing completion log's `fact_count` now counts the overlaid rows.
