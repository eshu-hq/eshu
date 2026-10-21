# Index-status by-state rewrite: Go-side source comparison (#8689)

The `#7766` repository_count_by_state graph read grouped repositories with
an inline Cypher boolean,
`WITH r.evidence_source = 'projector/canonical' AS idx`. The differential
quorum oracle reproduced a `results` divergence on main: NornicDB returns
one row `{count: 35}` with `idx` null/missing while Neo4j returns
`{count: 35, idx: true}` (Golden Corpus Gate run 39503054144, stable per
backend across both quorum pairings). Digest-preimage recovery over the
captured digests confirmed the row shapes; the per-repo listing and the
total count agree on all 35 repositories, so the grouping input is
identical and only the inline comparison evaluates differently.

The statement now groups by the projected property
(`RETURN r.evidence_source AS src, count(*) AS count`) and the handler
maps `src == 'projector/canonical'` to indexed in Go. Production behavior
on the deployment backend is unchanged: Neo4j rows that read
`{idx: true}` now read `{src: 'projector/canonical'}` and map to the same
indexed count, while stub and absent-source rows still map to stub. On
NornicDB the same rows now agree with Neo4j instead of collapsing to a
single null-idx group, so the served `repository_count_by_state` split
stops reporting every repository as stub there.

No-Regression Evidence: one statement replaces one statement on the same
label scan (`MATCH (r:Repository)` with implicit grouping in both
versions); the rewrite removes the per-row comparison operator rather
than adding traversal, and the Go-side comparison runs over at most one
row per distinct source. Live probe on the pinned backends
(`neo4j:2026-community`, `nornicdb-amd64-cpu:fix-500-e022384c`, 2
canonical + 1 stub + 1 sourceless repositories seeded identically): the
old statement diverges (Neo4j three grouped rows, NornicDB one collapsed
row), the new statement returns byte-identical grouped rows on both
backends with agreeing digests. Per cypher-query-rigor this correctness
fix trades a full bench for a no-measurable-regression check on the same
input shape: the read runs once per index-status request, never on a hot
write or poll path, and removes work rather than adding it.

No-Observability-Change: the served wire shape is unchanged
(`repository_count_by_state: {indexed, stub}` counts); no telemetry,
spans, metrics, logs, or status surfaces are added or changed. The
differential oracle re-proof is the gate itself: the rewritten statement
fingerprints differently, so any regression reappears as an unexcused
quorum divergence instead of passing silently.
