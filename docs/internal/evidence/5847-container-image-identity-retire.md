# #5847 — a reopened `container_image_identity` replay leaves the superseded decision live

`container_image_identity` sits in the bootstrap maintenance reopen slice, its
fact identity embeds `outcome`, and the writer had no retire. A replay that
reaches a different answer than the first execution therefore did not correct
anything: it added a second row and left the first one live for the same active
generation.

This is the same defect #5837 fixes for `aws_cloud_runtime_drift`, on a
different domain.

## What the identity is built from

`containerImageIdentityIdentity`
(`go/internal/reducer/container_image_identity_writer.go`) keys on `scope_id`,
`generation_id`, `image_ref`, and `outcome`.
`containerImageIdentityStableFactKey` and `canonicalContainerImageIdentityID`
are both built from that same map, so `outcome` reaches the `fact_id`.

The durable write is `reducerBatchInsertFacts`, whose statement is
`ON CONFLICT (fact_id) DO UPDATE` (`reducer_fact_batch_insert.go`). Conflict
resolution is on `fact_id` alone, so a decision that changes any identity field
lands as a NEW row rather than replacing the old one.

## Correction to the issue's premise

#5847 describes the failing replay as "an unresolved-outcome row" being
corrected to a resolved one. That direction is actually the safe one, and the
reason is worth recording because it changes which replay you have to test.

`containerImageIdentityCanonicalDecisions` filters the write to decisions with
`CanonicalWrites > 0` (`container_image_identity.go:431`). Only two branches in
`classifyContainerImageRef` set that
(`container_image_identity_registry.go:126,139,176`):

| outcome | canonical | durably written |
| --- | --- | --- |
| `exact_digest` | yes | yes |
| `tag_resolved` | yes | yes |
| `ambiguous_tag` | no | no |
| `unresolved` | no | no |
| `stale_tag` | no | no |

So an `unresolved` → `exact_digest` replay writes nothing on the first pass and
one row on the second. No stale row.

The reachable stale shapes are the reverse and the sideways move:

1. **Demotion out of the canonical set.** A digest-only reference resolves to
   `exact_digest` while `observationsForDigest` returns exactly one observation
   (`container_image_identity_registry.go:106-129`). Once a second cross-scope
   OCI generation activates with another repository observing the same digest,
   `len(observations) > 1` and the same reference falls to `ambiguous_tag` —
   non-canonical, so NO row is written and the upsert overwrites nothing. The
   confident `exact_digest` row, with a `repository_id` now known to be
   ambiguous, stays live forever. `tag_resolved` → `stale_tag` (line 154) is the
   same shape. This is precisely the cross-scope OCI activation the reopen slice
   exists for.
2. **A changed `image_ref` under the same canonical outcome.** The exact-digest
   branch REWRITES `decision.ImageRef` to `imageRefFromOCIRepositoryID(obs.
   repositoryID, digest)` (line 121). If the single matching observation resolves
   to a different repository on a later pass, the identity changes, a second
   `exact_digest` row is minted, and both are served.

`exact_digest` ↔ `tag_resolved` for one reference is NOT reachable: `byRef` is
keyed on `parsed.raw` (`container_image_identity_evidence.go:190`), so
`ref.parsed.digest` — the field that selects between the two branches — is fixed
per key. The writer-level regression test still drives an outcome change
directly, because the writer's contract is "the durable set equals the written
set" regardless of which classifier transition produced it, and because the
identity-churn assertion is what would silently rot if `outcome` were ever
removed from the identity.

## Read path: stale rows are served, not hidden

`PostgresContainerImageIdentityStore.ListContainerImageIdentities`
(`go/internal/query/container_image_identities.go:144`) filters on
`fact_kind`, `is_tombstone = FALSE`, the `ingestion_scopes.active_generation_id`
join, and `generation.status = 'active'`, then
`ORDER BY fact.fact_id ASC LIMIT $8`. There is **no** `DISTINCT ON`, no
`GROUP BY`, no per-digest or per-image_ref latest-wins, and no in-process dedupe
in `decodeContainerImageIdentityRow`. A digest filter that matches both rows
returns both.

