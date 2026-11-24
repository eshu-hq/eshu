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
