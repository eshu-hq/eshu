# #5847 — a reopened `container_image_identity` replay leaves the superseded decision live

`container_image_identity` sits in the bootstrap maintenance reopen slice, its
fact identity embeds `outcome`, and the writer has no retire. A replay that
reaches a different answer than the first execution therefore does not correct
anything: it adds a second row and leaves the first one live for the same active
generation.

**#5847 is still open after this branch.** The branch was built as the retire
that closes it, the retire was withdrawn under review, and what ships is the
half that is safe on its own: the durable write now carries an evidence-read
watermark and the shared batched insert's conflict clause is guarded on it, so
two passes that COLLIDE on one `fact_id` resolve in favour of the fresher
evidence. The duplicate-row half — deleting the superseded row a re-classified
or demoted replay leaves behind — is tracked as **#5854**, and
[Why the retire is not in this branch](#why-the-retire-is-not-in-this-branch)
is the reason it cannot land until the OCI collector's bounded-degradation paths
are fixed.

This file keeps its original name because it is the #5847 record. Sections
marked *(withdrawn retire)* describe measurements taken against the retire this
branch no longer ships; they are retained because #5854 has to re-derive every
one of them and should not pay for them twice.

**On this file's length.** This record runs well past the general 500-line cap
in `CLAUDE.md` — comfortably over 800 lines, and growing with each correction
it absorbs. An exact figure is not quoted here on purpose: the first draft of
this paragraph quoted one, and the very edit that corrected the counts below
invalidated it. A self-referential line count is stale the moment anyone edits
the file, including whoever is editing it to fix the count. Run `wc -l` if you
need the number. The enforced gate, `precommit-go.sh filecap-all`,
applies that cap to Go sources only and passes here; the rule text is general,
so the overage is real and is recorded rather than argued away. Five of the 80
evidence records on `origin/main` already exceed 500 lines, the longest at 1011.
That was four of 80 when this branch opened; #5850 took
`5426-golden-corpus-coverage.md` to 612 lines, which is a reminder that a count
of other people's files goes stale under you and has to be re-derived at the
base you actually merge onto.
The file is not split, because #5854 has to re-derive the measurements kept here
and will EXTEND this record rather than replace it, so a split now would only
have to be undone.

The duplicate-row defect is the same one #5837 fixes for
`aws_cloud_runtime_drift`, on a different domain.

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

## Verification of the assumptions a retire would rest on

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

## Why the retire is not in this branch

The retire deletes on the strength of ABSENCE: a row the current pass did not
write is removed. That is sound only if a pass can never see LESS than the
registry currently holds. An earlier revision of this document argued it could
not, on the grounds that `ociruntime.Source.scanTarget` aborts on every
collection failure instead of committing what it had. Three rounds of analysis
accepted that argument. A later review refuted it with a reproduction, and an
architectural re-verdict confirmed the refutation: the collector has TWO paths
that shrink a generation with nothing upstream asserting a failure.

1. **Config-blob soft-fail.**
   `go/internal/collector/ociregistry/ociruntime/config_provenance.go:56-67`
   downgrades a `GetBlob` failure to a warning envelope and returns nil labels
   with a **nil error**; `source.go:243-246` then emits the manifest fact anyway,
   with `config_labels` simply omitted. `config_labels` is ref-CREATING —
   `container_image_identity_evidence.go:109-115` seeds `byRef` from it — so a
   transient 429, 5xx, or timeout demotes every digest whose reference existed
   only through that label. A retire deletes the corresponding row.
2. **Tag-list truncation.** `distribution/client.go:155-172` is unpaginated, and
   `source.go:161-165` truncates the result lexicographically to `TagLimit`
   (default 20). A new low-sorting tag evicts the tail; the evicted tag's
   observation vanishes and the prior `tag_resolved` row is retired. No failure
   occurs anywhere in that sequence.

The collector is deliberately designed for bounded degradation, so an activated
generation is known-incomplete BY DESIGN. Under additive semantics both
mechanisms cost only under-reporting, which is benign. A generation-authoritative
retire converts them into destruction of valid rows — the exact P1 the review
raised, and the reason the DELETE is out of this branch rather than shipped with
a caveat.

Two consequences for #5854. The collector fixes belong to it, not here (this
branch deliberately does not touch
`go/internal/collector/ociregistry/**`). And the writer-side signals this branch
built to make a suspicious retire findable —
`RetiredWithoutCanonicalWrites`, `RetiredMoreThanWritten`, and their two
`slog.Warn` lines — went with the DELETE rather than staying as flags that can
never fire.

