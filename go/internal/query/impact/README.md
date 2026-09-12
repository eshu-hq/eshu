# Impact handler family

## Purpose

`impact` holds the Handler HTTP surface and every file that declares
one of its methods (Issue #6060, lane B): blast radius, change surface
(investigate, legacy, traversal), pre-change checks, developer change plans,
contracts, entity maps, resource investigation, deployment-trace chain and
its GitOps/OCI/K8s/source pieces, exposure paths, and the exported seam the
staying root package consumes through aliases in `family_impact_shim.go`.

## Ownership boundary

This package owns handler orchestration for the impact routes and the
`Handler` struct with its `Neo4j`, `Content`, `Profile`, `TraceContext`,
`CodeSurface`, and `PathProbe` dependencies. Non-method helpers the family
needs but that touch no handler state live in `impacttrace`; this package
imports `impacttrace`, never the reverse, and neither imports the query
root (the root would cycle back through `compare.go` and
`family_impact_shim.go`).

It also owns the family's capability rows (`capabilities.go`, registered via
`querycontract.RegisterCapabilities`): each family declares the support
contract for the routes it implements, following the
`semanticsearch.Support` precedent. The query root's matrix must not repeat
these rows — duplicate initialization is a contract failure.

The staying root package keeps thin aliases and forwarders
(`family_impact_shim.go`) so external callers (`query_test` seam tripwires,
`cmd` wiring) keep their spelling. Production graph drivers, the Neo4j
driver, service-story shaping, and entity handling stay in root behind the
`TraceContext`, `CodeSurface`, and `PathProbe` interfaces; the root `init`
assigns the production adapters.

## Exported surface

The exported surface is described in [doc.go](doc.go). Method families that
moved here keep an exported boundary for the staying root tests that pin
them (query text builders, selector predicates, traversal specs, result
types with exported `Rows`/`Limits` fields or `Rows()`/`Limits()` methods,
the `entityMapResolverQuery` `Cypher`/`Params` fields). Unexported helpers
stay unexported; cross-package test pins go through `querytestutil`
(`FakeGraphReader`, `FakePortContentStore`, `RecordingResourceInvestigationGraph`,
`SqlBlastRadius*` cleanup probes, `ScopedTestAuthContext`) or `querycontract`
(row-value decoders, shared bounds, ports).

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
capability registry, shared bounds), `querytestutil` in tests only,
`impacttrace`, `queryauth` (tests), and `queryspan`/`internal/telemetry`
for handler spans. It must not import the query root or graph drivers.

## Telemetry

Handlers keep their existing `query.*` spans and `eshu_dp_api_request_duration_seconds`
timing; the move changes no operator signal. New runtime behavior must add
spans an operator can use at 3 AM.

No-Observability-Change: relocating handler methods between packages emits
nothing and moves no span boundary, attribute, or log line.

## Performance

Handler methods are request-orchestration, not a hot decode loop; the move
adds no call indirection to row paths (shared helpers are called directly,
and the small `querycontract` row decoders inline away). No benchmark delta
is claimed because there is no runtime delta to measure.

No-Regression Evidence: `go test ./internal/query/... ./internal/mcp/... -count=1`,
`go test ./internal/queryplan/ -count=1`, the golden-corpus, replay-coverage,
and ci gates, plus `go build ./...` and `go vet ./...`, all exit 0. The B-7
cassettes and B-12 snapshot are byte-identical: the diff moves definitions,
import blocks, and manifest digests, and touches no Cypher text, queue,
lease, or projection path.

## Naming (#6642 Part D)

Rule 2 (never repeat the directory name in the file name) and rule 4 (no
package-name stutter in exported identifiers) destutter: 65 of the 83 files
renamed (`git mv`, history preserved) to drop the leading `impact`/`impact_`
or, in a later round, the trailing `_impact` stutter (a suffix repeat is a
repeat too); the 18 files that already complied (`capabilities.go`,
`deployment_config_influence*.go`, `developer_change_plan.go`,
`entity_map*.go`, `exposure_path*.go`, `doc.go`) stayed. Every test function
name is unchanged.

