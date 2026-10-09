# Repository Retirement: Surfaces (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the design
and the two deliverables), [Runner](7766-repository-retirement-runner.md), and
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

This file holds the read surfaces, the OpenAPI changes, the operator surface,
the candidates read, and telemetry. Read surfaces and the admin create, get,
list, and dry-run routes ship in Deliverable 1 (PRs 5 and 8). The CLI, docs,
candidates, and console follow (PRs 9 to 11).

Binding inputs: the arbiter rulings on
[#7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598) and
[the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
and the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)), F7.
Source check: origin/main 3b03f018e, 2026-10-09.

## Read Surfaces And OpenAPI

The bounded `index_state` is `indexed | stub | retiring | retired`, derived in
this order:
1. an open tombstone in a non-`complete` state → `retiring`;
2. a `complete` tombstone → `retired`;
3. otherwise `evidence_source = 'projector/canonical'` → `indexed`;
4. else `stub`.

Deliverable 1 never produces a `complete` row, so it reports `retiring` and
`indexed` and `stub`. `retired` appears once the runner completes a retirement.

Retiring and retired rows also carry `retirement` = {`state` (policy enum),
`retired_at`, `retirement_id`}.

| Surface | Change |
| --- | --- |
| GET `/api/v0/repositories` (`query/repository/handler.go:137-219`) | Graph path: add `r.evidence_source` to the page query and look up tombstones for the page ids only (`WHERE repo_id = ANY($page_ids)`, in a new `retirement_state.go`; the dir carries `//nolint:dirgate`, `doc.go:16`). Content path (`content_reader_repository_catalog.go:16-32` reads `ingestion_scopes WHERE scope_kind='repository'`): `UNION ALL` the complete tombstones that have no scope row. Nothing is dropped from the list; every row is labelled. `total` stays the node count. |
| MCP `list_indexed_repositories` (`mcp/dispatch_repositories.go:74`, routes to the above) | Picks up the fields; the tool description in `tools_codebase.go:227` names `index_state` (mcp-schema-drift). |
| `/api/v0/index-status`, MCP `get_index_status` (`query/status.go:225-245`) | `repository_count` keeps its query. Adds `repository_count_by_state{indexed,stub}` from `MATCH (r:Repository) WITH r.evidence_source = 'projector/canonical' AS idx RETURN idx, count(*)`. Adds a `repository_retirements` block: counts by state, `oldest_pending_age_seconds`, `last_completed_at`. Counts only, no ids. |
| Freshness (`status/repository_freshness.go:183-205`) | New verdict `retired`, rule 0, from a tombstone lookup by `repo_id` that runs before `repositoryFreshnessResolveQuery`. The snapshot gains `Retirement *RepositoryFreshnessRetirement`. |
| Console (`apps/console/src/api/repoCatalog.ts`) | Badge from `index_state`; retired and retiring rows are muted, not hidden. |

OpenAPI changes:
- `paths/repository/freshness.go:40`: add `retired` to the verdict enum, plus a `retirement` object;
- the repositories list schema: add `index_state` and `retirement`;
- the index-status schema: add the new blocks;
- new admin fragments go in `paths/status/repository_retirement.go`
  (`paths/status/admin.go` is 461 lines, so it cannot take them).

## Operator Surface

Admin routes live in a new leaf, `query/admin/retirement/`, because the admin
`AGENTS.md` sends new families to their own leaf. The root router wires them.

- `POST /api/v0/admin/repository-retirements` with body
  `{"repositories":[sel,...1..25], "reason":str, "idempotency_key":str, "dry_run":bool}`.
  - **Selectors:** resolved with `MatchRepositories` and `gitDefaultScopeMatch`
    (`reindex_repository.go:55-131`, `content_reader_repository_catalog.go:145-171`
    matches repo id, `scope_id`, name, slug, path, local path). The request is
    all or nothing, and a 400 names every bad selector. A scope id
    disambiguates the duplicate case.
  - **Required fields:** `reason` and `idempotency_key` are mandatory.
    `ClaimReplayIdempotency` uses fingerprint `sha256(sorted scope_ids)`, and
    `CompleteReplayIdempotency` follows the semantics in
    `query/admin/generations.go:84-128`.
  - **Auth:** admin (all-scopes) only. Scoped tokens get 403 (`generations.go:97-101`).
  - **Responses:**
    - 202 `{status:"accepted", retirements:[{retirement_id, repository_id, scope_id, state:"pending", requested_at}], idempotency_key, duplicate}`;
    - 200 dry-run preview;
    - 409 for `blocked` (`inflight_reducers`, `lock_timeout`, `claim_fence_busy`), `request_too_large` (with per-repo counts), key reuse, or in progress.
- `GET /api/v0/admin/repository-retirements/{retirement_id}` and
  `GET /api/v0/admin/repository-retirements?state=&limit=&cursor=` return the
  readback:
  - `retirement_id`, `repository_id`, `scope_id`;
  - `state` (`pending|running|blocked|repairing_graph|complete|failed`), `phase`
    (`fenced|quiesce|barrier|graph_retract|purge|census|finalize|done`),
    `blocked_reason`, `failure_class`, `attempt_count`;
  - `requested_at`, `updated_at`, `retired_at`, `readmitted_at`;
  - `rows_deleted_by_table`;
  - `graph{nodes_deleted, relationships_deleted, repository_node: demoted|absent|readmitted}`.
  - With Deliverable 1 alone the readback shows `state=pending`, `phase=fenced`,
    and empty counts.
