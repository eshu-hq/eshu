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

Re-confirmed against `c23e30001`, the `origin/main` tip when these checks were
run — not the branch's merge-base, which has since advanced to `76931f4d8`
(`c23e30001` is an ancestor of it). Read the line numbers below at `c23e30001`;
some have shifted since, so `git show c23e30001:<path>` is the way to resolve
them:

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

The token is stamped by the **insert** (`reducerFactBatchInsertQuery` now writes
`fencing_token`). Stamping it from the retire instead is not sufficient: the
retire runs in a separate autocommit after the insert, so a freshly inserted row
is committed and visible at the default `0` until the stamp lands, and `0` is at
or below every other worker's token. A stalled worker's fenced retire arriving in
that window deletes the fresher row anyway, and in the worst case BOTH rows
vanish — A's retire takes B's unstamped row, then B's retire takes A's stamped
one. That is strictly worse than main. Stamping at birth closes it.
`reducerFactBatchInsertVersionedQuery` does NOT carry the column, and its doc
comment now says a governed domain that grows a fenced retire must add it.

### The conflict clause is guarded, not merged

The insert's conflict update is gated on `fact_records.fencing_token <=
EXCLUDED.fencing_token`, so a stale pass's upsert is rejected WHOLE.

An earlier revision raised only the token
(`fencing_token = GREATEST(existing, excluded)`) with the content columns
assigned unconditionally. That protects the token and nothing else, and the
combination is worse than no fence. The fact identity embeds only
`(scope_id, generation_id, image_ref, outcome)`, while `source_revision`,
`source_revision_provenance`, `build_provenance_repository_ids` and
`evidence_fact_ids` are payload-only and are filled in by the cross-scope
enrichment (`applyCIRunDigestRevision`/`applySLSADigestRevision`) whose
visibility depends on which generations are active at load time. Two passes that
agree on the outcome therefore collide on the SAME `fact_id` with DIFFERENT
payloads, and the pass that read before the CI/SLSA generation activated carries
the poorer one. This is the reachable stalled-holder shape, not a hypothetical:
it is the same class as the #5426 failure where a persisted identity row named
the building repository in `source_repository_ids` but carried nothing in
`build_provenance_repository_ids`.

Measured on Postgres 16, GREATEST form:

```
--- FAIL: TestReducerFactBatchInsertRejectsStaleContentUpsertLive
    stored source_revision/provenance = ""/"", want "commit-fresh"/"ci_run_commit"
```

The stalled worker overwrote the fresher row's content and `GREATEST` left the
fresher token on it, so the row advertised a freshness its payload did not have —
and the retire's fence, reading that token, then protected the wrong row from
correction.

The shipped guarded form, same three live proofs, against a throwaway
`postgres:16-alpine` (16.14) on 127.0.0.1:55901:

```
$ ESHU_POSTGRES_DSN=postgres://eshu:eshu@127.0.0.1:55901/eshu?sslmode=disable \
    go test ./internal/storage/postgres -run 'ReducerFactBatchInsert.*Live' -count=1 -v
