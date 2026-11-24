# PR-C1 b: the writer's stored read leaves fact_records (#10880)

The impact writer reads, inside its transaction and under the (scope, generation)
conflict-domain lock, the rows it is about to overwrite: the generation being
written and the scope's previous generation. PR-C stops writing the finding facts,
so that input must come from the supports table. This change moves it there while
the facts are still written, so the facts stay the oracle for the equality proof.

## What the read does now

`listStoredSupplyChainImpactFindingsQuery`
(`go/internal/reducer/supplychain/core/writer_stored_supports.go`) reads
`supply_chain_impact_finding_supports AS s`. Payload-derived columns are the legacy
expressions over `s.payload`, which is the fact payload verbatim. `cve_id` and
`advisory_id` are the support's derived columns. The stable key is
`COALESCE(s.stable_fact_key, twin.stable_fact_key, '')`, the twin being the fact
row joined by primary key, for supports written before migration 350. The fencing
token is the constant 0: the per-row guard is redundant with whole-pass admission
(the fact id embeds scope and generation; the pair lock and the admission reject a
stale pass). A row with no key on either side reads under a unique
`missing-stable-key:<fact id>` key, so the pass treats it as changed, and is
counted on `eshu_dp_supply_chain_impact_stored_key_missing_total` at commit.

The frozen legacy statement stays in `writer_stored_supports_test.go`, digest
pinned, as the oracle the live equality proof will run on the same seed.

## Evidence

Performance Evidence: PARTIAL, and the arbiter's T1 to T5 are not complete, so this
does not claim the gate is met. The QA replica plan measurements of 2026-11-24
(read-only, the QA replica only, PG 18.6, hot_standby_feedback off,
max_standby_streaming_delay 30s; supports table about 10.69M estimated rows, 96 GB;
`fact_records` about 96.9M, 551 GB), recorded in
`.claude/qa-prc-plans-2026-11-24.md` with raw plans under
`~/.cache/qa-prc/T-20261124T085425Z/`. Statements rendered from this branch at
8898e4657c. Execution ms, first run then second run, supports versus legacy twin:

- Stored read (b), one scope of 14,123 rows: supports 11,608 then 878; legacy 807
  then 798. The supports plan is the scope/generation index plus 14,123
  `fact_records_pkey` probes (the twin join); the first run read 19,351 shared
  buffers. Warm it is within 10% of legacy; cold it is about 14 times slower.
- Recovery reads (c), 2-leg form (61 rows): cve ids 1,206 then 541 versus 351 then
  350; group ids 372 then 362 versus 347 then 345. Index scans only. This run still
  includes the facts leg, so it is not supports-only.
- Keyed scope idents (d): supports 2,722 then 686; legacy 7,432 then 674.
- Keyed active_facts (d), an identifier of 1,616 legacy rows: legacy 1,289 then 59
  on `fact_records_supply_chain_impact_winners_keyed_idx`; supports is a Parallel
  Seq Scan at estimated cost 3,047,857 (plain EXPLAIN only, not run): the supports
  table has no index on the five payload expressions. This is the proven gap behind
  migration 354 (below).
- Operator facts (d): 0.13 then 0.21 versus 0.11 then 0.10 ms (0 rows).
- Full winner select (d): plain plans only; estimated cost 7,614.81 supports versus
  7,619.14 legacy, both nested-loop scope probes.
- Explain (f): public id 191 then 23 ms (finding_id index); granted 1,805 then 548
  ms; image 0.70 then 0.67 ms on a synthetic image (0 rows; supports has no
  non-empty image_ref). The ungranted CVE-plus-package lookup is plain plan only on
  supports (scope-driven probes, est. cost 7,629, no cve or package btree on the QA
  supports table until migration 345 and the cve/advisory indexes land); its legacy
  twin took 19,545 ms warm (the first run was cancelled at 24,049 ms), so the
  statement the product runs today is already near the replica's limit.
- No sequential scan ran on supports or `fact_records` in any executed plan.

Not measured: the 3-leg recovery read and the stored read with the key column
(migrations 350 and 351 are not on the replica; the 2-leg and the key-less forms
above stand in for them); the 83,642-row keyed case and a 20,000-row scope (not
found on the replica); the retraction DELETE and its retracted-keys fill; exact row
counts; cold versus warm under controlled cache; any write path; the primary; WAL
(T5). Owed before the gate can be claimed: a post-roll re-measure with migrations
345 and 346 present and the candidate index set g built; the live same-rows
differentials for the two contract re-pins; the retraction and recovery live proof;
the scope-delete capture order proof; T5 WAL.

