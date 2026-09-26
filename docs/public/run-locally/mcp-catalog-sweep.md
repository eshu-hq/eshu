# Scoped-Token MCP Catalog Sweep

Issue #5167 asks that every MCP tool succeeds with a personal token whose role
covers it, and that a route which cannot be tenant-filtered refuses a scoped
token and says why. The opt-in `catalog-sweep` module of the MCP-identity E2E
harness proves both on a fresh `docker-compose.e2e.yaml` stack (project
`eshu-e2e-auth-mcp`, 29xxx ports, graph on Neo4j; see
[Docker Compose](docker-compose.md#mcp-identity-auth-e2e-stack)).

```bash
bash scripts/run-auth-mcp-e2e.sh --module catalog-sweep
```

The module never runs in the empty-selector full suite, so the `auth-mcp-e2e`
baseline manifest is unchanged.

## How it decides pass or fail

- **Policy from the Go source.** Before any stack work the script runs
  `go test ./internal/mcp -run TestCatalogSweepPolicy` with its
  policy-output variable set (named by `catalogSweepPolicyOutEnv` in
  `dispatch_catalog_sweep_policy_test.go`). The test resolves each tool's route the
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
- **A route promotion cannot go unnoticed.** Each case's route class is checked
  in beside it, in `go/internal/mcp/testdata/catalog_sweep_expected_classes.json`
  (keyed `<tool>/<label>`). The test derives the class from the live predicates
  and fails when it differs, so a route promoted off (or onto) a ledger stops
  `go test ./internal/mcp` until the case's arguments and accepted outcomes are
  revisited and the expectation is updated. The derived policy is still never
  checked in; only the class each case was written against is.
- **Scoped, not shared.** The stack has no shared `ESHU_API_KEY`. The module
  seeds a role granting every feature and data class plus a repository target
  for one seeded repository, mints a personal token through `/profile`, and
  asserts through `GET /api/v0/auth/profile` that the token resolves through
  roles with the permission catalog enforced.
- **Every tool, judged.** Each tool is called with fixed small limits and
  seeded identifiers. The seed gives the granted repository a graph node and a
  repository-catalog scope, adds an ungranted repository the same way, and adds
  one `state_snapshot` scope; there is no indexed content. Of the 167
  checked-in calls, 134 allowlisted calls must answer `ok`: they name the
  seeded repository or scope, or need no subject, so a grant filter that wrongly
  hid the seeded subject would fail them. 30 allowlisted calls may also answer a
  second outcome, each with a specific `acceptReason`: 23 name a subject the
  fixture does not seed (a workload, service, code symbol, file, or evidence
  packet) and may answer a typed `not_found`; 7 depend on the stack profile
  (`unsupported_capability` for code divergence and path comparison, the
  default-off `503` for `ask` because Ask Eshu is not enabled on the stack, matched on
  its "ask is not enabled" body so a backend `503` still fails,
  `component_registry_unavailable` because `ESHU_COMPONENT_HOME` is unset). For
  those 30 the proof is only that the route is mounted and not refused by the
  route policy; the answer alone does not tell an unseeded subject from a
  filtered one. Five of them (`analyze_code_relationships` for `who_modifies`,
  `calculate_cyclomatic_complexity`, `get_file_content`, `get_file_lines`,
  `trace_route_callers`) name the granted repository in their arguments and
  answer `not_found`, so the runner replays each through the all-scope console
  session, sending the request the MCP dispatcher sends (the policy carries each
  row's dispatched body and query), which must answer the same typed `404`: the
  fixture, not the grant, lacks the subject. The `who_modifies` dispatch sends
  `name` and `repo_id` (#7221), so its control proves the granted repository
  lacks the entity. The Go test rejects a tolerant
  entry unless its `acceptReason` quotes one of the row's own unseeded string
  arguments (a seeded-subject placeholder such as `$REPO`, a seeded id, or the
  tool name alone does not count), or, for a row that accepts only capability
  outcomes, names the `ESHU_*` variable, query profile, or graph mode it
  depends on, and unless every accepted outcome is one it
  lists. The remaining 3 calls reach a ledger or shared-key-only route (1
  pending row filtering, 2 shared-key only), which
  must answer the route-policy `403` with a live description that discloses it.
  An unexpected `403`, an unmounted route, an invalid-argument `400`, or a `5xx`
  fails. The runner prints this split in the step detail.
  The static split (134 `ok`, 30 tolerant, 3 ledger) is derived from that
  policy output. The latest live run, on Neo4j at `2eb226f58`, passed all 167 of
  167 calls, twice in a row, and the full suite passed 40/40 (author-run, on an
  Apple Silicon host with the Neo4j container running emulated amd64; timings are
  not native). It includes every row promoted off the pending-row-filtering ledger by
  #7183, #7193, #7191 and #7194: `search_registry_bundles/default` returned an
  empty `ok` page to the scoped token (a scoped caller reads only public
  packages and the fixture seeds none), and `find_infra_resources`,
  `analyze_infra_relationships` (#7226, #7215) and `count_infra_resources`
  (#7239, #7231) pass. The evidence page has the per-tool table, the image digests
  and the earlier 166/167 run.
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