--- PASS: TestReducerFactBatchInsertRejectsStaleContentUpsertLive (3.04s)
--- PASS: TestReducerFactBatchInsertAppliesEqualTokenRetryLive (0.24s)
--- PASS: TestReducerFactBatchInsertStaysInertForUnfencedWritersLive (0.23s)
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres  7.227s
```

That is the before/after pair for this section: the same first test that FAILs
under the `GREATEST` form PASSes under the guarded one. Those are cold-schema
times — the first test absorbs `ApplyBootstrap`, so a re-run against the warm
container returns 0.32s/0.22s/0.18s.

Two properties of the guard are pinned separately because both are easy to get
wrong:

- **`<=`, not `<`.** A retry, a redelivery, or a second chunk of the same pass
  carries the SAME watermark, because the watermark is the evidence-read time and
  a pass reads its evidence once. `<` would discard every one while reporting
  success — `TestReducerFactBatchInsertAppliesEqualTokenRetryLive`.
- **Inert for the six non-opted-in callers.** They bind `0` and their rows sit at
  the column default `0`, so `0 <= 0` holds and the update proceeds exactly as
  before. That is a property of the SQL, not of the arguments, so it is proven
  against real Postgres by
  `TestReducerFactBatchInsertStaysInertForUnfencedWritersLive` rather than
  asserted; `decodeCloudInventoryBatchedRows` covers only the bind half.

With the guard in place an explicit `GREATEST` would be dead text: no path can
lower a token, so the surviving value is always the larger of the two. The shape
matches `upsertFactBatchSuffix`
(`go/internal/storage/postgres/facts_streaming.go`, #4444), which already fences
the collector ingest path into this same table the same way.

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

With the insert not binding the token, so that only a retire-side stamp could
ever set it — the shape a verbatim port of the `aws_cloud_runtime_drift` fence
produces — BOTH rows are lost:

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

### The keep-set stamp was a no-op that still rewrote every kept row

The retire originally led with a `WITH stamped AS (UPDATE fact_records SET
fencing_token = $5 ... WHERE fact_id = ANY($4) AND fencing_token <= $5)` CTE, on
the reasoning that the partition should carry a durable record of how fresh each
row's evidence was.

Once the insert began binding the same token, that reasoning stopped holding.
`keepFactIDs` is built from the exact rows just handed to
`reducerBatchInsertFacts`, and that insert binds `fencing_token = $5` under a
conflict guard that never lowers it, so by retire time every keep-set row carries
`fencing_token >= $5`. The only rows `fencing_token <= $5` can still match are
the ones sitting at exactly `$5`, which the UPDATE then sets to `$5`. Both
properties the CTE existed for — the row is stamped, and a stale pass cannot
downgrade a fresher row — already come from the insert.

Postgres has no in-place UPDATE, so a no-op UPDATE is not a free UPDATE. Each
match wrote a SECOND row version per canonical decision per intent execution:

```
$ ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres \
    -run TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive -count=1 -v
--- FAIL: TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive
    keep-set row xmin moved 879 -> 880: the retire rewrote a row it was only
    meant to keep.
```

`fencing_token` was unchanged across that move — the row version was pure cost:
doubled WAL, doubled dead tuples, and doubled vacuum pressure on this domain's
hot write path. The committed cost budget counts STATEMENTS, and the count stayed
at two, so no committed gate saw it. The new proof counts row VERSIONS via
`xmin`, which is the unit the cost was actually paid in, and samples the keep-set
row from inside the insert/retire window so the token assertion cannot be
satisfied by the retire.

Dropping the CTE also removes a lock phase. It took row locks over the keep-set
while the DELETE took them over the complement, and Postgres specifies no
sub-statement ordering within a `WITH`, so two concurrent same-scope retires with
crossed keep/delete sets — `r1` in keepA ∩ deleteB, `r2` in keepB ∩ deleteA,
which is precisely the stalled-worker shape this fence exists for — deadlocks
ABBA. This branch did not measure that. The same statement shape was reproduced
on the `5837-drift-reopen` sibling branch by one harness run twice, on
Postgres 16.14: `SQLSTATE 40P01 deadlock detected`, with a `ShareLock` cycle in
both directions. That harness models the writer's real shape: two concurrent
autocommit retires over a 6,000-row crossed keep-set, each a single
`ExecContext` with no enclosing transaction and no external lock primer, so the
only lock phases are the ones the statement itself contains.

It is a race, so there is no fixed rate to quote and this doc does not quote one.
What reproduces is the asymmetry: the CTE variant deadlocked in most trials of
every run, while the plain fenced `DELETE` deadlocked in none of twenty. An
earlier harness that primed a `SELECT ... FOR UPDATE` on each pass's kept row was
discarded and its numbers retracted by its own author — it deadlocked the SHIPPED
statement too, because the primer manufactured the crossing instead of measuring
it. Any figure traceable to that harness is not evidence for this change.

One such figure is committed. The message body of commit `0cc4427b3` on this
branch states that an independent reproduction measured a deadlock in every
trial with the CTE and none without it. That is retracted, not merely stale: the
discarded harness deadlocked the SHIPPED statement too, so the clean half of
that comparison never happened. Commit messages cannot be corrected without
rewriting the branch, so it is named here instead. This branch is squash-merged
and GitHub's default squash body concatenates every commit message, so that
paragraph must be dropped when the squash body is written. The ABBA account in
this section is the one that holds.

A single `DELETE` scanning one index order cannot deadlock this way.

The retire is now a bare fenced `DELETE`.
`TestContainerImageIdentityRetireQueryIsASingleDeleteAndNothingElse` rejects any
reintroduced `WITH`, `UPDATE`, or `INSERT`, and the frozen-statement test carries
the new text.

### Live proofs (the five retire proofs)

The branch adds eight live tests in all. The five below cover the retire; the
three `ReducerFactBatchInsert*Live` proofs cover the shared insert's conflict
guard and are run and transcribed under
[The conflict clause is guarded, not merged](#the-conflict-clause-is-guarded-not-merged).

```
$ ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:55847/eshu \
    go test ./internal/storage/postgres -run 'ContainerImageIdentity.*Live' -count=1 -v
