# Index Status

`GET /api/v0/status/index` and its legacy alias `GET /api/v0/index-status`
share one handler. See [Status And Admin](status-admin.md#index-status) for the
rest of the status routes. The full report also carries an
[`active_work_source`](active-work-source.md) object that says whether the
queue came from the stored summary or the live statement, and a
[`terraform_state_source`](active-work-source.md#terraform-state-source) object
for the `terraform_state` section. A scoped caller gets neither.

A shared-key caller receives the full deployment-wide report: `status`,
`reasons`, `repository_count`, `repository_count_by_state` (the graph count
split into `indexed` versus `stub` by evidence source),
`repository_retirements` (the tombstone aggregate: per-state open-row
counts, the oldest pending age, and the last completion; counts only, no
ids), `queue`, `queue_blockages`, `coordinator`, `scope_activity`,
`aws_materialization`, `semantic_extraction`, and `terraform_state`. A
scoped-token caller (#5167) receives only:

```json
{
  "version": "...",
  "scoped": true,
  "repository_count": 1,
  "completeness_state": "scoped_repository_count_only",
  "withheld_sections": ["status", "reasons", "queue", "queue_blockages",
    "coordinator", "scope_activity", "aws_materialization",
    "semantic_extraction", "terraform_state", "repository_count_by_state",
    "repository_retirements"]
}
```

The retirement blocks are deployment-wide aggregates that cannot be
attributed to a grant, so scoped callers are told they were withheld. A
scoped caller's own retiring repositories still surface through the
grant-bound repositories list (`index_state`) and freshness verdicts.

`repository_count` is counted over the caller's granted repositories and
ingestion scopes inside the graph query, and a caller with no grant gets `0`
without a graph query.

An unscoped (shared-key) caller reads the status snapshot first and then the
same graph repository count over the whole deployment. A failed count
surfaces the same way as for a scoped caller: a graph outage answers the
retryable `503` with `Retry-After`, a graph read deadline answers `504`, and
any other failed count answers `500` `index status repository count failed`,
never a silent `repository_count: 0`. A nil graph query keeps the historical
`200` with `repository_count: 0`: with no query wired there is no read to
fail. No production wiring does this: both servers always wire a graph
reader, so a deployment without a graph driver answers the fixed `500`
through its driverless reader. The withheld sections are process-global: they cannot be
attributed to a grant, and a `queue_blockages` row reports `conflict_key` as
`COALESCE(conflict_key, scope_id)`, a raw scope id. They are withheld whole and
the status snapshot is not read for a scoped caller, the same posture as the
scoped `GET /api/v0/status/operations` board. The graph count is the scoped
caller's only read. A graph outage answers the retryable `503` with
`Retry-After`, a graph read deadline answers `504`, any other failed count
answers `500` with a fixed message, and an unconfigured graph answers `503`
with no `Retry-After`, never a silent `0`. See
[Status And Evidence Route Failures](status-evidence-failures.md). `eshu mcp setup
--verify` smokes this route and passes on the scoped shape.
