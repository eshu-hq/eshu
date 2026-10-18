# Repository selection page annotation (#8454 PR-2)

Companion to `README.md` for the `go/internal/storage/postgres` package:
the batched selection-annotation read path.

`repository_selection_batch.go` adds `PageSelectionStore`, the batched read
behind the per-entry `selection` aggregate on `GET /api/v0/repositories`
(both the content and graph paths, plus the MCP list that forwards to the
route): one slug-resolution statement for the page's repo slugs, one
live-rows statement (`scope_id = ANY`, the primary key's leading column),
the newest-generation-per-scope statement only when a scope's aggregate
needs rule (d) (lazy, memoized, skipped entirely for an all-selected page),
and the successor check only when the page holds a not_listed entry with a
successor slug (slug resolve plus one more bounded live-rows read for
off-page successors). Any sub-query error fails the whole read: entries
never serve with evidence silently missing. The single-scope freshness read
(`readSelection`) annotates through the same `selection.Summarize` plus
`WithUpstream`, so a list entry and the freshness verdict for one scope can
never disagree (`TestSummarizePageEqualsSingleScopeSummarize` pins it); the
status snapshot carries the same evidence as counts by state and verdict
(`readRepositorySelectionCounts`, one GROUP BY over live rows).

Performance Evidence (prove-theory-first shim, throwaway Postgres 18.6 with
`statement_timeout`, migrations 166+199 DDL): at the 802-scope QA shape the
live-rows batch runs 0.3 ms, the generations batch 3.3 ms, the slug
resolution 0.15 ms, the successor resolution 0.14 ms, and the status counts
GROUP BY 0.2 ms. At a 100k-scope / 500k-generation stress shape the
live-rows batch is a bitmap PK scan at 2.3 ms for a 500-id page and the
generations batch a bitmap `scope_generation_idx` scan at 2.4 ms; the two
slug scans stay sequential (no slug index) at 11 ms there, proportional to
the scope count — the smallest table on the path, the same shape the
known-scope read already accepts, with a repo_slug expression index held
back for evidence that scope counts outgrow it. No new index in this change.

Observability Evidence: the annotation reads ride the existing repository
list stages (`repository_query.stage_started` / `stage_completed`) and the
freshness duration/error instruments, which cover the added sub-queries;
the status counts read is labeled `repository_selection` on
`eshu_dp_status_snapshot_read_duration_seconds` like every other status
read. No new metric: the annotation adds bounded statements to existing
reads, not a new pipeline.
