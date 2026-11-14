# #10659: Name-first fan-out bound note

Issue eshu-hq/eshu#10659 (arbiter-settled in eshu-hq/eshu#10652: extend
`resolve_entity` with omit-type name-first, no new tool, type stays
optional, hostnames out of scope, PROFILE per branch before any
`Repository.name` index decision).

## What changed

`POST /api/v0/entities/resolve` with omitted `type` and no `repo_id` no
longer answers 400. It fans out to four exact-name branches (repository,
workload, package, content entity) and returns one ranked list; every row
carries `kind`, its canonical id, and `next_selector`. Names under 3
characters are refused before any branch runs. No index was added.

## No-Regression Evidence: bounded fan-out, unit and contract proof

- Scope: the omit-type path of `ResolveEntity`
  (`go/internal/query/entity/resolve_namefirst.go`). Limit: the shared
  `NormalizeResolveEntityLimit` clamp (default 10, max 100); each branch
  requests limit+1 rows, the merged page is trimmed to limit with the
  `truncated` flag. Timeout rides the request context, the same bound
  every other resolve call carries; no new knob.
- Backends: repository and package branches run exact-equality Cypher
  (`r.name = $name OR r.repo_slug = $name`; `p.normalized_name = $name`)
  with `ORDER BY` and `LIMIT $limit`; the workload branch reuses the
  exact `w.name` resolver; the content branch runs one exact all-types
  probe. No `CONTAINS`, no substring scan, no unlabeled `MATCH`.
- Grant scoping: the repository grant predicate is pushed into the
  `WHERE` before `LIMIT`; workload, content, and package branches apply
  their existing scoped shapes (`publicOnly` for scoped package
  callers). Grantless callers get the existing empty 200 without
  touching any backend.
- Proof (no live backend in this lane): `resolve_namefirst_test.go`
  (ranked repo-plus-workload list, cross-kind collision lists every
  kind, legacy twin `legacy_id`, short-name 400, grantless empty) and
  the updated `TestGlobalEntityResolveStaysBounded` guard, which fails
  any fan-out query containing `CONTAINS` or an unlabeled match. Live
  `EXPLAIN`/`PROFILE` per branch stays deferred to the promotion lane
  with a live backend, as the arbiter ruling requires before any index
  decision; this change adds no index, so that gate is not triggered.

## Observability Evidence: per-branch stage timers and truth envelope

- Each branch runs under a `service.StartServiceQueryStage` timer
  (`name_first_repository_lookup`, `name_first_workload_lookup`,
  `name_first_package_lookup`, `name_first_entity_lookup`) logging
  `row_count` on completion, so a slow branch is attributable even when
  the client times out.
- Answers carry the hybrid truth envelope
  (`code_search.exact_symbol`, basis hybrid); failures map through the
  route's existing sentinels and fixed messages (`writeReadFailure`,
  `writeNameFirstWorkloadFailure`, `writeNameFirstContentFailure`), so
  backend detail never reaches the body.
