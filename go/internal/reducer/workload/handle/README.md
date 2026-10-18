# handle

Provider-asserted workload handle ladder for reducer consumers (#5385).

## Why it exists

Collectors assert workload identity as either the full scoped node id
(`workload:<repo_id>:<name>`) or the public handle (`workload:<name>`).
After the repository-scoped re-key the handle resolves server-side: exact
id first, then the bare name within the caller's candidate set, failing
closed when the name spans repositories. Four reducer consumers share this
package instead of reimplementing the ladder: cloud `USES` rows, service
catalog decisions, suppression scopes, and documentation targets.

## What it owns

| piece | file | what it does |
|---|---|---|
| `Candidate`, `Resolution`, `Resolve`, `ResolveBatch`, `HandleNames` | `resolution.go` | pure ladder over injected candidates, no backend |
| `Item`, `Resolver.ResolveAll` | `resolution.go` | one batched name lookup per intent, scope/explicit-repo restriction, counting, ambiguous log |
| `Lookup`, `ScopeRepos` | `resolution.go` | graph and scope ports (structurally satisfied by the materializer sibling read and `crossrepo.ScopeRepositoryReader`) |
| `Record`, `LogAmbiguous`, `SkipReasonHandleAmbiguous`, `SurfaceReducer` | `resolution.go` | `eshu_dp_workload_handle_resolution_total{surface,outcome}` and the alertable skip log |

## Contract

- A full id resolves by exact match only; it never falls back to a name.
- A handle resolves to exactly one in-set node (`unique_by_name`) or to
  nothing: zero candidates is `miss`, more than one is `ambiguous` with the
  sorted candidate `repo_id`s.
- The allowed set is the item's explicit repository when set, else the
  scope's repositories when the reader is wired, else the whole candidate
  set. An empty selector never resolves.
- `ResolveAll` with a nil `Lookup` returns nil so callers keep legacy rows;
  a lookup error fails closed to the caller.
- Labels are closed and bounded: `surface="reducer"`,
  `outcome=exact|unique_by_name|ambiguous|miss`. No repository, handle, or
  scope id ever becomes a label; those ride the structured log.

## Telemetry

`eshu_dp_workload_handle_resolution_total` (`surface`, `outcome`); the
ambiguous point carries the alert rule, and `LogAmbiguous` writes the
matching WARN line with `scope_id`, `handle`, `candidate_repo_ids`, and
`skip_reason=workload_handle_ambiguous`.

## Related

- `docs/internal/design/5385-workload-identity-compatibility.md` (sections
  1.4 and 2.3: the ladder and the skip reason)
- `docs/internal/design/5385-workload-identity-migration.md` (section 8:
  the telemetry table; T3: the per-consumer matching contract)
