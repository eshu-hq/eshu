-- #5490: prove-theory-first index for the impact-trace K8sResource SELECTS
-- candidate scan (ListRepoK8sSelectCandidates,
-- go/internal/query/content_reader_k8s_select_candidates.go).
--
-- #5363 measured the worst-case candidate fetch (6,000-K8sResource repo,
-- LIMIT 5001) at ~25 ms wide / ~12.5 ms narrow-projection, and decomposed the
-- cost: the content_entities_type_idx scan + entity_type filter is only
-- ~2.0 ms; a top-N sort on (relative_path, start_line, entity_id) over the
-- matching rows dominates the rest. #5490 measured a composite
-- (repo_id, entity_type) index -- attacking only the scan+filter -- and
-- proved it does NOT move total wall time: even a perfect index-only bitmap
-- scan on that shape left execution time unchanged because the Sort node
-- still dominates. See docs/internal/evidence/5490-k8sresource-candidate-index.md
-- for the full before/after EXPLAIN ANALYZE ladder.
--
-- This index instead matches the query's ORDER BY key
-- (repo_id, relative_path, start_line, entity_id), so the scan itself
-- returns rows pre-sorted and the Sort node is eliminated, and it INCLUDEs
-- the two columns the SELECT list projects beyond the key (entity_name,
-- metadata) so the scan never needs a heap fetch. Measured effect: the
-- worst-case query moved from ~11-19 ms (Index Scan + Sort) to ~1.6-2.0 ms
-- (Index Only Scan, Heap Fetches: 0).
--
-- It is a PARTIAL index (WHERE entity_type = 'K8sResource') specifically so
-- write-amplification is confined to K8sResource content entities -- the
-- small minority of rows in this hot, continuously-ingested table. Measured
-- write cost: ~0 extra ms per 5,000-row batch insert for non-K8sResource
-- rows (Function/Variable/etc, the vast majority of the table), versus
-- ~+24 ms per 5,000-row batch insert (~4.7 microseconds/row) for
-- K8sResource rows, which do maintain this index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_k8s_select_partial_idx
    ON content_entities (repo_id, relative_path, start_line, entity_id)
    INCLUDE (entity_name, metadata)
    WHERE entity_type = 'K8sResource';