## What ships instead: the fenced upsert

Nothing here removes a row. What ships orders the case where two passes write the
SAME `fact_id`.

The fact identity embeds only `(scope_id, generation_id, image_ref, outcome)`,
while `source_revision`, `source_revision_provenance`,
`build_provenance_repository_ids` and `evidence_fact_ids` are payload-only and
are filled in by cross-scope enrichment
(`applyCIRunDigestRevision`/`applySLSADigestRevision`) whose visibility depends
on which generations are active at load time. So two passes that agree on the
outcome collide on one row with DIFFERENT payloads, and the pass that read before
the CI/SLSA generation activated carries the poorer one. Before this branch the
last writer won, whichever it was.

### The watermark

`ContainerImageIdentityWrite.EvidenceAsOf` is captured by the handler
immediately BEFORE its first fact load and rendered as microseconds into
`fact_records.fencing_token`. Evidence-read time, not write time: write time
ranks the stalled worker highest, which is the inversion the watermark exists to
stop. The reducer queue does not order the two for you — its claim-batch
in-flight exclusion requires a LIVE lease while the base predicate re-admits an
item whose lease has expired, and lease expiry IS the stalled-worker case (see
check 6 above). `Intent.AttemptCount` does not work either, because the
reopen-succeeded statement resets it to 0, so a reopened replay would rank below
the run it exists to repair.

A zero `EvidenceAsOf` is a hard error, never a defaulted unfenced write: rows
carry `fencing_token` 0 by table default, so a row left there is
indistinguishable from one written by a domain that never opted in, and
`0 <= EXCLUDED.fencing_token` admits every later pass unconditionally. The
domain would look fenced and behave unfenced with nothing saying so.

The token is stamped by the **insert** (`reducerFactBatchInsertQuery` now writes
`fencing_token`), which is what gives the conflict guard something to compare
against. `reducerFactBatchInsertVersionedQuery` does NOT carry the column, and
its doc comment says so.

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

The shipped guarded form, same three live proofs, re-run after the retire was
removed against a throwaway `postgres:16-alpine` on 127.0.0.1:55849
(`eshu-cii-noretire-pg`, removed afterwards):

```
$ ESHU_POSTGRES_DSN=postgres://eshu:eshu@127.0.0.1:55849/eshu?sslmode=disable \
    go test ./internal/storage/postgres \
      -run 'ReducerFactBatchInsert.*Live|ContainerImageIdentity.*Live' -count=1 -v
--- PASS: TestReducerFactBatchInsertRejectsStaleContentUpsertLive (2.61s)
--- PASS: TestReducerFactBatchInsertAppliesEqualTokenRetryLive (0.20s)
--- PASS: TestReducerFactBatchInsertStaysInertForUnfencedWritersLive (0.16s)
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres  18.282s
```

The `-run` pattern deliberately still admits `ContainerImageIdentity.*Live`: it
matches nothing now, which is the check that the five retire proofs are gone
rather than silently skipping. Those are cold-schema times — the first test
absorbs `ApplyBootstrap`.

That is the before/after pair for this section: the same first test FAILs under
the `GREATEST` form (transcribed above) and PASSes under the guarded one.

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

### What the guard does not close

It orders a COLLISION; it does not remove a duplicate. A replay that
re-classifies an image mints a different `fact_id` and never collides at all, and
a replay that demotes an image out of the canonical outcomes writes no row to
collide with. Those are the two shapes #5847 is about, and they stay open for
#5854.