| Old | New |
| --- | --- |
| `impact.go` | `handler.go` |
| `impact_blast_radius.go` | `blast_radius.go` |
| `impact_blast_radius_coverage_test.go` | `blast_radius_coverage_test.go` |
| `impact_blast_radius_grant_before_limit_test.go` | `blast_radius_grant_before_limit_test.go` |
| `impact_blast_radius_rows.go` | `blast_radius_rows.go` |
| `impact_blast_radius_test.go` | `blast_radius_test.go` |
| `impact_bounds.go` | `bounds.go` |
| `impact_candidate_access_filter.go` | `candidate_access_filter.go` |
| `impact_change_surface_code.go` | `change_surface_code.go` |
| `impact_change_surface_grant_before_limit_test.go` | `change_surface_grant_before_limit_test.go` |
| `impact_change_surface_investigation.go` | `change_surface_investigation.go` |
| `impact_change_surface_investigation_test.go` | `change_surface_investigation_test.go` |
| `impact_change_surface_label_whitelist_test.go` | `change_surface_label_whitelist_test.go` |
| `impact_change_surface_legacy.go` | `change_surface_legacy.go` |
| `impact_change_surface_legacy_test.go` | `change_surface_legacy_test.go` |
| `impact_change_surface_resolvers.go` | `change_surface_resolvers.go` |
| `impact_change_surface_response.go` | `change_surface_response.go` |
| `impact_change_surface_semantics_test.go` | `change_surface_semantics_test.go` |
| `impact_change_surface_traversal.go` | `change_surface_traversal.go` |
| `impact_defaults_test.go` | `defaults_test.go` |
| `impact_edge_materialization_gate.go` | `edge_materialization_gate.go` |
| `impact_edge_materialization_gate_test.go` | `edge_materialization_gate_test.go` |
| `impact_legacy_bounds_test.go` | `legacy_bounds_test.go` |
| `impact_path_probe_test.go` | `path_probe_test.go` |
| `impact_resource_investigation.go` | `resource_investigation.go` |
| `impact_resource_investigation_flux_helm_release_test.go` | `resource_investigation_flux_helm_release_test.go` |
| `impact_resource_investigation_reads.go` | `resource_investigation_reads.go` |
| `impact_resource_investigation_response.go` | `resource_investigation_response.go` |
| `impact_resource_investigation_selector.go` | `resource_investigation_selector.go` |
| `impact_resource_investigation_selector_slo_live_test.go` | `resource_investigation_selector_slo_live_test.go` |
| `impact_resource_investigation_selector_test.go` | `resource_investigation_selector_test.go` |
| `impact_resource_investigation_test.go` | `resource_investigation_test.go` |
| `impact_seam.go` | `seam.go` |
| `impact_trace_cloud_resource_limits_test.go` | `trace_cloud_resource_limits_test.go` |
| `impact_trace_deployment.go` | `trace_deployment.go` |
| `impact_trace_deployment_argocd_test.go` | `trace_deployment_argocd_test.go` |
| `impact_trace_deployment_cloud_limits_test.go` | `trace_deployment_cloud_limits_test.go` |
| `impact_trace_deployment_config_bounds_test.go` | `trace_deployment_config_bounds_test.go` |
| `impact_trace_deployment_controllers.go` | `trace_deployment_controllers.go` |
| `impact_trace_deployment_enrichment_test.go` | `trace_deployment_enrichment_test.go` |
| `impact_trace_deployment_flux_test.go` | `trace_deployment_flux_test.go` |
| `impact_trace_deployment_gitops_limits_handler_test.go` | `trace_deployment_gitops_limits_handler_test.go` |
| `impact_trace_deployment_gitops_own_repo_test.go` | `trace_deployment_gitops_own_repo_test.go` |
| `impact_trace_deployment_k8s_limits_test.go` | `trace_deployment_k8s_limits_test.go` |
| `impact_trace_deployment_k8s_select.go` | `trace_deployment_k8s_select.go` |
| `impact_trace_deployment_k8s_select_truncation_test.go` | `trace_deployment_k8s_select_truncation_test.go` |
| `impact_trace_deployment_k8s_select_widening_test.go` | `trace_deployment_k8s_select_widening_test.go` |
| `impact_trace_deployment_k8s_tristate_test.go` | `trace_deployment_k8s_tristate_test.go` |
| `impact_trace_deployment_live_evidence.go` | `trace_deployment_live_evidence.go` |
| `impact_trace_deployment_live_evidence_count.go` | `trace_deployment_live_evidence_count.go` |
| `impact_trace_deployment_live_evidence_count_test.go` | `trace_deployment_live_evidence_count_test.go` |
| `impact_trace_deployment_live_evidence_count_truncation_test.go` | `trace_deployment_live_evidence_count_truncation_test.go` |
| `impact_trace_deployment_live_evidence_declared_test.go` | `trace_deployment_live_evidence_declared_test.go` |
| `impact_trace_deployment_live_evidence_test.go` | `trace_deployment_live_evidence_test.go` |
| `impact_trace_deployment_oci.go` | `trace_deployment_oci.go` |
| `impact_trace_deployment_oci_test.go` | `trace_deployment_oci_test.go` |
| `impact_trace_deployment_query_test.go` | `trace_deployment_query_test.go` |
| `impact_trace_deployment_resources.go` | `trace_deployment_resources.go` |
| `impact_trace_deployment_sources.go` | `trace_deployment_sources.go` |
| `impact_trace_deployment_test.go` | `trace_deployment_test.go` |