Observability Evidence: `eshu_dp_supply_chain_impact_stored_key_missing_total` (no
labels), documented in `docs/public/reference/telemetry/supply-chain-impact-supports.md`
and the coverage row of `docs/public/observability/telemetry-coverage-reducer-domains.md`.

Hermetic proof only: query-shape tests (supports row source, primary-key twin join,
the fallback key, token 0, the shared generation window, one `fact_records`
mention), a derivation test (each payload expression equals the frozen legacy one
over `s.payload`), the frozen-oracle digest, fake-DB tests of the missing-key rule
and of `changedCVEIDs` over a keyless baseline, and the adapter's hold-and-flush
counting. The equality of the two reads on a seed is a live proof and is not run
here.

## C1 c: the retraction records its keys, and the recovery reads take them

PR-C stops tombstoning facts, but the recovery reads find what a crashed attempt
retracted by reading tombstoned fact rows. Migration 351 adds
`supply_chain_impact_retracted_finding_keys` (primary key `(generation_id, scope_id,
key_hash)`, cascading foreign key as migration 338). Both retraction statements
(`writer_retract.go`) gain two shared fragments from `writer_retracted_keys.go`:
extra RETURNING columns and a `recorded` CTE that inserts one row per distinct
retracted key (cve id, advisory id, advisory-group members, sha256 key hash) in the
same statement and transaction as the tombstone, skipping keys already recorded with
`NOT EXISTS` (no `ON CONFLICT`: the retraction statements keep none on their hot
path, and every writer of a pair's keys runs under the pair's conflict-domain lock).

The two recovery reads become three legs joined by `UNION`: the live supports, the
retracted keys, and the unchanged fact read with tombstones as the transition leg,
byte-identical to the previous statement and pinned by digest. The retention
row-count statement gains an arm for the new table, because its generation foreign
key cascades.

Hermetic proof: both retraction statements reduce to their base digest when the two
fragments are removed; the fill is the same text in both; the legs, their `UNION`
joins, the shared generation window and the byte-identical fact legs are pinned; the
retention arm is pinned. Not run here (live, coordinator slot): the equality of the
three-leg read with the fact read on a seed, a crash-after-commit recovery across
the cutover, and the retention cascade accounting.

## C1 d: the winners maintainer's full and keyed inputs read supports

The canonical-winners maintainer builds its winners from the active finding facts:
the full rebuild's select, and the keyed resweep's scope expansion, per-identifier
probe and operator insert. PR-C stops writing those facts, so the maintainer reads
the supports table instead, while the facts are still written.

`supply_chain_impact_winners_supports_input.go` derives each supports statement from
the shipped legacy constant by declared swaps (the table, and the kind and
tombstone predicates a support makes implicit); a swap that does not match exactly
once panics at initialization. The rebuild, the keyed build_idents list, the keyed
active-facts build and the keyed operator insert use the supports statements. The
audit (`selectWinnersAuditCandidatesSQL`, its keyed active-facts statements, the
scope expansion) and the staged audit scan keep the legacy statements, as the
ruling says: they are the oracle the supports-derived winners are compared with.

The winners contract pin (version 2) is re-pinned, not bumped. A bump runs one full
on every instance after the roll. `TestWinnersContractRepinnedForSupportsInputs`
reverses the declared swaps in every contract statement and requires the previous
pin `8e761819...`. The live byte-identity differential of the stored winners on a
seed is not run here.

Not done, and why:

- The keyed probe on the supports table has no counterpart of
  `fact_records_supply_chain_impact_winners_keyed_idx` (the five-expression index).
  The statements are correct without it and unmeasured with it; the index set is
  PR-C1 g, which waits for T1. The live plan guards that assert the probe on that
  index (`keyed_contract_live_test`, the explain export) still point at the legacy
  statement, which the audit runs, and will need re-targeting with g.