Nor does it tell an operator when it fires. A pass fenced out in WHOLE still
reports `CanonicalWrites=N`, which is byte-identical to a pass that landed
normally against an unchanged partition. The rows are right either way; the
summary cannot distinguish them. Reading back the accepted `fact_id`s — the
`upsertFactBatchReturningAccepted` shape #4444 had to prove live on the collector
path — would close that, and needs its own live proof and concurrency argument.
Stated, not implemented.

The reopen-slice comment in `go/cmd/bootstrap-index/bootstrap_pipeline.go`
claiming the decision "upserts on a scope-keyed stable fact key, so replay is
idempotent" was false and is corrected — it now names #5847 as the open bug and
#5854 as the fix. `go/cmd/bootstrap-index/AGENTS.md` gains the general rule for
the next domain added to that slice: what to check about the fact identity
BEFORE reopening it, and the bounded-degradation audit a retire needs before it
can be the answer.

## Proof

Every number below was produced against a throwaway Postgres 16 container
(`eshu-cii-retire-pg`, host port 55847) created for this work. Measurements taken
against the withdrawn retire are labelled as such and are kept for #5854.

### Failing before, passing after

The shipped change is the guarded upsert, and its regression is the live one: the
same test FAILs against the token-only `GREATEST` form and PASSes against the
guarded one, transcribed under
[The conflict clause is guarded, not merged](#the-conflict-clause-is-guarded-not-merged).

The credential-free half is `TestReducerFactBatchInsertFreezesItsConflictGuard`,
which freezes the whole `ON CONFLICT` clause, plus the four
`ContainerImageIdentity*` fence unit tests (token direction, stamped on the
insert, hard error on a missing watermark, watermark taken before the load).
Cutting the `WHERE` guard from the production statement reddens the first; the
three live proofs are DSN-gated and skip without one, which is why the frozen
clause exists.

### Live proofs

Three live proofs ship, all gated on `ESHU_POSTGRES_DSN` alone, all covering the
shared insert's conflict guard. They are run and transcribed under
[The conflict clause is guarded, not merged](#the-conflict-clause-is-guarded-not-merged).

The branch previously carried five more, covering the retire's partition
bounding, the empty-keep-set clear, the stalled-worker fence, the insert/stamp
window, and the keep-set row-version count. Those were removed with the DELETE;
their findings are the two subsections above.

They live in `go/internal/storage/postgres`, not `go/internal/reducer`. An
earlier revision put them in package `reducer` behind an extra
`ESHU_CONTAINER_IMAGE_IDENTITY_RETIRE_LIVE=1` flag that appears nowhere in
`scripts/` or `.github/`, over a hand-rolled four-column `fact_records` with no
`fencing_token`, no `is_tombstone`, and no indexes — so it could never run in CI
and could not have exercised the guard at all. `internal/storage/postgres`
importing `internal/reducer` is the reason the test belongs on this side: it is
the direction that compiles, and it is where `ApplyBootstrap` gives the real
table with its real foreign keys.

### Mutants

Applied to the final code, run against the throwaway Postgres, and reverted.

```
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token cut from the insert
  FAIL TestReducerFactBatchInsertFreezesItsConflictGuard
  FAIL TestReducerFactBatchInsertRejectsStaleContentUpsertLive

guard rewritten as fencing_token = GREATEST(existing, excluded), content unguarded
  FAIL TestReducerFactBatchInsertFreezesItsConflictGuard
  FAIL TestReducerFactBatchInsertRejectsStaleContentUpsertLive
       stored source_revision/provenance = ""/"", want "commit-fresh"/"ci_run_commit"

insert no longer stamps at birth (fencingToken forced to 0)
  FAIL TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert
       inserted fencing_token = 0, want 1785146400000000
```

The first two mutants live in the SQL and the third in the writer's argument
build, which is why the frozen-clause test and the stamp test are separate: no
amount of auditing the statement text would catch a writer that stopped binding
the column. Note also that the GREATEST mutant is the one the frozen-clause test
earns its keep on — it leaves `fencing_token` in the statement, so a substring
probe for the column name would pass it.

Three further mutants were run against the withdrawn retire (`OR TRUE` appended
to the DELETE, the fence predicate cut from the DELETE, and the born-unstamped
insert proven against a real interleaving). #5854 has to re-run them.

### Gate

```
$ bash scripts/test-verify-golden-corpus-gate.sh
test-verify-golden-corpus-gate: pass
```

The `list_container_image_identities` MCP shape pins `maximum_results: 1`
alongside its existing `minimum_results: 1`, and the ceiling is KEPT even though
the retire it was added for is not. Its job changes rather than disappearing.
`QueryShape.MaximumResults` is a generally-available ceiling in
`go/internal/goldengate` (the query-shape counterpart to `RequiredNode`'s
`MaximumNodePropertyCount`), covered by `TestEvaluateQueryShape`'s
`maximum results ceiling` and `maximum results without an array result field`
subtests.

#### What the ceiling covers, and what it does not

#5847 has two shapes, and the ceiling sees one of them.

- **Duplicate (covered).** A replay that RE-CLASSIFIES an image mints a second
  `fact_id` beside the first, and both stay live. The floor alone cannot see
  that — an ANY-match "at least one identity for this repository" assertion
  passes identically on one correct row and on that row plus its superseded
  contradiction. The ceiling is what makes this shape visible to a committed
  gate if the corpus ever starts producing it.
- **Demotion (NOT covered).** A replay that demotes an image out of the two
  canonical outcomes (`exact_digest`, `tag_resolved`) writes no row at all. The
  stale row stays live, the count stays 1, and the ceiling passes. The demoting
  fixture that would close this gap is **#5853**; nothing in this branch covers
  it.

So the ceiling makes the DUPLICATE shape of #5847 visible to a committed gate.
It is not a detector for #5847 as a whole.

#### Why the value is 1 — an inference, not a measurement

The value 1 is an INFERENCE from a main-era measurement, not a measurement of
this narrowing.

The measurement is real. The sibling `list_supply_chain_impact_findings`
description — committed on `origin/main`, measured on the live corpus post-#5810
— records that this digest carries 16 `reducer_container_image_identity` rows and
that "only ONE ... names the BUILDING repository
(github.com/eshu-hq/supply-chain-demo, repository:r_69256c06)", which is the
`source_repository_ids` value this assertion narrows on. The other fifteen name
only the deploying repository. Nothing in this branch adds or removes a row from
that set.

What makes it an inference is scope. That measurement is DIGEST-scoped, while
the pinned MCP query filters on `source_repository_id` across all digests and
scopes. It therefore bounds a subset of the set the ceiling bounds, and the
ceiling has never been run through the LIVE golden-corpus gate — only through
`scripts/test-verify-golden-corpus-gate.sh`, which exercises the gate script
rather than the corpus. Nothing has yet counted the rows this narrowing actually
returns.

**Falsifier:** a live-gate count above 1 on
`list_container_image_identities?source_repository_id=r_69256c06` disproves the
inference and the ceiling has to move.

That same scope mismatch is why
`docs/internal/evidence/5428-built-from-projection-rescinded.md`'s "narrowing
selects the same single row" is not the evidence either. The 5428 sentence is
about the reducer-internal `cicdImageMatchesForRepository` narrowing, which is
likewise scoped to a single digest.

Keeping the ceiling is still the right call — it is the only committed detector
for the duplicate shape — but its stated basis is an inference with a named
falsifier, not a measurement of the set it bounds. The ceiling was in the same
position before the retire was withdrawn; the value was argued from the same
main-era measurement then too.

## Cost

The `container-image-identity` cost budget is UNCHANGED at 1 statement and 1
`eshu_dp_postgres_query_duration_seconds` write observation per intent execution.
It moved to 2 while the retire was in the branch and moves back with it. The N+1
negative control costs 2 and still exceeds the budget.

Note what that budget does NOT bound. It counts statements, so it was blind to
the withdrawn retire's dropped stamping CTE — a second `fact_records` row version
per canonical decision, on every intent execution, inside a statement count that
never moved. #5854 needs a row-version assertion, not only a statement one; the
`xmin` shape is recorded above.

The batched insert gains one bind array (`fencing_token`, 16 columns instead of
15) and one comparison in its `ON CONFLICT` clause. No extra statement, no extra
round-trip. `reducerFactBatchSize` is unchanged at 1000, and 1000 rows x 16
columns is still well under Postgres' 65535 bind-parameter ceiling. Callers that
leave `reducerFactRow.FencingToken` at zero write `0`, which is the column
default they already had, and `0 <= 0` admits their update unchanged — pinned by
`decodeCloudInventoryBatchedRows` on the bind side and by
`TestReducerFactBatchInsertStaysInertForUnfencedWritersLive` on the SQL side.

Performance: the write path gains no statement. No hot-path Cypher, graph write,
or query handler changed.

No-Observability-Change: nothing here destroys a row, so there is nothing for a
new counter to report. The insert runs inside the already-instrumented
`InstrumentedDB.ExecContext` wrapper that records
`eshu_dp_postgres_query_duration_seconds`; a write rejected by
`validateContainerImageIdentityFence` before any statement is issued surfaces as
a non-success `status` on `eshu_dp_reducer_executions_total` (labeled
`domain`=`container_image_identity`); and the existing
`eshu_dp_container_image_identity_decisions_total` counter and reducer run spans
are unchanged. The one gap is named under
[What the guard does not close](#what-the-guard-does-not-close): a pass fenced
out in whole is indistinguishable in the summary from a pass that landed.

## Records carried forward for #5854 *(withdrawn retire)*

The four subsections below measured the retire this branch no longer ships. They
are kept because each cost real machine time and #5854 would otherwise repeat
them. The tests they name were deleted with the DELETE.

### The born-unstamped hole, reproduced and closed *(withdrawn retire)*

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

### The keep-set stamp was a no-op that still rewrote every kept row *(withdrawn retire)*

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

The retire this branch withdrew had been reduced to a bare fenced `DELETE`
because of this. #5854 must ship it that way, and must keep a guard that rejects
a reintroduced `WITH`, `UPDATE`, or `INSERT` — the shape of that guard, and the
two ways it was evadable, are the subsection after next.

### What `RetiredMoreThanWritten` did not reach *(withdrawn retire)*

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

### The single-DELETE guard was evadable on its own *(withdrawn retire)*

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

## Review findings that still apply

### The frozen-text guard normalized away a byte PostgreSQL rejects

A frozen-statement guard is only as good as the normalizer it compares through,
and this one was normalizing via `strings.Fields`, which splits on
`unicode.IsSpace`. That counts U+00A0 NO-BREAK SPACE as whitespace. PostgreSQL
does not, and rejects a statement containing one. Injecting `0xC2 0xA0` into the
production constant, in a throwaway worktree — measured on the retire statement
this branch then carried:

```
payload: DELETE FROM<U+00A0>fact_records ...
  before: ok    (frozen-text comparison AND the keyword scan both passed)
  after:  FAIL  the U+00A0 survives normalization
```

This is not a silent-correctness hole: the statement fails loudly at the
database. It is a hole in the guard the frozen comparison leans on, and a
frozen-text comparison that erases a byte the database refuses is not comparing
the text that ships. `normalizeReducerSQL` splits on the six ASCII whitespace
characters only, so any other Unicode space stays inside a field and reaches the
comparison.

The normalizer outlived the statement it was written for. It now backs
`TestReducerFactBatchInsertFreezesItsConflictGuard`, which freezes the shared
batched insert's whole `ON CONFLICT` clause — including the
`fencing_token <= EXCLUDED.fencing_token` guard, which is the thing this branch
must not let a later change weaken. `TestReducerSQLNormalizerKeeps
NonASCIIWhitespace` pins the U+00A0 property against that statement, so the next
`strings.Fields` reintroduction fails in the package that owns it.

### The live proofs' 90s budget was shared with one-time schema DDL

The live-test helper (`containerImageIdentityFenceLiveDB`, then named for the
retire) created ONE 90-second context and used it
for both `ApplyBootstrap` and the proof. On a cold database, with this host
saturated, the DDL consumed the budget and the first run reddened:

```
--- FAIL: <a live proof in this family> (90.00s)
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