--- PASS: TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive (0.44s)
--- PASS: TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive (0.16s)
--- PASS: TestContainerImageIdentityRetireBoundedToOwnPartitionLive (0.22s)
--- PASS: TestContainerImageIdentityRetireEmptyKeepSetClearsDemotedGenerationLive (0.16s)
--- PASS: TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive (0.14s)
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres  4.895s
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
subtests.

Exactly one row matches that narrowing today, and the on-point measurement is in
the same snapshot file: the sibling `list_supply_chain_impact_findings`
description records, from the live corpus and post-#5810, that this digest
carries 16 `reducer_container_image_identity` rows and that "only ONE ... names
the BUILDING repository (github.com/eshu-hq/supply-chain-demo,
repository:r_69256c06)" — which is exactly the `source_repository_ids` value this
assertion narrows on. The other fifteen name only the deploying repository and
cannot match.

That is the statement to cite, not
`docs/internal/evidence/5428-built-from-projection-rescinded.md`'s "narrowing
selects the same single row". The 5428 sentence is about the reducer-internal
`cicdImageMatchesForRepository` narrowing, which is scoped to a single digest;
the pinned MCP query filters on `source_repository_id` across all digests and
scopes, so 5428 does not measure the set this ceiling bounds.

## Cost

The `container-image-identity` cost budget moves from 1 statement to 2, and
stays at 2 after the fence: the retire is a single set-based `DELETE` regardless
of decision count, so the per-decision bound the budget actually guards is
unchanged in shape. The N+1 negative control costs 4 and still exceeds the
budget.

Note what that budget does NOT bound. It counts statements, so it was blind to
the dropped stamping CTE's per-row cost — a second `fact_records` row version per
canonical decision, on every intent execution, inside a statement count that
never moved. Row-version cost is now pinned separately by
`TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive`.

The batched insert gains one bind array (`fencing_token`, 16 columns instead of
15) and one comparison in its `ON CONFLICT` clause. No extra statement, no extra
round-trip. `reducerFactBatchSize` is unchanged at 1000, and 1000 rows x 16
columns is still well under Postgres' 65535 bind-parameter ceiling. Callers that
leave `reducerFactRow.FencingToken` at zero write `0`, which is the column
default they already had, and `0 <= 0` admits their update unchanged — pinned by
`decodeCloudInventoryBatchedRows` on the bind side and by
`TestReducerFactBatchInsertStaysInertForUnfencedWritersLive` on the SQL side.

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

