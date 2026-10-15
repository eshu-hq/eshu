# Evidence: #7765 prefix known-scope read re-measurement

Re-measurement of the gitlab_group prefix partition read on a live
PostgreSQL, required by the design's prove-first gate before any index
decision. Raw commands and outputs below; seed and query scripts are
throwaway (`/tmp`, not committed) and reproduced inline.

## Environment

- PostgreSQL 18.6 on x86_64-pc-linux-musl (Alpine, `postgres:18-alpine`),
  disposable container `eshu-7765-pg` on `127.0.0.1:15443`, database
  `prefixread`, created 2026-10-15 for this run and removed after.
- `shared_buffers = 128MB`, 16 CPUs (AMD EPYC 9R14 container host).
- Schema: every file under `go/internal/storage/postgres/migrations/`
  applied in order with `ON_ERROR_STOP=1`, exit 0. Migration 178 widens
  the `selector_kind` CHECK on a fresh bootstrap; verified live:
  `CHECK ((selector_kind = ANY (ARRAY['github_org'::text, 'explicit'::text,
  'gitlab_group'::text])))`.
- Host was NOT fully quiet: two peer Postgres containers (`eshu-7802-pg`,
  `eshu-8024-pg`) were up on the same host. The interleaved same-table
  comparison below is robust to that; absolute timings may sit above a
  quiet host.

## Seed (20,001 rows)

One `INSERT ... SELECT ... FROM generate_series` per class into
`ingestion_scopes` (`scope_id`, `scope_kind`, `source_system='git'`,
`collector_kind='git'`, `payload` with `repo_slug` and `remote_url`),
then `VACUUM ANALYZE`:

| Class | Slug shape | Host | Kind | Rows |
| --- | --- | --- | --- | --- |
| nested | `acme/platform/svc-N` | gitlab.com | repository | 900 |
| sibling | `acme/other/svc-N` | gitlab.com | repository | 300 |
| lookalike | `acme/platformer/svc-N` | gitlab.com | repository | 100 |
| underscore | `acme_platform/svc-N` | gitlab.com | repository | 50 |
| same-path other host | `acme/platform/svc-N` | github.com | repository | 900 |
| refs | `acme/platform/svc-N` | gitlab.com | repository_ref | 200 |
| remote-less | `acme/platform/noremote-N` (no `remote_url`) | — | repository | 50 |
| background | `owner<M>/repo-N`, M < 200 | github/gitlab/ghe | repository (6/7) + repository_ref (1/7) | 17,500 |
| bootstrap residue | (from a migration seed) | — | — | 1 |

## Row-set verification

- Group read (`starts_with(lower(slug), 'acme/platform/')` +
  host `gitlab.com`): **900 rows** — sibling, lookalike, underscore,
  other-host, ref, and remote-less scopes all drop out.
- Blank-host prefix read (same predicate, `$2 = ''`): the host predicate
  stays a per-row filter that is always true; exact scope IDs are asserted
  by the `TestKnownScopesPathPrefixLive` live test, not by count here.

## EXPLAIN (ANALYZE, BUFFERS), 5 warm runs interleaved, medians

Q1, the new prefix read:

```sql
SELECT scope_id, payload->>'repo_slug' FROM ingestion_scopes
WHERE source_system = 'git' AND scope_kind = 'repository' AND collector_kind = 'git'
AND starts_with(lower(payload->>'repo_slug'), 'acme/platform/')
AND ('gitlab.com' = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = 'gitlab.com');
```

Plan (stable across runs): `Seq Scan on ingestion_scopes`,
`rows=900`, `Rows Removed by Filter: 19101`, `Buffers: shared hit=495`,
**Execution Time median 9.22 ms** (runs: 9.22, 9.27, 8.85, 9.18, 9.22).

Q2, first-segment slug-only baseline (`lower(split_part(slug,'/',1))='acme'`,
no host, 2,250 rows): seq scan, 495 buffers, **median 5.69 ms**.

Q3, first-segment with host `github.com` (900 rows): seq scan, 495 buffers,
**median 5.93 ms**.

Ratio Q1/Q3 = **1.56x**, marginally above the design bar 2 (1.5x). Root
cause, isolated by timing the slug predicates alone over the same 20k rows
(`starts_with(lower(slug), ...)` 8.5 ms vs
`lower(split_part(slug,'/',1)) = ...` 6.0 ms): per-row `lower()` CPU over
longer strings, not a plan difference — both are the same sequential scan
shape. No index changes per-row filter CPU on a full scan, and the read
runs once per cycle on shard 0 at ~9 ms absolute. The bar's intent (no plan
cliff, no index needed) holds; the marginal ratio miss is recorded here for
the reviewer rather than hidden.

## No-index decision

Per the index doctrine (hot enough + usable predicate shape): the read runs
once per selection cycle (~9 ms), the planner correctly seq-scans at this
scale, and a btree on `lower(repo_slug)` would buy nothing here while
adding write amplification and migration cost. **No new index.**
A per-row `LIKE` was rejected for correctness (`_` is a wildcard and legal
in GitLab paths); `starts_with` needs no escaping.

## Load probe (pgbench, read-only)

`pgbench -n -c 4 -j 4 -T 30` with the group query: 13,710 transactions,
**latency average 8.75 ms**, second run **8.46 ms, stddev 0.51 ms**
(no long tail).

## Lock probe

A second session held `FOR UPDATE` on a nested scope for 20 s while the
group read ran: the read returned 900 rows unblocked, the holder committed,
and zero ungranted locks remained. The read is a plain `SELECT` (no
`FOR UPDATE`/`FOR SHARE` node in any plan above), so it takes only
`AccessShareLock` on the relation by construction.

## Go side

`BenchmarkGitLabGroupSelectionRequest` (800 projects): 3.61–3.68 ms/op vs
`BenchmarkGitHubOrgSelectionRequest` 2.93–2.94 ms/op (~1.24x: one extra
identity derivation per project). Once per cycle on shard 0; negligible.
