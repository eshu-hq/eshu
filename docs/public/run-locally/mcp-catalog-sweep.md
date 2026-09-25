# Scoped-Token MCP Catalog Sweep

Issue #5167 asks that every MCP tool succeeds with a personal token whose role
covers it, and that a route which cannot be tenant-filtered refuses a scoped
token and says why. The opt-in `catalog-sweep` module of the MCP-identity E2E
harness proves both on a fresh `docker-compose.e2e.yaml` stack (project
`eshu-e2e-auth-mcp`, 29xxx ports; see
[Docker Compose](docker-compose.md#mcp-identity-auth-e2e-stack)).

```bash
bash scripts/run-auth-mcp-e2e.sh --module catalog-sweep
```

The module never runs in the empty-selector full suite, so the `auth-mcp-e2e`
baseline manifest is unchanged.

## How it decides pass or fail

- **Policy from the Go source.** Before any stack work the script runs
  `go test ./internal/mcp -run TestCatalogSweepPolicy` with
  `ESHU_CATALOG_SWEEP_POLICY_OUT` set. The test resolves each tool's route the
  way `dispatchTool` does and classifies it with
  `ScopedHTTPRouteSupportsTenantFilter`, `IsSharedKeyOnlyRoute`, and
  `IsPendingRowFilteringRoute`, so the expected outcome cannot drift from the
  route policy. The derived table is written under `e2e-artifacts/` and is not
  checked in.
- **A new tool cannot be skipped.** The same test, part of the default
  `go test ./internal/mcp`, fails when a registered tool has no entry in
  `go/internal/mcp/testdata/catalog_sweep_args.json` (the checked-in per-tool
  minimal-argument table), when the table names a tool that is not registered,
  when a case resolves to a route no ledger classifies, and when a route that
  refuses scoped tokens is not disclosed in its tool description. The runner
  repeats the tool-name comparison against the live `tools/list`.
- **Scoped, not shared.** The stack has no shared `ESHU_API_KEY`. The module
  seeds a role granting every feature and data class plus a repository target
  for one seeded repository, mints a personal token through `/profile`, and
  asserts through `GET /api/v0/auth/profile` that the token resolves through
  roles with the permission catalog enforced.
- **Every tool, judged.** Each tool is called with fixed small limits and
  seeded identifiers. The seed gives the granted repository a graph node and a
  repository-catalog scope, adds an ungranted repository the same way, and adds
  one `state_snapshot` scope; there is no indexed content. Of the 167
  checked-in calls, 130 allowlisted calls must answer `ok`: they name the
  seeded repository or scope, or need no subject, so a grant filter that wrongly
  hid the seeded subject would fail them. 28 allowlisted calls may also answer a
  second outcome, each with a specific `acceptReason`: 21 name a subject the
  fixture does not seed (a workload, service, code symbol, file, or evidence
  packet) and may answer a typed `not_found`; 7 depend on the stack profile
  (`unsupported_capability` for code divergence and path comparison, `503` for
  `ask` because `ESHU_ASK_ENABLED` is unset, `component_registry_unavailable`
  because `ESHU_COMPONENT_HOME` is unset). For those 28 the proof is only that
  the route is mounted and the grant admitted the call. The Go test rejects
  such an entry unless its `acceptReason` quotes one of the row's own string
  arguments or the tool name, and unless every accepted outcome is one it
  lists. The remaining 9 calls reach a ledger or shared-key-only route, which
  must answer the route-policy `403` with a live description that discloses it.
  An unexpected `403`, an unmounted route, an invalid-argument `400`, or a `5xx`
  fails. The runner prints this split in the step detail.
- **Negative control.** The same token, asked for a second seeded repository it
  was not granted, must not read it: `list_indexed_repositories` returns the
  granted repository only, and each single-repository tool refuses the ungranted
  id while answering for the granted one. A positive control makes the same
  ungranted-repository read through the all-scope console session; it must
  answer `200`, so the scoped refusal is not vacuous. The run prints one
  `granted=... ungranted=... all-scope(ungranted)=...` line per tool.

The runner prints a tool / route / expected / actual table with the pass count
and writes it to `e2e-artifacts/auth-mcp-e2e-catalog-sweep.txt`
(`.json` beside it).
