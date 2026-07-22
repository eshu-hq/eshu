// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// This file is the #5584 route-serves-data registry: the committed,
// reviewable derivation that removes routeServesDataBackingMap's
// self-certification gap (PR #5583 round-3 P1b, codex). The backing map used
// to be pure hand-maintained data — nothing cross-checked a ServedDomains
// claim against the real Go handler registered for the route, so poisoning
// the map could force a false pass. The registry here is different in kind:
// every route entry names the real registration line, handler struct, and
// method, and every served-domain claim carries falsifiable source evidence
// (a verbatim marker that must appear in a cited file, or a store field that
// must exist on the handler struct AND be referenced by the method body).
// route_serves_data_registry_check.go verifies all of it against the actual
// source tree, and the gate in route_serves_data_registry_test.go asserts
// the backing map equals the registry's verified claims exactly.
//
// Per-route rationale, producer-side citations, and the flagged
// architect-review items live in
// docs/internal/design/5584-route-serves-data-registry.md.

// domainDataSignature holds the discriminative source markers for one
// reducer domain: the strings that appear in a route's read path exactly
// when that route reads this domain's data.
//
//   - Markers are verbatim substrings of real query text: SQL fact_kind
//     literals (e.g. "reducer_kubernetes_correlation"), Cypher node-label
//     anchors (e.g. ":ContainerImage"), or the facts.<Kind>FactKind
//     identifier when the query builds its SQL from the Go constant instead
//     of an inline literal (e.g. "facts.DocumentationSourceFactKind").
//   - StoreTypes are the query-layer store interface types whose
//     implementations read this domain's rows; a route "uses" one only when
//     the handler struct declares a field of that type AND the registered
//     method body references that field (h.<Field>), so a field that backs a
//     sibling route on the same struct does not count.
//
// The anti-poison scan (verifyRouteServesDataScan) asserts that no route's
// read path contains a foreign domain's signature unless the pair is
// declared in Served or Disclosed — so a domain's data cannot silently leak
// into a route the backing map does not admit, and a backing-map edit alone
// can never make a false pairing pass.
type domainDataSignature struct {
	Markers    []string
	StoreTypes []string
}

