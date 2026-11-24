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

Performance Evidence: PENDING, not measured here. The ruling's proof T4 (the stored
read, the retraction DELETE and the recovery read at 20,000 scope rows with a
10,000-id keep set, EXPLAIN (ANALYZE, BUFFERS)) needs a coordinator Postgres slot
and runs before this PR opens. This note is updated with its plans and numbers. The
read adds one primary-key join per stored row (a bounded index probe on
`fact_records`) and drops the `is_tombstone` and kind predicates; whether that
costs or saves is exactly what T4 measures.

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
- `stagedWinnerFactIdentSQL` (a point fetch of one fact's identifier) and the
  staged audit scan stay on facts: both belong to the audit path. Whether the
  staged scan counts as a maintainer input under "full, keyed and staged" is a
  question for the arbiter's reading; it was left on facts.

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
