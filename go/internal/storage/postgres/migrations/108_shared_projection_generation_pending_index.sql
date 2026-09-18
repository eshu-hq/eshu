-- #6738: CountActiveGenerationsByAge and RecoverWedgedActiveGenerations
-- probe unfinished intents by generation_id. The existing pending indexes
-- start with projection_domain or scope_id, so this exact lookup otherwise
-- scans a growing unfinished-intent backlog for each active generation.
--
-- Ops-qa had 753 active generations and an estimated 520K pending intents;
-- its read-only liveness plan used a correlated sequential intent scan with
-- cost 157729, above its JIT threshold. A disposable PostgreSQL 18 fixture
-- with those cardinalities measured the direct liveness query at 10.8-12.2
-- ms before this index and 1.9-2.0 ms after. The partial index was 3.57 MiB.
-- These fixture timings are not a claim about ops-qa runtime.
--
-- One statement per file is required for a concurrent index build. The
-- partial predicate excludes completed intents from the lookup and its write
-- cost. Concurrent construction lets active intent writers continue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS shared_projection_intents_generation_pending_idx
    ON shared_projection_intents (generation_id)
    WHERE completed_at IS NULL;