- Staged statements, final reading (lead's rule: a statement moves when the
  winners maintainer's non-audit path consumes it; it stays on facts when only the
  audit consumes it). Caller census, `rg` over `go/` at this HEAD, non-test code:
  `stagedWinnersBatchActiveFactsSQL`, `stagedWinnersSourceBatchSQL` and
  `stagedWinnersOperatorBatchSQL` are read only by `StageBatch`;
  `stagedWinnersCensusSQL` only by `CensusPairs`; `stagedWinnerFactIdentSQL` only by
  `fetchWinnerFactIdent`, reached only from `ResolveCandidateIdents`. None of the four
  entry points (`StageBatch`, `CensusPairs`, `ResolveCandidateIdents`,
  `FetchStoredWinners`) has a caller in the maintainer (rebuild, keyed, scope-settle)
  code. They are the Lane A/B/C pieces of the staged stale-winners audit scan
  (#10451): their file headers say so, and the only runtime consumer is the injected
  plan in `go/internal/runtime/postgres/staged_audit_scan.go` ("The legacy writer
  audit path is untouched"). No statement is shared between the batch path and the
  maintainer, so there is nothing to split. All of them stay on facts, which remain the
  oracle while the dual write lasts; `TestWinnersAuditStaysOnFacts` pins
  `stagedWinnersSourceBatchSQL` on facts. If the lead reads a maintainer caller of
  `StageBatch` that this census missed, the swap is the same mechanism as the other
  four inputs.

## C1 e: the winners dirty-capture functions read supports

Three SECURITY DEFINER capture functions read finding facts to name the keys a
change touches: the scope-delete capture (when a scope row is deleted) and the
scanner insert and delete captures (when a scanner classification or association
row appears or goes). The activation capture reads no fact row. Migration 352
replaces the scope-delete capture and migration 353, public-only like 311, the
scanner pair; each body is the previous migration's (310, 311) with the row source
swapped for the supports table and the kind and tombstone predicates dropped.
`supply_chain_impact_winners_supports_capture_test.go` derives the expected text from
the previous migration file and requires byte equality, so nothing else in the
function changed, and pins the security shape (SECURITY DEFINER, the pinned
search_path, the PUBLIC revoke, no conflict arbiter, the dirty log untouched).

Open, owed to a live run: the scope-delete capture is `BEFORE DELETE` on
`ingestion_scopes`, which fires before the foreign-key cascade removes the scope's
generations and their supports, so the supports are still readable. That order is a
reading of the trigger and cascade rules, not an observation. The ruling's own
condition applies: if the capture reads no supports after the cascade removed them,
this returns to the arbiter. The existing live scope-delete capture test covers it
and has not been run for this migration.

## C1 f: the ungranted explain reads move to supports

With the winners gate on, two explain lookups still read the finding facts: an
ungranted selector lookup (cve, advisory, package, repository or subject digest)
without an image reference, and the identity fallback of an ungranted public-id miss.
`ExplainFindingUngrantedFromSupportsQuery` is the same builder the granted and image
reads use, with the whole supports table as the source and the identity predicate as
the source-candidate predicate; the route function sends both lookups to it under a
new route label, `supports_selector`. With the gate off every call keeps the legacy
read. The route and read-duration metrics, their instrument descriptions, the metrics
catalog and the telemetry README name the new label.

Hermetic proof: `TestExplainRoutesByGrantAndLookup` (ungranted selector reads
supports, the public-id miss falls back to the supports identity read, nothing reads
the finding facts as a candidate source; the runtime filter's legs read other fact
kinds on every route, as before) and `TestExplainCountsEachStatementByRoute`.

Not measured: the statement drives from the cve, advisory, package and repository
btrees for a selector lookup, but a lookup by fact id or canonical key has no
dedicated supports index. That lookup, and the explain plans at the QA shape, are
the ruling's T2 and belong to the C1 index set (g), which waits for a coordinator
Postgres slot. Until then the new statement is correct and its plan is unmeasured.

## C1 g (DRAFT): the keyed probe index on supports

The keyed active_facts plan above is the measured gap. Migration
`354_supply_chain_impact_supports_keyed_probe_idx.sql` is one
`CREATE INDEX CONCURRENTLY IF NOT EXISTS` on the supports table that mirrors the
legacy index's key expressions (migration 291: `package_id`, `repository_id`,
`subject_digest`, the coalesced c0, `advisory_id`, all over the payload) and then
`fact_id`. It uses the payload expressions, not the derived supports columns, so the
probe stays the legacy statement with the row source swapped: the supports columns
carry `COALESCE(..., '')` and the probe compares the raw `->>` value, which differ
for a payload that lacks a key, so a column-keyed probe would be a semantic change
to prove, not a row-source swap. No predicate: a support exists only for a live
finding. `TestKeyedSupportsIndexCoversTheKeyedProbe` requires the index's leading
five keys to equal the probe's equalities in order, derived from the shipped
statements; `TestKeyedSupportsIndexMirrorsTheLegacyKeyExpressions` derives the index
from migration 291; `TestKeyedProbeCoverageRejectsSeededViolations` fails the rule
on a reordered pair, a dropped expression, a plain column in place of the c0
expression, and a sixth probe equality.

This is a DRAFT. The final set is decided after the post-roll QA measurement (an
axios-size package may need a different composite than 345 plus this one), and it
must not be applied inside W1b: the build on the 96 GB table takes hours and adds
WAL. Not measured: the index's size, build time or the plan it gives.