The aggregate rollups are the same:
`go/internal/query/container_image_identity_aggregates.go:113,136,160` count
`fact_kind = 'reducer_container_image_identity'` rows directly, so a duplicate
inflates the counts.

Severity is therefore not reduced by the read path. The duplicate rows are wrong
at rest AND wrong on the wire.

## Verification of the assumptions the retire rests on

Re-confirmed against this branch's `origin/main` base (`c23e30001`):

1. **One intent per (scope, generation).**
   `buildContainerImageIdentityReducerIntent` returns a single `ReducerIntent`
   with `EntityKey: "container_image_identity:" + scopeValue.ScopeID`
   (`go/internal/projector/container_image_identity_intents.go:36-54`).
2. **The evidence loader pages to exhaustion.**
   `loadIdentityFactsUncached` loops `for { ... }` appending each page until
   `len(page) < listFactsByKindPageSize`
   (`go/internal/storage/postgres/facts_active_container_image_identity.go`).
   The `LIMIT $3` is a keyset page size, not a truncation, so the handler sees
   the complete active set.
3. **Every written row carries the intent's scope.** `write.ScopeID` and
   `write.GenerationID` are stamped on each row and on the identity
   (`container_image_identity_writer.go`), so a retire keyed on
   `(fact_kind, scope_id, generation_id)` touches only this intent's own rows.
4. **The handler calls the writer once per intent**
   (`container_image_identity.go:206`); chunking happens inside
   `reducerBatchInsertFacts`, so it is one logical write.

Two further checks the retire needed on its own:

5. **This domain writes no other fact kind under the same (scope, generation).**
   `rg -n "containerImageIdentityFactKind|reducer_container_image_identity"`
   over non-test Go shows exactly one producer —
   `container_image_identity_writer.go`. Every other hit is a reader. The
   handler's two remaining side effects,
   `projectContainerImageBuiltFromEdges` and
   `projectContainerImageDerivedFromEdges` (`container_image_identity.go:217,220`),
   write canonical graph edges, not `fact_records` rows. Nothing else is in the
   retire's blast radius.
6. **Concurrent same-scope executions CAN race, and the queue does not stop
   them.** An earlier revision of this document claimed the reducer claim batch
   fenced them, on the grounds that it excludes any work item whose
   `(conflict_domain, COALESCE(conflict_key, scope_id))` already holds an
   in-flight lease. That justification was wrong. The exclusion predicate
   requires `inflight.claim_until > $1` — a LIVE lease
   (`go/internal/storage/postgres/reducer_queue_batch_query.go`) — while the
   base predicate is `(claim_until IS NULL OR claim_until <= $1)`, which
   RE-ADMITS an item whose lease has expired. Lease expiry is exactly the
   stalled-worker case, so the cited exclusion covers every same-scope overlap
   except the one that matters. Heartbeat loss is quarantined only AFTER
   `Handle` returns (`go/internal/reducer/service.go`), so a worker whose lease
   lapsed mid-execution still completes its write.

   `eshuSearchDocumentRetireQuery` does not rely on that exclusion either: it is
   protected by the #4233 invalidate-before-mutate `ProjectionState` fence
   (`BeginBuilding` returning a revision and a fence,
   `go/internal/reducer/eshu_search_document_writer.go`).
   `container_image_identity` has no `ProjectionState` at all, so it needs a
   fence of its own.

## Fix

`containerImageIdentityRetireQuery`
(`go/internal/reducer/container_image_identity_writer_queries.go`), patterned on
`eshuSearchDocumentRetireQuery` and its `aws_cloud_runtime_drift` sibling:

```sql
WITH stamped AS (
    UPDATE fact_records
    SET fencing_token = $5
    WHERE fact_kind = $1
      AND scope_id = $2
      AND generation_id = $3
      AND fact_id = ANY($4::text[])
      AND fencing_token <= $5
    RETURNING fact_id
)
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
  AND fencing_token <= $5
```