// domainDataSignatures maps every reducer_domain in
// routeServesDataBackingMap to its discriminative signature. Closed set:
// the gate fails if the backing map or the route registry names a domain
// missing here, and if an entry here is used by no route.
//
// Signatures are deliberately MINIMAL (what the domain's declared read
// surface actually reads), not an exhaustive data-flow inventory: a
// signature exists to catch misrouting, and a too-broad signature (e.g.
// giving aws_cloud_runtime_drift the ":CloudResource" label its base node
// writer also MERGEs) would drown the scan in known-shared-surface noise.
// Known shared surfaces that are real but not signature-encoded are
// documented in docs/internal/design/5584-route-serves-data-registry.md.
//
// Grouped-signature tradeoff (#5584 review P2): three co-declared groups
// share one signature — {aws, azure, gcp} cloud inventory (all read
// reducer_cloud_resource_identity), {ec2, rds, s3_internet} cloud
// resources (all materialize onto :CloudResource), and {reducer_derived_findings,
// supply_chain_impact} (one shared finding kind). Within a group the scan
// cannot tell members apart, so a misroute that swaps one group member for
// another is NOT detectable by signature — the tradeoff fails toward
// under-detection inside the group, never toward a false pass for a domain
// outside it. All group members are co-served on one route today, so the
// intra-group distinction has no route boundary to cross.
var domainDataSignatures = map[string]domainDataSignature{
	// documentation_read_model.go builds its IN (...) list from the
	// facts.Documentation*FactKind constants, not inline literals.
	"documentation_materialization": {Markers: []string{"facts.DocumentationSourceFactKind", "facts.DocumentationDocumentFactKind"}},

	// The three provider inventory domains converge into ONE reducer-owned
	// canonical kind: projector/cloud_inventory_admission_intents.go admits
	// exactly {aws_resource, gcp_cloud_resource, azure_cloud_resource} and
	// reducer/cloud_inventory_admission_writer.go persists
	// reducer_cloud_resource_identity, which is what the /cloud/inventory
	// readback queries. All three domains therefore share the derived-kind
	// signature.
	"aws_cloud_runtime_drift":        {Markers: []string{"reducer_cloud_resource_identity"}},
	"azure_resource_materialization": {Markers: []string{"reducer_cloud_resource_identity"}},
	"gcp_resource_materialization":   {Markers: []string{"reducer_cloud_resource_identity"}},

	"ci_cd_run_correlation": {Markers: []string{"reducer_ci_cd_run_correlation"}, StoreTypes: []string{"CICDRunCorrelationStore"}},

	// code_graph_projection's read surface is the Repository graph label.
	"code_graph_projection": {Markers: []string{":Repository"}},

	// Both domains surface through the one reducer-derived finding kind:
	// reducer_derived owns the kind, supply_chain_impact is the producing
	// projection (specs/fact-kind-registry.v1.yaml:131-142, 346-356).
	"reducer_derived_findings": {Markers: []string{"reducer_supply_chain_impact_finding"}, StoreTypes: []string{"SupplyChainImpactFindingStore"}},
	"supply_chain_impact":      {Markers: []string{"reducer_supply_chain_impact_finding"}, StoreTypes: []string{"SupplyChainImpactFindingStore"}},

	// The three /cloud/resources domains all materialize onto the
	// CloudResource label: ec2 MERGEs nodes
	// (storage/cypher/ec2_instance_node_writer.go:33), rds and s3 decorate
	// existing nodes with posture properties
	// (rds_posture_node_writer.go:17, s3_internet_exposure_node_writer.go:16).
	"ec2_instance_node_materialization":    {Markers: []string{":CloudResource"}},
	"rds_posture_materialization":          {Markers: []string{":CloudResource"}},
	"s3_internet_exposure_materialization": {Markers: []string{":CloudResource"}},

	// incident_repository_correlation is shared by two registry families:
	// incident_context (incident.record / incident.lifecycle_event /
	// change.record read by /incidents/{id}/context) and work_item
	// (work_item.* kinds read by /work-items/evidence via
	// facts.WorkItemFactKinds()).
	"incident_repository_correlation":  {Markers: []string{"'incident.record'", "'incident.lifecycle_event'", "'change.record'", "facts.WorkItemFactKinds()"}, StoreTypes: []string{"IncidentContextStore", "WorkItemEvidenceStore"}},
	"incident_routing_materialization": {Markers: []string{"incident_routing.applied_pagerduty_resource", "incident_routing.observed_pagerduty_service"}},

	"kubernetes_correlation":             {Markers: []string{"reducer_kubernetes_correlation"}, StoreTypes: []string{"KubernetesCorrelationStore"}},
	"observability_coverage_correlation": {Markers: []string{"reducer_observability_coverage_correlation"}, StoreTypes: []string{"ObservabilityCoverageCorrelationStore"}},
	"container_image_identity":           {Markers: []string{":ContainerImage", "reducer_container_image_identity"}},
	"package_source_correlation":         {Markers: []string{":Package"}, StoreTypes: []string{"PackageRegistryCorrelationStore"}},

	// s3_external_principal_grant_materialization writes
	// (:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal) graph truth
	// (storage/cypher/s3_external_principal_grant_writer.go) that NO
	// read-surface route queries today — see the MapOnly disclosure on
	// "GET /api/v0/secrets-iam/posture-summary".
	"s3_external_principal_grant_materialization": {Markers: []string{":ExternalPrincipal", "s3_external_principal_grant"}},

	"secrets_iam_trust_chain":       {Markers: []string{"reducer_secrets_iam_identity_trust_chain", "reducer_secrets_iam_posture_gap"}, StoreTypes: []string{"SecretsIAMPostureSummaryStore"}},
	"sbom_attestation_attachment":   {Markers: []string{"reducer_sbom_attestation_attachment"}, StoreTypes: []string{"SBOMAttestationAttachmentStore"}},
	"security_alert_reconciliation": {Markers: []string{"reducer_security_alert_reconciliation"}, StoreTypes: []string{"SecurityAlertReconciliationStore"}},

	// The semantic read model builds SQL from the Go constant identifier.
	"semantic_entity_materialization": {Markers: []string{"facts.SemanticDocumentationObservationFactKind"}},

	"service_catalog_correlation": {Markers: []string{"reducer_service_catalog_correlation"}, StoreTypes: []string{"ServiceCatalogCorrelationStore"}},

	// codeowners_ownership projects (:Repository)-[:DECLARES_CODEOWNER]->
	// (:CodeownerTeam) graph truth (storage/cypher/canonical_codeowners_edges.go:34-35).
	"codeowners_ownership": {Markers: []string{"DECLARES_CODEOWNER"}},

	// config_state_drift materializes tfstate truth onto its OWN state
	// labels and properties (storage/cypher/tfstate_canonical_writer.go):
	// TerraformStateResource nodes, MATCHES_STATE config↔state edges, and
	// tf_attr_* promoted attributes. The signature deliberately names only
	// those state-specific markers — NOT the TerraformModule label the
	// writer also MERGEs, because config-side content reads share that
	// label and a shared marker cannot discriminate (PR #5641 codex P1:
	// the shared label falsely certified /iac/resources as serving this
	// domain).
	"config_state_drift": {Markers: []string{":TerraformStateResource", "MATCHES_STATE", "tf_attr_"}},
}

