# CLI: Indexing And Management

This page is the reference for repository indexing commands. For a task-first
walkthrough, use [Index Repositories](../use/index-repositories.md).

## Command Contracts

| Command | Use when | Runtime contract |
| --- | --- | --- |
| `eshu scan [path]` | You need local indexing plus readiness proof. | Resolves the source root, preflights API status and `/api/v0/repositories?limit=1`, runs `eshu-bootstrap-index`, then polls `/api/v0/status/pipeline` until the graph is queryable. |
| `eshu index [path]` | You only need to launch local bootstrap indexing. | Runs `eshu-bootstrap-index --path <resolved-root>` and returns after bootstrap exits; it does not wait for queue-zero or query readiness. |
| `eshu list` | You need the API's current repository list. | Calls the configured HTTP API through CLI config, environment, or the localhost default. |
| `eshu watch [path]` | You need a foreground local refresh loop. | Starts the local-host watch supervisor; missing local index state is bootstrapped before file events trigger repo-level reindexing. |

## `eshu scan`

`eshu scan` is stricter than `eshu index` because collection completion alone is
not enough to prove a source is queryable.

Important flags:

| Flag | Contract |
| --- | --- |
| `--force` | Re-index from scratch. |
| `--wait=false` | Run bootstrap without readiness polling. |
| `--timeout <duration>` | Cap readiness polling; default is `30m`. |
| `--poll-interval <duration>` | Control status polling; default is `3s`. |
| `--allow-partial` | Return success for partial or degraded readiness with warnings. |
| `--json` | Emit the canonical `{data, truth, error}` envelope. |
| `--discovery-report <file>` | Forward a discovery advisory path to `bootstrap-index`. |
| `--workspace-root <path>` | Override source-root detection. |

`eshu scan --json` reports bootstrap-complete and queue-zero timings. It leaves
collector-complete and source-local projection-complete timings as explicit
`null` values with warnings because the bootstrap child logs those milestones
but does not expose parent-process structured timestamps today.

## `eshu index`

`eshu index` launches the same `bootstrap-index` runtime for a local directory
or workspace target, but it does not poll the API for readiness.

Important flags:

| Flag | Contract |
| --- | --- |
| `--force` | Re-index from scratch even if the source looks unchanged. |
| `--discovery-report <file>` | Write a JSON discovery advisory report. |

The discovery report lists discovered, parsed, skipped, and materialized
file/entity counts plus noisy directories/files and skip breakdowns. It carries
`schema_version=discovery_advisory.v1`; treat it as an operator artifact, not a
metric label or stable API payload. For the evidence -> config -> rerun loop,
use [Discovery Advisory Playbook](local-testing/discovery-advisory.md).

Eshu skips hidden and cache directories such as `.git`, `.terraform`,
`.terragrunt-cache`, `.pulumi`, `.crossplane`, `.serverless`, `.aws-sam`,
`cdk.out`, `vendor/`, `node_modules/`, `site-packages/`, and `deps/` by
default. Use [.eshuignore](eshuignore.md) for project-specific exclusions.

Local launcher state lives under `ESHU_HOME/state/go-bootstrap-index/`.
Workspace-local service state for `eshu watch` and stdio `eshu mcp start`
lives under `${ESHU_HOME}/local/workspaces/<workspace_id>/`; the layout is
defined in [Local Data Root Spec](local-data-root-spec.md).

## `eshu list`

`eshu list` is an API read. It does not inspect local files directly and uses
the same service URL resolution order as other API-backed CLI reads:

1. command flags where registered
2. persisted `eshu config` values
3. process environment
4. `http://localhost:8080`

## `eshu watch`

`eshu watch [path]` runs in the foreground. It starts or attaches to the local
service for the resolved workspace, bootstraps missing index state, then
debounces filesystem events into the same repo-level indexing path.

For multi-repository local indexing, use `eshu workspace index`. The Go CLI
keeps ecosystem-wide indexing on workspace and admin flows rather than separate
ecosystem indexing commands.

## Compatibility Stubs

The current Go CLI keeps these compatibility stubs so older commands return
directed replacement guidance instead of silent behavior changes:

- `eshu delete`
- `eshu clean`
- `eshu add-package`
- `eshu ecosystem index`
- `eshu ecosystem status`

Deletion, cleanup, and recovery are owned by Go admin/runtime surfaces.
Optional runtime components use `eshu component`; `eshu add-package` does not
install source-language dependencies.

## Related Docs

- [CLI Reference](cli-reference.md)
- [Troubleshooting](troubleshooting.md)
