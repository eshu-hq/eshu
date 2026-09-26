# Index Status

`GET /api/v0/status/index` and its legacy alias `GET /api/v0/index-status`
share one handler. See [Status And Admin](status-admin.md#index-status) for the
rest of the status routes.

A shared-key caller receives the full deployment-wide report: `status`,
`reasons`, `repository_count`, `queue`, `queue_blockages`, `coordinator`,
`scope_activity`, `aws_materialization`, `semantic_extraction`, and
`terraform_state`. A scoped-token caller (#5167) receives only:

```json
{
  "version": "...",
  "scoped": true,
  "repository_count": 1,
  "completeness_state": "scoped_repository_count_only",
  "withheld_sections": ["status", "reasons", "queue", "queue_blockages",
    "coordinator", "scope_activity", "aws_materialization",
    "semantic_extraction", "terraform_state"]
}
```

`repository_count` is counted over the caller's granted repositories and
ingestion scopes inside the graph query, and a caller with no grant gets `0`
without a graph query. The withheld sections are process-global: they cannot be
attributed to a grant, and a `queue_blockages` row reports `conflict_key` as
`COALESCE(conflict_key, scope_id)`, a raw scope id. They are withheld whole and
the status snapshot is not read for a scoped caller, the same posture as the
scoped `GET /api/v0/status/operations` board. A failed graph count is a `500`
and an unconfigured graph a `503`, never a silent `0`. `eshu mcp setup
--verify` smokes this route and passes on the scoped shape.