### What `RetiredMoreThanWritten` does not reach

The partial-gap signal is `retired > CanonicalWrites`, so it only fires once the
shrink outweighs the pass's own surviving write count. Swept against the
production writer with the retire's `RowsAffected` driven directly:

```
canonical=6 retired=4  | blind=false moreThanWritten=false   <- 4 images lost, no warn, un-flagged
canonical=9 retired=1  | blind=false moreThanWritten=false
canonical=5 retired=5  | blind=false moreThanWritten=false
canonical=4 retired=6  | blind=false moreThanWritten=true
canonical=1 retired=4  | blind=false moreThanWritten=true
canonical=0 retired=10 | blind=true  moreThanWritten=true
```

The first row is a real four-image evidence-visibility gap that leaves both flags
false and `EvidenceSummary` reading `retired_without_canonical_writes=false
retired_more_than_written=false`. It is un-flagged rather than un-counted — the
same summary string also carries `canonical_writes=6 retired=4` — but the raw
pair reaches no operator on a succeeding pass, because
`Service.recordReducerResult` logs `SubDurations` and `SubSignals` and not
`EvidenceSummary`, and `ReducerQueue.Ack` takes the `Result` as `_`. A suspected
sub-threshold shrink is therefore cross-checked outside this writer, on
`eshu_dp_container_image_identity_decisions_total` (exact_digest falling,
unresolved rising) or on the OCI collector's `oci_registry.warning` facts and
`eshu_dp_oci_registry_api_calls_total{result="error"}`.

The sweep also confirms the warn exclusivity:
`canonical=0 retired=10` sets BOTH struct fields but emits only the blind warn,
because `partialRetire` is gated on `!blindRetire`.

The coarser signal is accepted rather than widened, and the two comments now
state the gap instead of implying full partial coverage. A fuller signal needs a
baseline the writer does not hold: the partition's row count from BEFORE the
write. Neither committed statement supplies it. The two candidate routes are a
third statement (a pre-write `COUNT(*)`, which breaks the committed
two-statement cost budget and adds a round-trip to the hot write path) or a
`RETURNING` readback on `reducerFactBatchInsertQuery` to separate net-new rows
from updated ones — but that statement is shared with six other callers, and
adding a readback to it is exactly the change #4444 had to prove live on the
collector path. Either would need its own live proof, its own concurrency
argument, and its own cost-budget revision, none of which belong in a
comment-accuracy pass. Not implemented; stated.

### The single-DELETE guard was evadable on its own

`TestContainerImageIdentityRetireQueryIsASingleDeleteAndNothingElse` ran its
keyword scan against the raw statement text, which made every check in it
case-sensitive, and it forbade only `UPDATE`, `INSERT` and `WITH `. Two evasions
were measured by appending to the production constant in a throwaway worktree and
running that test ALONE:

```
payload: ...AND fencing_token <= $5; update fact_records set fencing_token = 0
  before: ok    (all three checks passed a lowercase second write statement)
  after:  FAIL  "retire statement contains a `;` statement separator"

payload: ...AND fencing_token <= $5 returning fact_id
  before: ok    (no semicolon, lowercase, and RETURNING was not on the list)
  after:  FAIL  "retire statement contains \"RETURNING\""
```

The composite was never actually broken:
`TestContainerImageIdentityRetireQueryIsBoundedToItsOwnPartition` compares the
normalized production constant byte-for-byte against
`containerImageIdentityRetireStatement`, so both payloads redden the package. The
problem was that the test whose NAME promises "a single DELETE and nothing else"
was not the test holding the line, and the next person to touch the statement
would read the name and believe otherwise.

The scan now uppercases before matching, rejects a `;` statement separator
outright (a keyword list cannot see what a second statement does), and adds
`RETURNING`, `MERGE` and `TRUNCATE`. `isContainerImageIdentityRetireStatement`,
the recognizer that decides which exec call IS the retire, was case-sensitive for
the same reason and is now case-insensitive — a lowercased rewrite would have made
the retire vanish from the call list rather than fail an assertion, which is the
precise failure mode that recognizer's doc comment says it exists to prevent.

