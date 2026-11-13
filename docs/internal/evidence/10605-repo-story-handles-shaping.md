# 10605: repository story `evidence_detail` handles projection

`GET /api/v0/repositories/{repo_id}/story` accepts `evidence_detail=full|handles`
(default `full`). Unknown values return 400 `invalid_argument` before any read
runs. `handles` projects bulky row families to identity rows and names every
reduction in `truth.omissions`; shaping runs after enrichment, so counts and
answer metadata are computed from the full rows. `full` is the identity map.

No-Regression Evidence: `TestRepoStoryEvidenceDetailAtCapHandlesFitsBudget`
(at-cap fixture: 500 workload/platform strings, 5000-file documentation
signals, 200 wide relationships, 100 deployment plus 100 content artifacts,
every capped family at its documented cap) marshals 4,049,011 bytes `full` vs
188,422 bytes `handles` against the 262,144-byte (256 KiB) dispatch budget —
73,722 bytes (~28%) of headroom — green on 5/5 runs (ledger:10605-story-handles-atcap). `go test
./internal/query/querycontract ./internal/query/repository -count=1` green;
MCP `TestDispatchToolRepoStory*` plus
`TestOpenAPIRepoStoryEvidenceDetailNamesFamilies` green. Backend is a pure
in-process JSON projection on the already-built response map (no new
Postgres/Neo4j read); measured with go1.26.6 linux/arm64.

Observability Evidence: the new `evidence_detail` shaping stage reuses the
existing `repository_query.stage_started` / `repository_query.stage_completed`
timer (`operation=repository_story`) with `repo_id`, `duration_seconds`,
`evidence_detail`, and `omission_count` attrs. No new metric, span family,
queue, worker, collector, or runtime knob.