It runs AFTER the insert. That buys two things and it is worth being precise
about which, because it does NOT buy atomicity: a failed insert leaves prior
decisions intact rather than clearing them and writing nothing, and no reader
sees this scope generation with ZERO decisions, which retire-first would expose
for the width of the insert. The window that IS open is the opposite one — the
insert and the retire are separate autocommit statements on the same connection,
so a reader landing between them sees BOTH the superseded decision and the
corrected one. That is the same shape the retire exists to remove, just briefly
instead of permanently; closing it needs the two statements to share a
transaction, which this writer cannot do while it holds a bare execer.

The keep-set is built from the same rows handed to the insert, never re-derived
from `write.Decisions`, so it cannot disagree with what was persisted. An empty
keep-set retires everything for the generation at or below this write's token,
which is exactly right for the demotion case.

### The fence

`$5` is the write's evidence-read watermark
(`ContainerImageIdentityWrite.EvidenceAsOf`), captured by the handler
immediately BEFORE its first fact load and rendered as microseconds. Evidence
read time, not write time: write time ranks the stalled worker highest, which is
the inversion the fence exists to stop. `Intent.AttemptCount` does not work
either, because the reopen-succeeded statement resets it to 0, so a reopened
replay would rank below the run it exists to repair. A zero `EvidenceAsOf` is a
hard error, never a defaulted unfenced write: rows carry `fencing_token` 0 by
table default, so `fencing_token <= 0` would still match everything and the
retire would run completely unfenced with nothing saying so.

The token is also carried on the **insert**
(`reducerFactBatchInsertQuery` now writes `fencing_token` and raises it with
`GREATEST` on conflict). The stamping CTE alone is not sufficient: it runs in a
separate autocommit after the insert, so a freshly inserted row is committed and
visible at the default `0` until the stamp lands, and `0` is at or below every
other worker's token. A stalled worker's fenced retire arriving in that window
deletes the fresher row anyway, and in the worst case BOTH rows vanish — A's
retire takes B's unstamped row, then B's retire takes A's stamped one. That is
strictly worse than main. Stamping at birth closes it; `GREATEST` additionally
stops a stale re-upsert from downgrading a fresher row's token. Callers that
leave `reducerFactRow.FencingToken` at zero insert `0`, the value the column
default already gave them, and `GREATEST(existing, 0)` leaves an existing row
alone, so this is a no-op for every other writer on that statement.
`reducerFactBatchInsertVersionedQuery` does NOT carry the column, and its doc
comment now says a governed domain that grows a fenced retire must add it.

### What the fence does not close

A stale pass can still INSERT its own row: if A's insert lands after B's retire
has run, A's row is added under its own fact_id and no later pass in that
generation removes it, leaving the two-contradictory-rows shape that exists on
main today. Closing that needs the #4233 `ProjectionState.BeginBuilding`
begin-before-mutate fence, which is a larger change than this repair. The
statement here is what stops the retire from making that case WORSE by deleting
the correct row.

The fence also cannot help a pass that read EMPTY evidence, because that pass
read LAST and so ranks highest. `classifyContainerImageRef` returns `unresolved`
when `index.observationsForDigest` yields ZERO observations, which is the same
answer it gives an image that genuinely has no digest identity. A pass running
while the cross-scope OCI facts are not visible therefore lands every decision
non-canonical, hands the writer an empty keep-set, and clears the partition —
where before the retire existed the same pass was a harmless no-op, and nothing
re-triggers the domain afterwards. The writer cannot distinguish the two from
where it sits, so it is loud instead:
`ContainerImageIdentityWriteResult.RetiredWithoutCanonicalWrites` is set and a
`slog.Warn` names the intent, scope, generation, retired count, and watermark
whenever a zero-canonical pass retires a non-empty prior set.

The reopen-slice comment in `go/cmd/bootstrap-index/bootstrap_pipeline.go`
claiming the decision "upserts on a scope-keyed stable fact key, so replay is
idempotent" was false and is corrected. `go/cmd/bootstrap-index/AGENTS.md` gains
the general rule — including the fencing traps — so the next domain added to that
slice is checked for the same shape first.


## Proof

Every number below was produced on this branch after the final edit, against a
throwaway Postgres 16 container (`eshu-cii-retire-pg`, host port 55847) created
for this work.

