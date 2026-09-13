# internal/query/iac

## Purpose

`internal/query/iac` owns the IaC-quality and replatforming handler family:
Terraform/IaC resource browse, dead-IaC scanning, unmanaged-cloud-resource
finding, IaC management status and its explanation, Terraform import plan
candidates, AWS runtime drift findings, and the four replatforming routes
(plan composition, ownership packets, drift/readiness rollups, and the active
AWS collector-scope selector inventory). It moved out of root package `query`
in #6642 Part A (the #6060 lane A query-root restructure) as one of the
handler families root's own README documents under "Handler structs".

## Layout

Twenty-eight production files moved here from root, keeping every
`replatforming_*.go`/`aws_runtime_drift.go` name and applying naming.md rule 2
(drop the directory-word stutter) to every `iac`/`iac_*` name:

| Root file (pre-move) | Leaf file |
| --- | --- |
| `iac.go` | `handler.go` |
| `iac_config_shape_hints.go` | `config_shape_hints.go` |
| `iac_import_mappings.go` | `import_mappings.go` |
| `iac_import_plan.go` | `import_plan.go` |
| `iac_inventory.go` | `inventory.go` |
| `iac_inventory_postgres.go` | `inventory_postgres.go` |
| `iac_management.go` | `management.go` |
| `iac_management_access.go` | `management_access.go` |
| `iac_management_safety.go` | `management_safety.go` |
| `iac_management_status.go` | `management_status.go` |
| `iac_management_surface.go` | `management_surface.go` |
| `iac_management_transform.go` | `management_transform.go` |
| `iac_reachability_store.go` | `reachability_store.go` |
| `iac_resources.go` | `resources.go` |
| `iac_resources_metrics.go` | `resources_metrics.go` |
| `iac_resources_scope.go` | `resources_scope.go` |
| `replatforming_*.go` (11 files), `aws_runtime_drift.go` | unchanged names |

New files added by the move: `capabilities.go` (Support() constructors and
the two Part C capability mirrors), `handler_tracing.go` (the
`queryspan.HandlerTracer` seam), `drifted_attributes.go`
(`DriftedAttributesFromAWSEvidence`, moved from root's identically named
file), `main_test.go` (`TestMain` capability registration), and
`route_coverage_smoke_test.go` (six thin smoke tests, see Test Disposition).

Root keeps `iac_alias.go` (`//nolint:dirgate`): type aliases for every
pre-move exported type, the two Part C capability-string mirrors, and thin
forwarders for every family symbol a staying root file or test still needs.

## Naming

Every exported identifier that led with `IaC` dropped it (`IaCHandler` ->
`Handler`, `IaCManagementFilter` -> `ManagementFilter`, ...);
`PostgresIaCInventoryStore` and every `Replatforming*` export kept its name
because neither leads with the package word. Unexported names dropped a
leading `iac` the same way (`iacFirstNonEmpty` -> `firstNonEmpty`). A small
set of symbols that don't lead with `iac`/`IaC` were exported anyway because a
staying root forwarder or test fixture needs cross-package access to them:
the nine capability ID constants already led with a case that made this
moot, but `ManagementNextOffset`, `ManagementTruncated`,
`NormalizeManagementFindingSafety`, `NormalizeManagementFindingKinds`,
`AWSCloudRuntimeDriftDerivedStatus`, `AWSRuntimeDriftRowToManagement`,
`ReplatformingSelectorLabel`, `ReplatformingPlanRoute`, the eight
`ManagementStatus*` values, the six `FindingKind*` values, and the
`ReplatformingBlastGroup*`/`ReplatformingWave*`/`ReplatformingRollup*Key`
values were exported for that reason. `iacManagementSafetyGate` (a
constructor function) became `NewManagementSafetyGate` rather than
`ManagementSafetyGate` to avoid colliding with the type of the same rename.
`InventoryCandidate`, `InventorySearch`, `InventorySummary`, and
`InventoryFacet` (the `InventoryStore` interface's own parameter/return
types) were exported so any adapter -- including a root test double for
`resources_scope_auth_test.go`, which needs the genuine production
`AuthMiddlewareWithScopedTokens` and so cannot move -- can implement the
interface at all; before the move this was moot because every implementer
shared root's single package.

## Capabilities

This family owns nine capability IDs (`DeadCapability`, `ManagementCapability`,
`ManagementStatusCapability`, `ManagementExplainCapability`,
`TerraformImportCapability`, `AWSRuntimeDriftFindingsCapability`,
`ResourcesCapability`, `ReplatformingOwnershipCapability`,
`ReplatformingRollupsCapability`) and mirrors two it does not own
(`ReplatformingPlanReadinessCapability`, `ReplatformingSelectorInventoryCapability`,
registered by root's `contract_replatforming.go`, a Part C file this move does
not edit). `capabilities.go` declares each owned capability's `Support()`
contract; root's `capability_lockstep_iac_test.go` pins every owned
capability's string and `Support()` field-by-field against root's
`contract_capability_matrix.go` row, and pins both mirror strings equal to
root's private constants. Its `*querycontract.TruthLevel` field comparison
uses a `truthLevelPtrEqual` closure declared local to this test, not a
shared root helper: #6674 retired the shared one together with the
freshness lockstep test when root's contract rows adopted the leaf
capability constructors, so this test keeps its own copy until the same
adoption lands for this family's rows. `main_test.go`'s `TestMain` registers
all eleven capabilities (nine owned plus the two mirrors, the mirrors using a literal
`CapabilitySupport` rather than a `Support()` constructor since this package
does not own their registration) so `go test ./internal/query/iac` exercises
the same capability gate production does.