// routeReadEvidence is one falsifiable citation: Marker must appear verbatim
// in File (repo-relative). A claim whose marker disappears — the query was
// rewritten, the store was replaced — turns the gate RED and forces the
// registry entry to be re-derived from the new source.
type routeReadEvidence struct {
	File   string
	Marker string
}

// routeServedDomain is one verified served-domain claim for a route.
type routeServedDomain struct {
	Domain string
	// StoreField/StoreType, when set, assert the handler struct declares a
	// field with this exact name whose type contains StoreType, and that the
	// registered method body references "h.<StoreField>".
	StoreField string
	StoreType  string
	// Evidence markers proving the read path actually touches this domain's
	// data (query text, label anchors, producer writers).
	Evidence []routeReadEvidence
}

// routeDisclosure is a verified, reviewed (route, domain) touch that is
// deliberately NOT a served-domain claim: enrichment reads, anchor labels,
// and evidence side-channels. Each must cite live evidence (a stale
// disclosure fails the gate) and each exempts the pair from the anti-poison
// scan. Reason is the human rationale a reviewer signs off on.
type routeDisclosure struct {
	Domain   string
	Reason   string
	Evidence []routeReadEvidence
}

// routeMapOnlyClaim is a backing-map ServedDomains entry that has NO
// detectable read-path evidence today. It keeps the shipped map row green
// while making the gap explicit and contradiction-checked: the anti-poison
// scan still runs for the domain, so if evidence ever appears the claim must
// move to Served, and if the map row is removed the claim goes stale and
// fails. These are the #5584 architect-review items.
type routeMapOnlyClaim struct {
	Domain string
	Reason string
}

// routeServesDataRegistry is the merged #5584 route registry. The entries
// live in route_serves_data_registry_routes.go (part 1) and
// route_serves_data_registry_routes_2.go (part 2), split only for the
// 500-line file cap; a duplicate route across the parts is a programmer
// error and panics at init so it can never silently shadow an entry.
var routeServesDataRegistry = func() map[string]routeServesDataSource {
	merged := make(map[string]routeServesDataSource, len(routeServesDataRegistryPart1)+len(routeServesDataRegistryPart2))
	for _, part := range []map[string]routeServesDataSource{routeServesDataRegistryPart1, routeServesDataRegistryPart2} {
		for route, entry := range part {
			if _, dup := merged[route]; dup {
				panic("routeServesDataRegistry: duplicate route entry " + route)
			}
			merged[route] = entry
		}
	}
	return merged
}()

// routeServesDataSource ties one read_surface route literal to its real
// handler wiring and its verified served-domain derivation.
type routeServesDataSource struct {
	// RegistrationFile must contain the literal
	// `mux.HandleFunc("<route>", h.<Method>)` registration.
	RegistrationFile string
	// HandlerStruct is declared in StructFile; Method's body (receiver
	// *HandlerStruct) lives in MethodFile.
	HandlerStruct string
	StructFile    string
	Method        string
	MethodFile    string
	// ScanFiles is the route's read-path surface for the anti-poison scan:
	// the method file plus the query/store files carrying its query text.
	ScanFiles []string
	Served    []routeServedDomain
	Disclosed []routeDisclosure
	MapOnly   []routeMapOnlyClaim
}
