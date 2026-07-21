// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// Infra relationship read route and relationship-type filtering for
// POST /api/v0/infra/relationships (the MCP analyze_infra_relationships tool
// dispatches here). The MCP tool exposes a small set of semantic query_type
// values; the dispatch forwards them unchanged as relationship_type. This file
// maps both the semantic aliases and raw canonical edge-type names onto the
// concrete relationship types that exist in the graph so the handler can bound
// its Cypher to the requested edges instead of returning every relationship
// regardless of the argument (#3492).

// getRelationships returns the relationships for a given entity, optionally
// filtered to a single relationship kind.
// POST /api/v0/infra/relationships
// Body: {"entity_id": "...", "relationship_type": "what_deploys"}
//
// relationship_type is optional. When omitted the handler returns every
// relationship in both directions (the pre-#3492 behavior). When set it must be
// a recognized analyze_infra_relationships query_type alias (what_deploys,
// what_provisions, who_consumes_xrd, module_consumers, what_runs_image) or a
// canonical edge type (e.g. DEPLOYS_FROM); the read is then bounded to the
// matching edge types.
// An unrecognized value is rejected with 400 rather than silently ignored.
func (h *InfraHandler) getRelationships(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryInfraRelationships,
		"POST /api/v0/infra/relationships",
		"platform_impact.deployment_chain",
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"infrastructure relationship analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			requiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req struct {
		EntityID         string `json:"entity_id"`
		RelationshipType string `json:"relationship_type"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.EntityID == "" {
		WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	relationshipTypes, ok := resolveInfraRelationshipTypes(req.RelationshipType)
	if !ok {
		WriteError(w, http.StatusBadRequest, "unknown relationship_type: "+req.RelationshipType)
		return
	}
	span.SetAttributes(attribute.String("eshu.relationship_filter", infraRelationshipFilterLabel(relationshipTypes)))

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	typeFilter := infraRelationshipTypeClause(relationshipTypes)
	cypher := `
		MATCH (n) WHERE n.id = $entity_id` + infraRelationshipAnchorClause(access) + `
		OPTIONAL MATCH (n)-[r` + typeFilter + `]->(target)` + infraRelationshipNeighborClause(access, "target") + `
		OPTIONAL MATCH (source)-[r2` + typeFilter + `]->(n)` + infraRelationshipNeighborClause(access, "source") + `
		RETURN n.id as id, n.name as name, labels(n) as labels,
		       collect(DISTINCT {
		           direction: 'outgoing',
		           type: type(r),
		           target_name: target.name,
		           target_id: target.id,
		           target_labels: labels(target)
		       }) as outgoing,
		       collect(DISTINCT {
		           direction: 'incoming',
		           type: type(r2),
		           source_name: source.name,
		           source_id: source.id,
		           source_labels: labels(source)
		       }) as incoming
	`

	params := map[string]any{
		"entity_id": req.EntityID,
	}
	access.graphParams(params)

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	// Extract relationships, filtering out null entries
	outgoing := filterNullRelationships(row["outgoing"])
	incoming := filterNullRelationships(row["incoming"])

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"id":       StringVal(row, "id"),
		"name":     StringVal(row, "name"),
		"labels":   StringSliceVal(row, "labels"),
		"outgoing": outgoing,
		"incoming": incoming,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from infrastructure relationship graph"))
}

// filterNullRelationships removes entries where type is nil (from OPTIONAL MATCH with no matches).
func filterNullRelationships(v any) []map[string]any {
	switch slice := v.(type) {
	case []map[string]any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if item["type"] == nil {
				continue
			}
			result = append(result, item)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Skip entries where type is nil (no relationship matched)
			if m["type"] == nil {
				continue
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}

// infraRelationshipTypeAliases maps each semantic analyze_infra_relationships
// query_type value to the canonical graph edge type(s) it selects. The targets
// are real edge types written by go/internal/storage/cypher; no alias invents a
// relationship that the graph does not contain.
//
// what_deploys covers the full deployment topology the pre-#3492 untyped read
// surfaced for deployment, not just DEPLOYS_FROM (#3507):
//   - DEPLOYS_FROM: Repository->Repository / artifact deployment evidence
//     (entity_map_response.go entityMapIsDeployedBy).
//   - DEPLOYMENT_SOURCE: WorkloadInstance->Repository runtime deployment source
//     (canonical.go canonicalDeploymentSourceUpsertCypher, read by
//     fetchDeploymentSourcesFromGraph). Narrowing what_deploys to DEPLOYS_FROM
//     alone dropped this edge, so the tool could report an empty deployment
//     relationship for a workload-instance target even when the graph holds the
//     deployment-source edge.
//   - HAS_DEPLOYMENT_EVIDENCE: Repository->EvidenceArtifact deployment evidence
//     (canonical_relationships.go), grouped with DEPLOYS_FROM as the deploy
//     family by entity_map_response.go entityMapIsDeployedBy.
//
// who_consumes_xrd resolves to USES_MODULE because Crossplane XRD/composition
// consumption is modeled today as a module-reference edge; there is no distinct
// XRD-consumption edge type in the canonical graph. If one is added later, add
// it here without changing the wire contract.
//
// what_runs_image resolves to RUNS_IMAGE, the live-workload image edge
// (KubernetesWorkload)-[:RUNS_IMAGE]->(OciImageManifest|OciImageIndex|
// OciImageDescriptor) written by kubernetes_correlation_edge_writer.go (#388).
// The edge existed only as a graph write with no declared query/MCP read path
// before #5436; this alias gives it one through the existing bidirectional
// getRelationships pattern (anchor on the workload to see the image, or anchor
// on the image to see the workloads running it).
var infraRelationshipTypeAliases = map[string][]string{
	"what_deploys":     {"DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE"},
	"what_provisions":  {"PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"},
	"module_consumers": {"USES_MODULE"},
	"who_consumes_xrd": {"USES_MODULE"},
	"what_runs_image":  {"RUNS_IMAGE"},
}

// infraCanonicalEdgeTypes is the set of canonical edge types a caller may pass
// directly as relationship_type (the HTTP route and dispatch both forward the
// raw value). Accepting canonical names keeps the route usable from the HTTP API
// and the relationships catalog without forcing every caller through the MCP
// semantic aliases.
var infraCanonicalEdgeTypes = map[string]struct{}{
	"DEPLOYS_FROM":              {},
	"DEPLOYMENT_SOURCE":         {},
	"HAS_DEPLOYMENT_EVIDENCE":   {},
	"PROVISIONS_DEPENDENCY_FOR": {},
	"PROVISIONS_PLATFORM":       {},
	"USES_MODULE":               {},
	"DEPENDS_ON":                {},
	"INSTANCE_OF":               {},
	"RUNS_ON":                   {},
	"RUNS_IMAGE":                {},
	"DISCOVERS_CONFIG_IN":       {},
	"READS_CONFIG_FROM":         {},
	"DEFINES":                   {},
}

// resolveInfraRelationshipTypes maps a relationship_type argument to the
// canonical edge types the relationship query should filter on. An empty
// argument returns (nil, true): no filter, the whole-relationship behavior the
// route had before #3492, preserved for backward compatibility. A recognized
// semantic alias or canonical edge type returns its concrete edge-type slice and
// true. An unrecognized non-empty value returns (nil, false) so the handler can
// reject it with a 400 rather than silently dropping the argument.
func resolveInfraRelationshipTypes(relationshipType string) ([]string, bool) {
	trimmed := strings.TrimSpace(relationshipType)
	if trimmed == "" {
		return nil, true
	}
	if types, ok := infraRelationshipTypeAliases[strings.ToLower(trimmed)]; ok {
		out := make([]string, len(types))
		copy(out, types)
		return out, true
	}
	canonical := strings.ToUpper(trimmed)
	if _, ok := infraCanonicalEdgeTypes[canonical]; ok {
		return []string{canonical}, true
	}
	return nil, false
}

// infraRelationshipTypeClause renders the inline relationship-type predicate for
// the OPTIONAL MATCH variable-length-free pattern, e.g. ":DEPLOYS_FROM|USES_MODULE".
// It returns the empty string when no filter is requested so the unfiltered
// Cypher is byte-identical to the pre-#3492 query. Every edge type comes from
// resolveInfraRelationshipTypes' fixed allowlist, so the inline names are
// injection-safe and the relationship-type index serves the pattern directly.
func infraRelationshipTypeClause(relationshipTypes []string) string {
	if len(relationshipTypes) == 0 {
		return ""
	}
	return ":" + strings.Join(relationshipTypes, "|")
}

// infraRelationshipFilterLabel returns a low-cardinality span attribute value
// describing the resolved relationship filter: "all" when unfiltered, otherwise
// the pipe-joined edge types the read was bounded to.
func infraRelationshipFilterLabel(relationshipTypes []string) string {
	if len(relationshipTypes) == 0 {
		return "all"
	}
	return strings.Join(relationshipTypes, "|")
}