### Failing before, passing after

The three retire regressions failed on the pre-change writer:

```
$ go test ./internal/reducer -run 'TestContainerImageIdentityWriterRetire' -count=1
--- FAIL: TestContainerImageIdentityWriterRetiresEverythingWhenNothingIsCanonical
    retire statements issued = 0, want exactly 1
--- FAIL: TestContainerImageIdentityWriterRetiresAfterInsert
    exec calls = 1, want 2 (one batched insert, one retire)
--- FAIL: TestContainerImageIdentityWriterRetiresSupersededDecisionForSameImageRef
    retire statements issued = 0, want exactly 1
FAIL
```

After the change the whole set is green:

```
$ go build ./...                                        # exit 0
$ go test ./internal/reducer ./internal/query ./internal/storage/postgres \
    ./cmd/bootstrap-index ./internal/replay/costcounting \
    ./internal/goldengate ./cmd/golden-corpus-gate -count=1
ok  github.com/eshu-hq/eshu/go/internal/reducer               3.180s
ok  github.com/eshu-hq/eshu/go/internal/query                 2.793s
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres      6.248s
ok  github.com/eshu-hq/eshu/go/cmd/bootstrap-index            5.579s
ok  github.com/eshu-hq/eshu/go/internal/replay/costcounting   0.718s
ok  github.com/eshu-hq/eshu/go/internal/goldengate            0.830s
ok  github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate         2.346s
```

### The born-unstamped hole, reproduced and closed

`TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive`
drives the interleaving deterministically instead of racing for it: worker B's
execer is wrapped so that, at the moment B is about to issue its own retire, it
first runs worker A's ENTIRE write (insert plus fenced retire) at a watermark
five minutes older. Both workers use the production writer and the production
statements against real Postgres.

With the token stamped only by the retire's CTE — the shape a verbatim port of
the `aws_cloud_runtime_drift` fence produces — BOTH rows are lost:

```
--- FAIL: TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive
    surviving rows = [] (outcomes [], tokens []), want exactly 1: the fresher
    worker's row must survive a stalled worker's retire that lands between its
    INSERT and its stamp
```

A's retire deleted B's row because it was still sitting at the column default
`0`, and B's own retire then deleted A's stamped row because A's token was
below B's. That is strictly worse than main, which merely left a stale row
beside a correct one. Carrying `fencing_token` on the insert closes it.

### Live proofs (all four)

```
$ ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:55847/eshu \
    go test ./internal/storage/postgres -run 'ContainerImageIdentity.*Live' -count=1 -v
--- PASS: TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive (0.82s)
--- PASS: TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive (1.24s)
--- PASS: TestContainerImageIdentityRetireBoundedToOwnPartitionLive (1.55s)
--- PASS: TestContainerImageIdentityRetireEmptyKeepSetClearsDemotedGenerationLive (1.67s)
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres  14.739s
```

They live in `go/internal/storage/postgres`, not `go/internal/reducer`, and are
gated on `ESHU_POSTGRES_DSN` alone. An earlier revision put them in package
`reducer` behind an extra `ESHU_CONTAINER_IMAGE_IDENTITY_RETIRE_LIVE=1` flag
that appears nowhere in `scripts/` or `.github/`, over a hand-rolled
four-column `fact_records` with no `fencing_token`, no `is_tombstone`, and no
indexes — so it could never run in CI and could not have exercised the fence at
all. `internal/storage/postgres` importing `internal/reducer` is the reason the
test belongs on this side: it is the direction that compiles, and it is where
`ApplyBootstrap` gives the real table with its real foreign keys, which is what
the sibling `aws_cloud_runtime_drift_retire_live_test.go` also does.

### Mutants

All three were applied to the final code, run, and reverted.