## Test disposition

Every test file that used a symbol this move made unexported in a different
package moved with its subject. Six routes this family's `Handler.Mount`
registers had no test left inside this directory once their production
callers stayed in root for a reason unrelated to this family (a genuine
production auth-middleware dependency, or the `openapi.go`/`contract_*.go`
files this move must not edit): `handleDeadIaC`,
`handleAWSRuntimeDriftFindings`, `handleReplatformingSelectors`,
`handleReplatformingRollups`, `handleReplatformingPlan`, and
`handleReplatformingOwnershipPackets` each have a thin smoke test in
`route_coverage_smoke_test.go` that only proves the route is mounted (a
non-404 response); their full behavioral coverage stays in root
(`aws_runtime_drift_test.go`, `replatforming_ownership_handler_test.go`,
`replatforming_plan_handler_test.go`, `replatforming_plan_waves_handler_test.go`,
`replatforming_rollups_handler_test.go`, `replatforming_selectors_handler_test.go`,
and the `dead_iac_*_test.go` family).

A handful of small test fixtures are duplicated rather than shared, because a
fixture's only home before the move was one package and it is now needed from
both sides of the package boundary: `fakeIaCManagementStore`
(iac/management_test.go and root's `replatforming_management_store_fake_test.go`),
`stubIaCResourceGraph`/`iacResourceNode`/`stubIaCInventoryStore`/
`newIaCResourceTestHandler`/`decodeIaCResourceList` (iac/resources_test.go,
iac/resources_current_test.go, and root's `resources_scope_auth_fakes_test.go`),
`rollupFinding`/`ownershipFinding` (iac/replatforming_ownership_test.go and
root's `replatforming_rollups_handler_test.go`/`replatforming_ownership_handler_test.go`),
and the graph-read-error sweep fixtures (`graphReadSweepCases`,
`assertGraphReadSweepResponse`, `fakeGraphReader`; iac/resources_graph_read_sweep_test.go
duplicates root's `graph_read_error_sweep_shared_test.go`/
`graph_reader_test_adapter_test.go`, the same pattern that file's own doc
comment documents for codequery). No fixture was hoisted to querytestutil;
each is a trimmed copy, not a byte-identical one (non-comment code lines:
`resources_scope_auth_fakes_test.go` 97 of 138, `replatforming_management_store_fake_test.go`
63 of 96, `resources_graph_read_sweep_test.go` 73 of 116).

## Move evidence

Measured on this worktree's base `98e0f5b23`: a detached worktree at that
commit listed 2,709 top-level test names in root package `query`
(`go test ./internal/query/ -list '.*'`). After the move, root lists 2,634
names and this package lists 83 (77 moved plus 6 new smoke tests); root's net
change is 2,709 - 77 + 2 = 2,634, the +2 being two new pinning tests root
gained (`TestIaCCapabilityLockstep`, `TestIaCReplatformingCapabilityMirrorsMatchRoot`).
The union of root (2,634) and this package (83) is 2,717 = 2,709 + 8: every
one of base's 2,709 names is still selected somewhere (root or this package),
and the only 8 additions across the whole tree are the six named smoke tests
plus the two lockstep tests above. `go list -deps ./internal/query/iac`
contains no `github.com/eshu-hq/eshu/go/internal/query` entry, so the split
introduces no import cycle. `internal/query`'s dirgate row shrank from 361 to
334 non-test files (digest `a31cec4ac6d97abe6cb5305155e05e19f3aaa8a6a488e479058c5ae1d39e4d34`
-> `5bbe21cf7e39e9166e9a4713e20809ea17975954a2fc2e00ef4d8e9e6a18e586`,
`scripts/lib/dirgate-grandfather.tsv`); the new `iac/` directory (52 files: 50
`.go` -- 32 non-test -- plus `README.md` and `AGENTS.md`) carries no
naming-exemption row and passes `verify-dirgate.sh --all` on its own weight.

## No-Regression Evidence

No-Regression Evidence: `go test ./internal/query/iac/... -count=1` (138 cases before the two route-
coverage-driven additions above, 145 after -- `=== RUN` lines including
subtests) and `go test ./internal/query/... -count=1` (9,401 `=== RUN` lines
across the whole tree) both pass with the moved
production code unmodified except package-qualified spellings, imports, and
doc comments. `go test ./cmd/api ./cmd/mcp-server ./internal/mcp
./cmd/golden-corpus-gate/... -count=1` and `go test ./internal/queryplan/...
-count=1` also pass; `internal/mcp/route_serves_data_registry_routes_2.go`'s
`GET /api/v0/iac/resources` row and `internal/queryplan/testdata/query-source-coverage.yaml`'s
`(*Handler).listResources` row were re-pinned to the new path/symbol/source
digest (path and symbol only; class, count, and disposition unchanged).

## No-Observability-Change

No-Observability-Change: this move adds no new span, metric, log key, or runtime knob.
`handler_tracing.go` seeds its tracer from the same `queryspan.HandlerTracer()`
seam root's `handler_tracing.go` used before the move (see
`freshness/handler_tracing.go` for the identical precedent), so every route's
`telemetry.SpanQueryIaC*`/`telemetry.SpanQueryDeadIaC` span name and
`http.route`/`eshu.capability` attributes are unchanged. The nine capability
IDs and the two capability-string mirrors carry the identical string values
root registered before the move.
