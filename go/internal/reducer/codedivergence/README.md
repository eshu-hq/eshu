# internal/reducer/codedivergence

## Purpose

Materializes drifted parallel-implementation findings: function bodies that
were once the same and now differ (epic #6833, child #6837). This is the
reducer intent handler and durable-write side of the code-divergence report;
the #6836 read surface (`go/internal/query/codedivergence`) serves exact and
renamed equality findings by grouping fingerprint columns on read, while
drifted (Jaccard) pairs are too expensive to compute on read and are
materialized here per repo generation.

Pipeline per `(repo_id, generation_id)` intent: the handler loads candidate
pairs from the LSH band side table (`code_fingerprint_band`, shared-band
count desc, at most `MaxCandidatesPerEntity` partners per entity),
verifies each by exact Jaccard over the persisted shingle sets
(`code_function_fingerprint.shingles`), runs the intentional-parallel
suppression catalogue (reusing the per-rule functions and counters from
#6836) with drift-specific reasons, and the writer publishes admitted pairs
as `reducer_code_drifted_finding` facts. Findings from superseded
generations retire with the generation; the read path serves the active
generation only.

## Ownership boundary

**Owns:** `Jaccard`/`Admitted` verification, the candidate budget
(`MaxCandidatesPerEntity`), the drifted finding writer and its
generation-authoritative retire, handler telemetry (candidates generated,
pairs verified/rejected, findings written, budget exhaustions, handler
duration, dead-letter count).

**Does not own:** fingerprint emission (`internal/parser/fingerprint`), the
equality read surface and suppression rule functions
(`internal/query/codedivergence` — imported, not duplicated), the fact-kind
registry entry and payload schema (`specs/fact-kind-registry.v1.yaml`,
`sdk/go/factschema/`), or the trigger that enqueues one intent per repo
generation.

## Scaling bounds (from #6834 §5–§6, measured)

- Ship threshold Jaccard ≥ 0.7 (≥98% precision on 500 hand-labelled pairs).
- Per-entity candidate budget K = 200 ordered by shared-band count desc
  (preserves 100% of measured ship-band recall; replaces disproven row-cap).
- Partition by `repo_id`; budget exhaustions counted, never silently dropped.
- Worker/batch cuts are not an acceptable fix for conflicts found here.

## Exported surface

| symbol | what it is |
|---|---|
| `Jaccard` | exact Jaccard over two shingle identity sets |
| `Admitted` | threshold gate: pair becomes a drifted finding |
| `DriftedSimilarityThreshold` | ship threshold 0.7 |
| `MaxCandidatesPerEntity` | per-entity verification budget 200 |
