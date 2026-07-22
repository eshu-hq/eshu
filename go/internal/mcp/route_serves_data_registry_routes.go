// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// routeServesDataRegistryPart1 is the first half of the #5584
// handler-derived route→domain registry (split across two files for the
// 500-line cap; merged into routeServesDataRegistry in
// route_serves_data_registry.go): one entry per read_surface route in
// routeServesDataBackingMap, each derived by reading the registered handler
// method and its store implementation's actual SQL/Cypher. Every claim is
// verified against real source by route_serves_data_registry_check.go; a
// citation that stops matching turns the gate RED. File paths are
// repo-relative.
//
// Per-route derivation rationale, producer-side file:line citations, and
// the flagged architect-review items live in
// docs/internal/design/5584-route-serves-data-registry.md.
var routeServesDataRegistryPart1 = map[string]routeServesDataSource{
	// DocumentationHandler.listFacts reads the collected documentation fact
	// family from fact_records via (*ContentReader).documentationFacts: the
	// IN (...) list is built from facts.Documentation*FactKind constants
	// (go/internal/query/documentation_read_model.go:359-367). The list also
	// includes facts.SemanticDocumentationObservationFactKind (line 366) —
	// semantic observation rows genuinely return through this route, hence
	// the disclosure.
	"GET /api/v0/documentation/facts": {
		RegistrationFile: "go/internal/query/documentation.go",
		HandlerStruct:    "DocumentationHandler",
		StructFile:       "go/internal/query/documentation.go",
		Method:           "listFacts",
		MethodFile:       "go/internal/query/documentation_facts.go",
		ScanFiles: []string{
			"go/internal/query/documentation_facts.go",
			"go/internal/query/documentation_read_model.go",
		},
		Served: []routeServedDomain{{
			Domain: "documentation_materialization",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/documentation_read_model.go", Marker: "facts.DocumentationSourceFactKind"},
				{File: "go/internal/query/documentation_read_model.go", Marker: "FROM fact_records"},
			},
		}},
		Disclosed: []routeDisclosure{{
			Domain: "semantic_entity_materialization",
			Reason: "documentationCollectedFactKindSQLList includes semantic.documentation_observation, so semantic observation rows are returned by this route's facts listing; the semantic family's own declared read surface is GET /api/v0/semantic/documentation-observations",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/documentation_read_model.go", Marker: "facts.SemanticDocumentationObservationFactKind"},
			},
		}},
	},

	// CloudInventoryHandler.listInventory reads exactly one reducer-owned
	// canonical kind: fact_kind = 'reducer_cloud_resource_identity'
	// (go/internal/query/cloud_inventory_read_model.go:22,88). That kind is
	// written by reducer/cloud_inventory_admission_writer.go from the closed
	// provider source set {aws_resource, gcp_cloud_resource,
	// azure_cloud_resource} (projector/cloud_inventory_admission_intents.go:18-21),
	// which is how all three provider domains are served here.
	"GET /api/v0/cloud/inventory": {
		RegistrationFile: "go/internal/query/cloud_inventory_readback.go",
		HandlerStruct:    "CloudInventoryHandler",
		StructFile:       "go/internal/query/cloud_inventory_readback.go",
		Method:           "listInventory",
		MethodFile:       "go/internal/query/cloud_inventory_readback.go",
		ScanFiles: []string{
			"go/internal/query/cloud_inventory_readback.go",
			"go/internal/query/cloud_inventory_read_model.go",
		},
		Served: []routeServedDomain{
			{
				Domain: "aws_cloud_runtime_drift",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_inventory_read_model.go", Marker: "reducer_cloud_resource_identity"},
					{File: "go/internal/projector/cloud_inventory_admission_intents.go", Marker: "facts.AWSResourceFactKind"},
				},
			},
			{
				Domain: "azure_resource_materialization",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_inventory_read_model.go", Marker: "reducer_cloud_resource_identity"},
					{File: "go/internal/projector/cloud_inventory_admission_intents.go", Marker: "facts.AzureCloudResourceFactKind"},
				},
			},
			{
				Domain: "gcp_resource_materialization",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_inventory_read_model.go", Marker: "reducer_cloud_resource_identity"},
					{File: "go/internal/projector/cloud_inventory_admission_intents.go", Marker: "facts.GCPCloudResourceFactKind"},
				},
			},
		},
	},

	// CICDHandler.listRunCorrelations -> h.Correlations
	// (PostgresCICDRunCorrelationStore): fact_kind = $1 bound to
	// "reducer_ci_cd_run_correlation"
	// (go/internal/query/ci_cd_run_correlations.go:15,144).
	"GET /api/v0/ci-cd/run-correlations": {
		RegistrationFile: "go/internal/query/ci_cd.go",
		HandlerStruct:    "CICDHandler",
		StructFile:       "go/internal/query/ci_cd.go",
		Method:           "listRunCorrelations",
		MethodFile:       "go/internal/query/ci_cd.go",
		ScanFiles: []string{
			"go/internal/query/ci_cd.go",
			"go/internal/query/ci_cd_run_correlations.go",
		},
		Served: []routeServedDomain{{
			Domain:     "ci_cd_run_correlation",
			StoreField: "Correlations",
			StoreType:  "CICDRunCorrelationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/ci_cd_run_correlations.go", Marker: "reducer_ci_cd_run_correlation"},
			},
		}},
	},

	// RepositoryHandler.listRepositories is a graph read over the Repository
	// label (go/internal/query/repository.go:66,164-171), the canonical
	// code-graph projection's output. The struct's CICDRunCorrelations and
	// ServiceCatalogCorrelations fields back sibling repository routes and
	// are NOT referenced by listRepositories.
	"GET /api/v0/repositories": {
		RegistrationFile: "go/internal/query/repository.go",
		HandlerStruct:    "RepositoryHandler",
		StructFile:       "go/internal/query/repository.go",
		Method:           "listRepositories",
		MethodFile:       "go/internal/query/repository.go",
		ScanFiles: []string{
			"go/internal/query/repository.go",
		},
		Served: []routeServedDomain{{
			Domain: "code_graph_projection",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/repository.go", Marker: "MATCH (r:Repository)"},
			},
		}},
	},

	// SupplyChainHandler.listImpactFindings -> h.ImpactFindings
	// (PostgresSupplyChainImpactFindingStore): both the legacy and winners
	// queries bind fact_kind = $1 to "reducer_supply_chain_impact_finding"
	// (go/internal/query/supply_chain_impact_findings_queries.go:6,56).
	// reducer_derived_findings owns the kind
	// (specs/fact-kind-registry.v1.yaml:131-142); supply_chain_impact is the
	// producing projection (scanner_worker family, specs:346-356) — both are
	// served by the same read.
	"GET /api/v0/supply-chain/impact/findings": {
		RegistrationFile: "go/internal/query/supply_chain.go",
		HandlerStruct:    "SupplyChainHandler",
		StructFile:       "go/internal/query/supply_chain.go",
		Method:           "listImpactFindings",
		MethodFile:       "go/internal/query/supply_chain_impact_findings_handler.go",
		ScanFiles: []string{
			"go/internal/query/supply_chain_impact_findings_handler.go",
			"go/internal/query/supply_chain_impact_findings.go",
			"go/internal/query/supply_chain_impact_findings_queries.go",
		},
		Served: []routeServedDomain{
			{
				Domain:     "reducer_derived_findings",
				StoreField: "ImpactFindings",
				StoreType:  "SupplyChainImpactFindingStore",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/supply_chain_impact_findings_queries.go", Marker: "reducer_supply_chain_impact_finding"},
				},
			},
			{
				Domain:     "supply_chain_impact",
				StoreField: "ImpactFindings",
				StoreType:  "SupplyChainImpactFindingStore",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/supply_chain_impact_findings_queries.go", Marker: "reducer_supply_chain_impact_finding"},
					{File: "go/internal/reducer/supply_chain_impact_writer.go", Marker: "ReducerSupplyChainImpactFindingFactKind"},
				},
			},
		},
	},

	// InfraHandler.listCloudResources pages CloudResource identities from
	// the graph_node_owner ledger (cloud_resource_list_store.go:137-142)
	// and hydrates MATCH (n:CloudResource) (cloud_resources.go:203-223).
	// ec2 MERGEs the nodes; rds and s3 decorate them with posture
	// properties — all three domains materialize onto the label this route
	// enumerates. The aws/azure/gcp base creators also MERGE CloudResource
	// nodes; that shared surface is documented in the design doc, not
	// signature-encoded.
	"GET /api/v0/cloud/resources": {
		RegistrationFile: "go/internal/query/cloud_resources.go",
		HandlerStruct:    "InfraHandler",
		StructFile:       "go/internal/query/infra.go",
		Method:           "listCloudResources",
		MethodFile:       "go/internal/query/cloud_resources.go",
		ScanFiles: []string{
			"go/internal/query/cloud_resources.go",
			"go/internal/query/cloud_resource_list_store.go",
		},
		Served: []routeServedDomain{
			{
				Domain: "ec2_instance_node_materialization",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_resources.go", Marker: "MATCH (n:CloudResource)"},
					{File: "go/internal/storage/cypher/ec2_instance_node_writer.go", Marker: "MERGE (r:CloudResource {uid: row.uid})"},
				},
			},
			{
				Domain: "rds_posture_materialization",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_resources.go", Marker: "MATCH (n:CloudResource)"},
					{File: "go/internal/storage/cypher/rds_posture_node_writer.go", Marker: "MATCH (r:CloudResource {uid: row.uid})"},
				},
			},
			{
				Domain: "s3_internet_exposure_materialization",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/cloud_resources.go", Marker: "MATCH (n:CloudResource)"},
					{File: "go/internal/storage/cypher/s3_internet_exposure_node_writer.go", Marker: "MATCH (resource:CloudResource {uid: row.uid})"},
				},
			},
		},
	},

	// IncidentHandler.getIncidentContext -> h.Context
	// (PostgresIncidentContextStore). Core reads: incident.record,
	// incident.lifecycle_event, change.record
	// (incident_context_sql.go:36,48,61) = the incident_context family
	// (incident_repository_correlation); routing reads: the
	// incident_routing.* kinds (incident_context_routing_sql.go:31,51,71) =
	// incident_routing_materialization. The runtime-enrichment branch also
	// reads kubernetes and CI/CD correlation kinds — disclosed, not served.
	"GET /api/v0/incidents/{incident_id}/context": {
		RegistrationFile: "go/internal/query/incident_context_handler.go",
		HandlerStruct:    "IncidentHandler",
		StructFile:       "go/internal/query/incident_context_handler.go",
		Method:           "getIncidentContext",
		MethodFile:       "go/internal/query/incident_context_handler.go",
		ScanFiles: []string{
			"go/internal/query/incident_context_handler.go",
			"go/internal/query/incident_context_store.go",
			"go/internal/query/incident_context_sql.go",
			"go/internal/query/incident_context_routing_sql.go",
			"go/internal/query/incident_context_runtime_sql.go",
			"go/internal/query/incident_context_runtime_store.go",
		},
		Served: []routeServedDomain{
			{
				Domain:     "incident_repository_correlation",
				StoreField: "Context",
				StoreType:  "IncidentContextStore",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/incident_context_sql.go", Marker: "'incident.record'"},
					{File: "go/internal/query/incident_context_sql.go", Marker: "'incident.lifecycle_event'"},
					{File: "go/internal/query/incident_context_sql.go", Marker: "'change.record'"},
				},
			},
			{
				Domain:     "incident_routing_materialization",
				StoreField: "Context",
				StoreType:  "IncidentContextStore",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/incident_context_routing_sql.go", Marker: "incident_routing.applied_pagerduty_resource"},
					{File: "go/internal/query/incident_context_routing_sql.go", Marker: "incident_routing.observed_pagerduty_service"},
				},
			},
		},
		Disclosed: []routeDisclosure{
			{
				Domain: "kubernetes_correlation",
				Reason: "runtime-evidence enrichment: the incident context response decorates incidents with reducer_kubernetes_correlation rows resolved by image digest; the correlation rows' own read surface is GET /api/v0/kubernetes/correlations",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/incident_context_runtime_sql.go", Marker: "reducer_kubernetes_correlation"},
				},
			},
			{
				Domain: "ci_cd_run_correlation",
				Reason: "runtime-evidence enrichment: the incident context response decorates incidents with reducer_ci_cd_run_correlation rows; the correlation rows' own read surface is GET /api/v0/ci-cd/run-correlations",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/incident_context_runtime_sql.go", Marker: "reducer_ci_cd_run_correlation"},
				},
			},
		},
	},

	// KubernetesHandler.listCorrelations -> h.Correlations
	// (PostgresKubernetesCorrelationStore): fact_kind = $1 bound to
	// "reducer_kubernetes_correlation"
	// (go/internal/query/kubernetes_correlations.go:15,168).
	"GET /api/v0/kubernetes/correlations": {
		RegistrationFile: "go/internal/query/kubernetes.go",
		HandlerStruct:    "KubernetesHandler",
		StructFile:       "go/internal/query/kubernetes.go",
		Method:           "listCorrelations",
		MethodFile:       "go/internal/query/kubernetes.go",
		ScanFiles: []string{
			"go/internal/query/kubernetes.go",
			"go/internal/query/kubernetes_correlations.go",
		},
		Served: []routeServedDomain{{
			Domain:     "kubernetes_correlation",
			StoreField: "Correlations",
			StoreType:  "KubernetesCorrelationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/kubernetes_correlations.go", Marker: "reducer_kubernetes_correlation"},
			},
		}},
	},

	// ObservabilityCoverageHandler.listCorrelations -> h.Correlations:
	// fact_kind = $1 bound to "reducer_observability_coverage_correlation"
	// (go/internal/query/observability_coverage_correlations.go:15,172).
	"GET /api/v0/observability/coverage/correlations": {
		RegistrationFile: "go/internal/query/observability_coverage.go",
		HandlerStruct:    "ObservabilityCoverageHandler",
		StructFile:       "go/internal/query/observability_coverage.go",
		Method:           "listCorrelations",
		MethodFile:       "go/internal/query/observability_coverage.go",
		ScanFiles: []string{
			"go/internal/query/observability_coverage.go",
			"go/internal/query/observability_coverage_correlations.go",
		},
		Served: []routeServedDomain{{
			Domain:     "observability_coverage_correlation",
			StoreField: "Correlations",
			StoreType:  "ObservabilityCoverageCorrelationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/observability_coverage_correlations.go", Marker: "reducer_observability_coverage_correlation"},
			},
		}},
	},

	// ImageHandler.listImages is a graph read anchored on
	// MATCH (img:ContainerImage) (go/internal/query/images.go:30-49), the
	// container_image_identity projection's node label
	// (projector/canonical.go:251).
}
