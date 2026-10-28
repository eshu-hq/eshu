# HTTP Runtime Declaration Routes

`POST /api/v0/runtime-declarations/query` lists the runtime versions
repositories declare, so a security user can ask which repositories declare a
Node major below 20. The MCP tool `list_runtime_version_declarations` calls the
same handler. The route reads the `RuntimeDeclaration` nodes the
`runtime_declaration_projection` reducer domain writes (see
[Runtime Declaration Graph Writes](../cypher-runtime-declarations.md)); it
needs the authoritative graph and answers `501` on the lightweight profile.

## What a declaration is

One declaration is one runtime version one file declares: a version-pin file
(`.nvmrc`, `.tool-versions`, `rust-toolchain`), the `engines`, `volta` and
`packageManager` sections of `package.json`, the `go` and `toolchain`
directives of `go.mod`, PHP constraints and platform pins in `composer.json`
and `composer.lock`, the `.php-version` pin, the `FROM` base-image tag of a
`Dockerfile`, or the
`with.*-version` inputs of a GitHub Actions workflow step. A pin-file line
that lists several versions is one row per token. There is no effective-version view in v1: a repository with a root
`.nvmrc` and a `services/web/.nvmrc` returns both rows, each with its
`scope_dir` (`.` at the repository root).

`source_kind` names the declaring source, one value per pin file (`nvmrc`,
`tool_versions`, `rust_toolchain`, `php_version` for `.php-version`), one per manifest section
(`package_json_engines`, `package_json_volta`, `package_json_package_manager`,
`go_mod_go`, `go_mod_toolchain`, `composer_json_require_php`,
`composer_json_platform_php`, `composer_lock_platform_php`,
`composer_lock_platform_overrides_php`), and one per new reader (`dockerfile_from`,
`workflow_setup_version`). The filter is an exact match on an open set.

## Request

The body is one JSON object. Unknown fields and wrong-typed values are a `400`
that names the field, so a misspelled filter cannot silently widen the answer.

| Field | Meaning |
| --- | --- |
| `runtime` | Exact runtime name (`node`, `python`, `go`, ...). Required with `major_below` or `version_below`. |
| `major_below` | Return declarations whose resolved major is below this integer. |
| `version_below` | Return declarations below `major`, `major.minor` or `major.minor.patch`. Not with `major_below`. |
| `source_kind` | Exact declaring source. |
| `repo_ids` | At most 100 repository ids. |
| `limit` | 1 to 200, default 50. |
| `cursor` | Opaque `next_cursor` from a prior page. |

A version comparison is only meaningful inside one runtime, so `runtime` is
required with either bound.

## Response

The body (the `data` of the `{data, truth, error}` envelope) carries
`declarations`, `count`, `limit`, `truncated`, `next_cursor` (only when
`truncated`), `filters` (the filters as applied; `version_below` echoes the
bound that was read), `excluded_from_comparison` and `partial_reasons`.

Rows are ordered by `(repo_id, relative_path, line_number, runtime,
order_index)`, the graph key, so a page is stable and a `next_cursor` this
route issued resumes strictly after the last returned row. A `cursor` the route
did not issue (tampered, truncated, or from another route) is a `400`;
restart paging without it. `truth` is `exact` on `authoritative_graph`.

Every row carries `repo_id`, `repo_name`, `relative_path`, `scope_dir`,
`line_number`, `order_index`, `runtime`, `source_kind`, `tool_name`, `raw_spec`,
`spec_kind`, `distribution`, and the version parts. `version_major`,
`version_minor` and `version_patch` are set only for `exact` and `partial`
specs. A `range` row leaves them `null` and carries `range_lower`,
`range_upper`, `range_lower_major`, `range_upper_major` and the inclusive
flags. An `alias` (`lts/*`, `node`, `system`) or `unresolved` (`${NODE_VERSION}`)
row carries neither. A `null` version part is not zero.

### What a comparison excludes

`major_below` and `version_below` return only the declarations they can place.
They never guess:

- `range`, `alias` and `unresolved` specs have no resolved major, so they are
  left out of the comparison.
- With `version_below`, an `exact` or `partial` spec that stops short of the
  bound is left out too: a bare `20` cannot be placed against `20.10`, and `20.10`
  cannot be placed against `20.10.3`. A declaration equal to the bound is not
  below it.

Each exclusion is counted, over the whole filter and not only the page, in
`excluded_from_comparison` (`total`, and `by_spec_kind` keyed `range`, `alias`,
`unresolved`, `partial` or `other`) and named in `partial_reasons`:
`runtime_declaration_range_specs_excluded`,
`runtime_declaration_alias_specs_excluded`,
`runtime_declaration_unresolved_specs_excluded`,
`runtime_declaration_partial_specs_excluded` and
`runtime_declaration_other_specs_excluded` (a spec kind this build does not
know). Read `partial_reasons` before concluding that a fleet is clean: an empty
comparison with a non-empty exclusion report means the declarations exist but
could not be compared.

`runtime_declaration_repository_names_unavailable` means the display-name read
failed; the rows are complete and only `repo_name` is missing.

## Access

The route supports scoped tokens. A scoped caller reads only declarations of
its granted repositories; the grant binds before the `LIMIT`, in the page read
and in the exclusion count. `repo_ids` narrows within the grant, never around
it, and a scoped caller with no grants gets the empty page without a graph read.

## Failures

`400` for an invalid filter, limit or cursor, an unknown field, or a version
comparison without `runtime`. `501` on the lightweight profile. `503` when the
graph backend is unavailable. A failed graph read answers a fixed body,
`runtime declaration graph read failed` or `runtime declaration comparison
exclusion read failed`, never the backend error; a fence or availability
verdict answers `503` or `504`.

## Repository context

`GET /api/v0/repositories/{repo_id}/context` gains `runtime_declarations`:
`count` (all declarations of the repository, including runtimes omitted from
`runtimes` when `truncated` is set), `runtimes` (the distinct declared
runtimes in name order, each with `declaration_count`, `resolved_count` and
`lowest_resolved_major`, `null` when nothing of that runtime resolves to a
major) and `truncated`. A failed read adds `runtime_declarations_read_degraded`
to `partial_reasons` and leaves an empty section; more than 50 runtimes adds
`runtime_declarations_truncated`.

## Performance and telemetry

The route's p95 budget is 1,000 ms on the local profiles and 1,500 ms in
production. The measured p95, the plans and the signals are in
`docs/internal/evidence/8817-runtime-declaration-query.md`. The handler opens
the `query.runtime_declarations` span and records
`eshu_dp_runtime_declaration_query_duration_seconds` (by `operation` and
`outcome`) and `eshu_dp_runtime_declaration_query_excluded_total` (by
`reason`); see [Telemetry](../telemetry/index.md).