- **Dry run:** writes no ledger row and no marker, but emits an audit event.
  Per repository it returns:
  - `ref_scope_count` and `generations_by_status`;
  - per-table row counts from cheap index counts by scope/repo key, under a 10s
    `statement_timeout`, with `counts_partial` on timeout;
  - `live_leases{projector,reducer}`;
  - a graph `{file_count, incoming_foreign_edges:[{type, evidence_source, count}]}`
    from anchored reads on `Repository {id}`;
  - `grants{tenant_scope_grants, tenant_repository_grants, identity_role_scope_targets, identity_role_repository_targets}`
    as active counts (`tombstoned_at IS NULL AND (expires_at IS NULL OR
    expires_at > now())`; the two identity target tables also require
    `status = 'active'`), and `will_drop_grants: true` when any is non-zero;
  - `selection.state`, plus `will_readmit:true` when the repo is still `selected`.
    Retirement does not change selection, so the next sync re-admits it.
- **CLI:** `eshu admin repository retire <sel>... --reason --idempotency-key [--dry-run]`,
  `eshu admin repository retirement-status <id>|--state`, and
  `eshu admin repository retirement-candidates`.
  - Request builders go in `internal/cli/admin/retirement.go` and cobra in
    `cmd/eshu/admin.go`, because `cmd/eshu` already has 40 non-test files.
  - With no key given, the CLI generates one, as `replay` does (`admin.go:88`).
- **Tombstone row:** the marker row itself, holding the safe hashes, bounded
  reason code, actor class, counts by table (the four grant counts are inside
  `rows_deleted`), and lifecycle timestamps. Raw ids appear only in admin
  readbacks, which is where `reindex` already returns them.

### Grants are dropped on purpose

Phase 3's scope delete cascades the four grant tables (see the
[runner](7766-repository-retirement-runner.md#phase-3-complete-one-transaction)).
Access fails closed. Re-admission does not restore them. Three places say so:
- the dry run reports `grants` and `will_drop_grants`;
- the tombstone carries the four counts, and the WARN logs below repeat them;
- `docs/public/reference/hosted-retention-deletion-policy.md`, the operate
  runbook, and the HTTP API reference state: "Retirement drops tenant and role
  grants for the repository. Re-admission does not restore them; grant again."

## Candidates

`GET /api/v0/admin/repository-retirements/candidates?limit=(1..200)&cursor=<scope_id>`
is read-only. It never feeds the primitive.

The SQL is a prefilter, and `selection.Summarize`
(`scope/selection/summary.go:74-144`) re-runs on the page's rows (PK reads) as
the authority:

```sql
WITH live AS (SELECT * FROM repository_selection_observations
  WHERE evaluated_at + make_interval(secs => liveness_window_seconds) >= $1),
agg AS (SELECT scope_id, bool_or(state='selected') AS any_sel,
  bool_and(state_cycle_count >= 2 AND evaluated_at - state_since >= interval '5 minutes') AS confirmed,
  max(state_since) AS newest_exclusion FROM live GROUP BY scope_id)
SELECT a.scope_id, a.newest_exclusion FROM agg a
JOIN ingestion_scopes s ON s.scope_id = a.scope_id AND s.scope_kind = 'repository'
WHERE NOT a.any_sel AND a.confirmed AND a.scope_id > $2
  AND NOT EXISTS (SELECT 1 FROM scope_generations g WHERE g.scope_id = a.scope_id
                  AND g.observed_at > a.newest_exclusion)
  AND NOT EXISTS (SELECT 1 FROM repository_retirements r
                  WHERE r.scope_id = a.scope_id AND r.readmitted_at IS NULL)
ORDER BY a.scope_id LIMIT $3
```

## Telemetry

Instruments go in `telemetry/instruments_operator_repository_retirement.go`.
The existing `instruments_repository_retirement.go` is the 7324 counter.

| Signal | Definition |
| --- | --- |
| `eshu_dp_repository_retirements_total{outcome}` | `accepted`, `refused`, `completed`, `failed`, `readmitted_after_retirement` |
| `eshu_dp_repository_retirement_ingest_refusals_total{writer}` | `collector_commit`, `webhook_handoff`, `deferred_backfill`, `recover_generations`, `reindex` |
| `eshu_dp_repository_retirement_step_duration_seconds{phase}` | `fence`, `quiesce`, `barrier`, `graph_retract`, `purge_batch`, `census`, `finalize` |
| Spans | `admin.repository_retire` and `reducer.repository_retirement_step` (attr `phase`), registered through a contract subpackage step (telemetry `AGENTS.md`) |
| Logs | WARN `repository retirement completed`: `scope_id`, `retirement_id`, `generation_count`, ≤10 `generation_ids` + `generation_ids_hash`, `rows_deleted`, `nodes_deleted`, `relationships_deleted`, and the four grant counts. WARN `repository ingest refused: retiring` with `writer`. WARN `repository readmitted after retirement`, with the four grant counts read from the tombstone. |
| Audit | `governanceaudit.EventTypeRepositoryRetirement = "repository_retirement"` beside `audit.go:56`, added to `validEventType` (`:327-342`), `ScopeClassRepository`. Reason codes `repository_retire_{accepted,preview,idempotent_replay,refused_missing_reason,refused_missing_idempotency_key,refused_unauthorized,refused_selector,refused_key_reused,refused_request_too_large,blocked}`. |
| Status | Readback fields (see Operator Surface) and the index-status block (see Read Surfaces And OpenAPI). The `blocked_reason` values `reducer_lease_live` and `claim_fence_busy` make 2q visible. |

Every row lands in `docs/public/observability/telemetry-coverage.md`, the
`telemetry-coverage` gate, `docs/public/reference/telemetry/metrics-reducer-storage.md`,
`logs.md`, and `traces.md`. The metrics doc entry must distinguish the new
counter from `eshu_dp_canonical_repository_retirements_total`.
