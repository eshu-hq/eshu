# #10535 paged downstream consumers: read-path performance and observability evidence

The new `list_service_downstream_consumers` surface recomputes the merged
consumer set at a widened internal read bound (`MaxConsumerScanBound = 500`)
and pages it with a (repository, repo_id) keyset cursor. No Cypher text
changed: the candidate query is byte-identical (only the caller-side limit
grew), and the content searches keep their indexed `ORDER BY repo_id,
relative_path LIMIT` shape with a +1 truncation probe. This note records why
the widened bound stays inside the repo-scale performance contract.

## Backend

Pinned Neo4j per `docker-compose.yml` / `deploy/helm/eshu` (QA runs the same
pinned backend image as CI). Content Postgres is the QA read replica. All
measurements below ran against QA-scale data (api-node-boats: 15 candidate
rows / 14 distinct repos; 113 service-name content rows).

## No-Regression Evidence: widened bound costs are bounded and measured

- Candidate Cypher at B=500, single anchored start
  (`Repository{id=$repo_id}`), ORDER BY repo.name, repo.id, LIMIT 501:
  **130 ms wall** including the HTTP round trip (15 rows back). Anchored
  start unchanged; the only delta is the larger LIMIT literal.
- Indexed content search at B=500: **1.65 ms** (543 buffers hit).
- Substring fallback at B=500: **~5.2 s** (trigram index + sort). The
  fallback therefore keeps the legacy 100-row cap with its own +1 probe:
  on QA the indexed path is always taken (the reference index exists), so
  no production read pays the fallback cost; a deployment without the
  index keeps the old bound and still reports truncation honestly.
- End-to-end page baseline: `get_service_story` at
  `downstream_read_limit=100` completes in **2.73 s** wall on QA. Stop
  threshold carried forward: a new-tool page p95 must stay under 2x that
  story baseline; if a live re-probe misses it, lower the ceiling
  (`MaxConsumerScanBound`) rather than widening anything.
- Per-response allocation delta of the story-touching part of this change
  is **+1 alloc with inlining disabled** (721 to 722 on
  `TestBuildServiceStoryResponseNormalizesAPISurfaceOnce`); the remainder of
  the observed race-detector delta is inlining-threshold noise from the new
  builder code, not per-row work — the merged read is recomputed, never
  accumulated, and no loop gained an allocation. Boundary note: race and
  non-race totals are not comparable (different inlining); the +1 claim
  comes from identical `-gcflags=all=-l` runs, and the 712 budget in
  `service_story_api_surface_test.go` applies to the race-instrumented
  `-race` total only, keeping the ratchet against repeated normalization.

## Observability Evidence: no new telemetry surface

No new spans, metrics, or log lines. The paged path reuses the existing
`query.graph.read` span family and the per-search truncation
`consumer_repositories_truncated` limitation (plus the new
`consumer_truncation_sources` map, which names the fired bound without
adding cardinality: fixed key set, no per-repo labels). A slow page shows up
exactly where a slow story read does today; the only new operator-visible
signal is the `ceiling_reached` flag, which is a terminal-page marker, not a
new alert source.