What the hardened guard still cannot see is a side effect smuggled inside the one
DELETE, such as a volatile function in the `WHERE` clause. Only the byte-exact
frozen-text comparison catches that. The test comment now says so instead of
implying the keyword scan is sufficient.

### The frozen-text guard normalized away a byte PostgreSQL rejects

The guard the section above defers to — the byte-exact comparison — was itself
normalizing through `strings.Fields`, which splits on `unicode.IsSpace`. That
counts U+00A0 NO-BREAK SPACE as whitespace. PostgreSQL does not, and rejects a
statement containing one. Injecting `0xC2 0xA0` after `DELETE FROM` in the
production constant, in a throwaway worktree:

```
payload: DELETE FROM<U+00A0>fact_records ...
  before: ok    (frozen-text comparison AND the keyword scan both passed)
  after:  FAIL  "retire statement changed" — the U+00A0 survives normalization
```

This is not a silent-correctness hole: the statement fails loudly at the
database. It is a hole in the guard everything else in that file leans on, and a
frozen-text comparison that erases a byte the database refuses is not comparing
the text that ships. `normalizeReducerSQL` now splits on the six ASCII
whitespace characters only, so any other Unicode space stays inside a field and
reaches the comparison.

The accepted cost is stated in the helper's comment:
`isContainerImageIdentityRetireStatement` shares the normalizer, so the same
injected statement now vanishes from the exec-call list rather than failing in
place — the weaker failure mode that recognizer otherwise exists to avoid. It is
accepted because the frozen-text comparison reddens the package on that input
regardless. `TestContainerImageIdentityRetireNormalizerKeepsNonASCIIWhitespace`
pins the property so the next `strings.Fields` reintroduction fails in the
package that owns it.

### The live proofs' 90s budget was shared with one-time schema DDL

`containerImageIdentityRetireLiveDB` created ONE 90-second context and used it
for both `ApplyBootstrap` and the proof. On a cold database, with this host
saturated, the DDL consumed the budget and the first run reddened:

```
--- FAIL: TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive (90.00s)
    apply bootstrap schema: ... context deadline exceeded
```

Reproduced twice against a fresh schema, failing at a different bootstrap step
each time (`webhook_refresh_triggers`, then `service_evidence_snapshots`) —
which is the signature of a budget running out mid-DDL rather than of any one
step being broken. Warm re-runs against the same database passed 8/8.

Cold schema alone does not cost that, and an earlier revision of this section
said it did. Measured after the fact against fresh `postgres:18-alpine`
containers holding 0 tables, on this same host once the CPU load generators
described below were reaped (1-minute load average 3.6 to 13),
`ApplyBootstrap` builds all 178 tables in
**849.9 ms** and **1.00 s** across two separate containers, and the whole test
under the old single shared budget — the unfixed helper at `0e7c88a30`, the last
commit before the split landed in `4a69b8769` — passes cold in **0.87 s**:

```
# Isolated ApplyBootstrap cost, via an uncommitted timing shim in a throwaway
# worktree at 0e7c88a30, against a fresh container holding 0 tables:
$ docker run -d --name cold5847-pg-a -p 127.0.0.1:54871:5432 postgres:18-alpine
COLD_DDL_TABLES_BEFORE=0 COLD_DDL_TABLES_AFTER=178 COLD_DDL_ELAPSED=849.903625ms
# repeated on a second fresh container (port 54873): COLD_DDL_ELAPSED=1.00118825s

# The unfixed helper itself, another fresh container (port 54872), 0 tables:
$ ESHU_POSTGRES_DSN=postgres://eshu:eshu@127.0.0.1:54872/eshu?sslmode=disable \
    go test ./internal/storage/postgres \
      -run '^TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive$' \
      -count=1 -v
--- PASS: TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive (0.87s)
```