```
OR TRUE appended to the DELETE
  FAIL TestContainerImageIdentityRetireQueryIsBoundedToItsOwnPartition
       (normalized full-text mismatch)
  FAIL TestContainerImageIdentityWriterRetiresAfterInsert
  FAIL ...RetireBoundedToOwnPartitionLive   Retired = 5, want exactly 1
  FAIL ...RetireEmptyKeepSetClearsDemotedGenerationLive  Retired = 3, want 2
  FAIL ...RetireCannotDeleteFresherEvidenceRowsLive

fence predicate cut from the DELETE
  FAIL TestContainerImageIdentityRetireQueryIsBoundedToItsOwnPartition
  FAIL ...RetireCannotDeleteFresherEvidenceRowsLive
       the stalled worker's retire deleted 1 row(s) written from FRESHER
       evidence, want 0
  FAIL ...FreshlyInsertedRowIsFencedBeforeItIsVisibleLive  surviving rows = []

insert no longer stamps at birth (FencingToken left 0)
  FAIL TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert
       inserted fencing_token = 0, want 1785146400000000
  FAIL ...FreshlyInsertedRowIsFencedBeforeItIsVisibleLive  surviving rows = []
```

The fenced run is the `DELETE 0` side of the same pair: with the fence intact
the stalled worker reports `Retired = 0` and the fresher row survives carrying
its own watermark; with the fence cut it reports one deleted row.

Note what the second and third mutants prove separately. The fence-cut mutant
lives in the retire predicate; the born-unstamped mutant lives in the INSERT
path, and no amount of auditing the `DELETE` would have found it. That is why
the third mutant exists.

### Gate

```
$ bash scripts/test-verify-golden-corpus-gate.sh
test-verify-golden-corpus-gate: pass
```

The `list_container_image_identities` MCP shape now pins `maximum_results: 1`
alongside its existing `minimum_results: 1`. The floor alone could not detect
this fix regressing: an ANY-match "at least one identity for this repository"
assertion passes identically on one correct row and on that row plus its
superseded contradiction, which is exactly what a regressed retire produces.
`QueryShape.MaximumResults` is a new, generally-available ceiling in
`go/internal/goldengate` (the query-shape counterpart to `RequiredNode`'s
`MaximumNodePropertyCount`), covered by `TestEvaluateQueryShape`'s
`maximum results ceiling` and `maximum results without an array result field`
subtests. Exactly one row matches that narrowing today; see
`docs/internal/evidence/5428-built-from-projection-rescinded.md`, which records
that "narrowing selects the same single row".

## Cost

The `container-image-identity` cost budget moves from 1 statement to 2, and
stays at 2 after the fence: the retire is a single set-based stamp-plus-delete
CTE regardless of decision count, so the per-decision bound the budget actually
guards is unchanged in shape. The N+1 negative control costs 4 and still exceeds
the budget.

The batched insert gains one bind array (`fencing_token`, 16 columns instead of
15) and one `GREATEST` expression in its `ON CONFLICT` set-list. No extra
statement, no extra round-trip. `reducerFactBatchSize` is unchanged at 1000, and
1000 rows x 16 columns is still well under Postgres' 65535 bind-parameter
ceiling. Callers that leave `reducerFactRow.FencingToken` at zero write `0`,
which is the column default they already had, and `GREATEST(existing, 0)` leaves
an existing row's token untouched — pinned by
`decodeCloudInventoryBatchedRows`, which now asserts a non-opted-in domain binds
`0` for every row.

Performance: the write path gains exactly one bounded statement per intent
execution, O(1) in decision count. No hot-path Cypher, graph write, or query
handler changed.

Observability Evidence: the retire now reports what it destroyed.
`ContainerImageIdentityWriteResult.Retired` carries the DELETE's `RowsAffected`,
the handler's evidence summary renders `retired=<n>` and
`retired_without_canonical_writes=<bool>` beside the decision counts, and a
zero-canonical pass that cleared a non-empty prior set emits a `slog.Warn`
naming intent, scope, generation, retired count, and evidence watermark. The
earlier `No-Observability-Change:` marker on this change was an overclaim: the
statement did run inside the instrumented `InstrumentedDB.ExecContext` wrapper
that records `eshu_dp_postgres_query_duration_seconds`, which is true and beside
the point — that records that A write happened, never what was destroyed. The
existing `eshu_dp_container_image_identity_decisions_total` counter and reducer
run spans are unchanged.
