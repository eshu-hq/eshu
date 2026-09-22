# CloudResource owner-ledger backfill: fence the first page (#6842)

Change: `go/internal/query/cloud_resource_owner_backfill.go` served its first
page as `MATCH (n:CloudResource) RETURN <projection> ORDER BY n.uid LIMIT
$limit` and every later page with `WHERE n.uid > $after_uid`. One query now
serves every page; the first page passes `$after_uid = ''`. The projection,
page size, ordering, ledger rows, order keys, and completion marker are
unchanged. The empty-string fence excludes only a node whose uid is not a
non-empty string (null, empty, or a non-string value, which compares as
null); the ledger cannot key such a node and it previously aborted startup as
an unattributable row. Every production CloudResource node is MERGEd on a
non-empty composed uid (`go/internal/storage/cypher/*_node_writer.go`).

Proof provenance: the RED run used an `origin/main` worktree at `965631995`
with only the new test file copied in; the GREEN run used the fix worktree on
the same base with the Go sources exactly as committed in the first commit of
this branch (the log's test line numbers match the committed test file). The
branch was then rebased onto newer `origin/main`; every commit in that base
delta touches files off the backfill path and none of this change's files, and
the post-review edits changed comments and test wording only; the unit shape
tests were rerun after them.

Backend: `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530f…`
(the compose default), fresh container per shape, Eshu's full NornicDB schema
applied once before seeding, 150,000 `CloudResource` nodes with `uid`,
`source_fact_id`, `resource_type`, `name`, `collector_kind`, unique parameter
per probe run so the result cache cannot answer.

Performance Evidence: first page of 500 rows over 150,000 uid-indexed CloudResource nodes, production `Neo4jReader` policy (10s deadline), same seeded store both sides: before (origin/main, unfenced) `graph query exceeded its deadline` at 10.00s, raw shape 29.5s to 45.9s with a 150s cap; after (fenced, `$after_uid = ''`) 1.4s to 1.7s cold through the reader and 0.08s to 0.35s per probe with a fresh parameter; continuation page (`$after_uid` = last uid) 0.13s to 0.41s. Both directions are `TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale` runs, RED from an origin/main worktree and GREEN from the fix worktree against the same container (run log in the PR).

No-Observability-Change: the backfill keeps its `cloud resource owner ledger backfill complete` log (`pages_seeded`, `rows_seeded`, `duration_seconds`) and the reader keeps emitting `query.graph_read.warning` with `failure_class=deadline|slow` per page; no span, metric, log field, or status surface is added, removed, or renamed.

Equivalence: the fenced first page returned the 500 lowest uids in strict
order (asserted by the live test against the seeded population), which is the
row set the unfenced page defines; the continuation pages were already fenced.

Two findings recorded alongside, both in
`docs/public/reference/nornicdb-order-limit-pitfalls.md` ("ORDER BY Plus LIMIT
Without A Range Predicate On The Sort Key"): a uniqueness constraint creates
no index on NornicDB (only Eshu's `nornicdb_<label>_uid_lookup` index makes
uid reads seek), and `CREATE INDEX … IF NOT EXISTS` on an existing index
re-runs the backfill without deduplication, so a second schema application on
a populated store makes every index-driven read return each row twice. The
live test's reuse path therefore never re-applies the schema.