That block pins `postgres:18-alpine`, while every other measurement in this
document ran on Postgres 16 (the throwaway `eshu-cii-retire-pg` container, and
the 16.14 that returned the `40P01`). 18 was used because it is what CI already
stands up for this package's live tests:
`.github/workflows/reducer-contention-gate.yml` runs
`go test ./internal/storage/postgres/` under `ESHU_POSTGRES_DSN` against
`image: postgres:18-alpine` — though that lane's `-run` regex selects only the
contention, rank-once, and sign-in gates, not these retire proofs. The two sets
are not being compared: these are schema-setup timings sizing a deadline, not a
retire or deadlock result held against any Postgres 16 figure above.

The budget was roughly 90x the cold-DDL cost, not short of it. What the first
account left out is the machine: those reds were produced while this host sat at
a load average near 200, from CPU load generators an earlier session leaked and
never reaped; killing them dropped the load to 65 within seconds. The trigger is
cold schema AND a saturated host, not cold schema alone. A first-time runner on
an idle machine passes; a first-time runner on a heavily loaded one is the one
who sees that red.

The fix stands, but for budget coupling rather than for cost or for attribution.
The red above is not a mis-attributed failure: it is `apply bootstrap schema`,
emitted by a `t.Fatalf` that predates the split — the same line is present at
`d41b972b8`, `0cc4427b3`, `1eec626f3`, and `0e7c88a30` — so the pre-split helper
already named the setup phase, and the `--- FAIL:` header names the retire test
in both shapes because that is the test Go's runner had entered. That red is
bootstrap exhausting the entire budget, correctly labelled as such.

What one shared deadline does hide is a bootstrap that is slow but FINITE. The
pre-split helper created its single 90-second context BEFORE calling
`ApplyBootstrap`, so setup consuming 30 seconds left the proof 60, and a retire
that then timed out would surface inside the retire with nothing about the
retire being slow — that is the mis-attribution risk, and it never leaves a
`bootstrap` string behind to correct the reading. Splitting removes the coupling:
the proof's clock starts after `ApplyBootstrap` returns, so it gets its full 90
seconds whatever setup cost, and a genuine hang in the retire path still trips
it.

`ApplyBootstrap` runs under its own 5-minute allowance. That number is headroom,
not a derived size: `ApplyBootstrap`'s duration under the load average near 200
that produced the red was never measured, so 5 minutes was chosen to sit far
above the 0.85-1.0 s cost measured above at load 3.6-13, rather than fitted to
the loaded-host case.

### The transient no-DSN `internal/storage/postgres` failure

The earlier best candidate was `deferred_maintenance_concurrency_test.go`, whose
`t.Fatal`s sit behind 2-second wall-clock deadlines. That is disproven: 0 failures
in 60 consecutive runs of the maintenance-concurrency tests under 20-way CPU
saturation on a 10-core host.

A different load-sensitive test in the same package reproduces instead. Running
the whole package 30 times under 24-way CPU saturation:

```
=== ITER 16 FAILED ===
--- FAIL: TestIdentityEpochCacheConcurrentSingleflight (0.25s)
    identity_epoch_cache_test.go:278: probeCalls=64 loadCalls=1 elapsed=250.184666ms (probeDelay=20ms)
    identity_epoch_cache_test.go:290: 32 concurrent callers took 250.184666ms, want <= 200ms
        (mu must not serialize the epoch probe across callers)
FAILS=1 / 30 under 24-way CPU load
```

It asserts a 200 ms wall-clock ceiling on 32 concurrent callers against a 20 ms
probe delay — a 10x margin that a saturated scheduler can still eat. The same
binary run without competing load passed 30/30. This branch touches neither that
file nor the epoch cache, and the failure is a timing budget rather than a
correctness assertion, so it is recorded here and left alone: retuning an
unrelated package's timing test on the strength of one reproduction does not
belong in this change.