A later round renamed the five files that still repeated `impact` as a
trailing suffix rather than a leading prefix — rule 2 forbids the stutter
either way:

| Old | New |
| --- | --- |
| `contract_impact.go` | `contract.go` |
| `contract_impact_test.go` | `contract_test.go` |
| `prechange_impact.go` | `prechange.go` |
| `prechange_impact_request.go` | `prechange_request.go` |
| `prechange_impact_test.go` | `prechange_test.go` |

Four exported identifiers led with the package word and are renamed at their
declarations (rule 4 retires the old names, so every qualified caller inside
`internal/query` — root's `family_impact_shim.go`/`family_impact_change_surface_code.go`/
`family_impact_path_probe_adapter.go`/`compare.go`/`compare_story.go`/
`impact_seam_export_test.go`/`w3_scoped_grant_filter_bench_test.go`,
`repository/context_helpers.go`, and
`service/query_truncation_wiring_test.go`/`service/evidence_file_bound_test.go`
— was repointed in the same change):

| Old | New |
| --- | --- |
| `ImpactHandler` | `Handler` |
| `ImpactPathProbeBackend` | `PathProbeBackend` |
| `ImpactMaxListLimit` | `MaxListLimit` |
| `ImpactRepoIDAllowed` | `RepoIDAllowed` |

The root alias in `family_impact_shim.go` keeps its pre-move spelling
(`type ImpactHandler = impact.Handler`) so `cmd/api`/`cmd/mcp-server` wiring
and the ~260 `query.ImpactHandler`/`ImpactHandler` call sites across
`internal/query` compile unchanged; only this package's own declarations and
their direct qualified callers moved. `impacttrace.ImpactRepoIDAllowed` is a
different package's own export (its own rule-2/4 debt, out of scope here) and
keeps its name; this package's `RepoIDAllowed` seam still forwards to it.

No-Regression Evidence (#6642 rule 2/4): `go test ./internal/query/... -count=1`,
`go test ./cmd/api ./cmd/mcp-server ./internal/mcp ./internal/queryplan/...
./cmd/golden-corpus-gate/... -count=1`, `go build ./...`, and `go vet ./...`
all exit 0 on the renamed tree; `go test ./internal/query/impact -list '.*'`
lists the identical sorted test-function set before and after. The 15
`query-source-coverage.yaml` and 2 `hot-cypher.yaml` digests that moved did so
only because the recorded function text names the renamed `Handler` receiver
or `RunChangeSurface*` symbols, not because any Cypher, row shape, or bound
changed; `go test ./internal/queryplan/... -count=1` is green on the re-pinned
rows. The B-7 cassettes and B-12 golden snapshot are byte-identical
(`git diff origin/main --stat -- testdata/golden testdata/cassettes` is empty).

No-Observability-Change (#6642 rule 2/4): the rename touches no span, metric,
or log name; `query.*` spans and `eshu_dp_api_request_duration_seconds`
timing are unchanged.

## Gotchas / invariants

- A test binary for this package does not run the query root's `init`, so
  the capability registry and the `Default*` backends arrive empty. Gated
  HTTP paths answer 501 and backend-backed reads nil-panic. The external
  `impact_test` init file (`defaults_test.go`) wires the production
  adapters from root constructors — legal because nothing imports
  `impact_test`, so it cannot cycle — reproducing the base environment
  exactly. Tests needing narrower behavior inject per-handler fakes instead.
- `BuildDeploymentTraceResponse` takes a caller-built, non-nil overview map
  and attaches counts in place. Counts that are pure functions of the fields
  (instance/environment/platform/config counts, evidence counts) derive
  inside, reproducing the service-story builder's values exactly; evidence
  lists attach only when the caller left them absent, so the builder's
  normalized forms win in production. `deployment_truth_tier` stays fully
  caller-owned.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext`, never hand-built to widen access.

## Verification

From `go/`, run `go test ./internal/query/... ./internal/mcp/... -count=1`,
`go test ./internal/queryplan/ -count=1`, `go build ./...`, and
`go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh` and the B-7 golden-corpus proof selected by
the parent package instructions.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
