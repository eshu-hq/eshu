package query

import (
	"fmt"
	"log/slog"
	"net/http"
)

// ImpactHandler serves HTTP endpoints for impact analysis queries including
// blast radius, change surface, resource-to-code tracing, and dependency paths.
type ImpactHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
	Logger  *slog.Logger
}

// Mount registers impact analysis routes on the given mux.
func (h *ImpactHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/impact/trace-deployment-chain", h.traceDeploymentChain)
	mux.HandleFunc("POST /api/v0/impact/deployment-config-influence", h.investigateDeploymentConfigInfluence)
	mux.HandleFunc("POST /api/v0/impact/blast-radius", h.findBlastRadius)
	mux.HandleFunc("POST /api/v0/impact/change-surface", h.findChangeSurface)
	mux.HandleFunc("POST /api/v0/impact/change-surface/investigate", h.investigateChangeSurface)
	mux.HandleFunc("POST /api/v0/impact/resource-investigation", h.investigateResource)
	mux.HandleFunc("POST /api/v0/impact/trace-resource-to-code", h.traceResourceToCode)
	mux.HandleFunc("POST /api/v0/impact/explain-dependency-path", h.explainDependencyPath)
}

func (h *ImpactHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// findBlastRadius analyzes the blast radius for a target entity.
// POST /api/v0/impact/blast-radius
// Body: {"target": "repo-name", "target_type": "repository"}
func (h *ImpactHandler) findBlastRadius(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.blast_radius") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"blast radius analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.blast_radius",
			h.profile(),
			requiredProfile("platform_impact.blast_radius"),
		)
		return
	}

	var req struct {
		Target     string `json:"target"`
		TargetType string `json:"target_type"`
		Limit      int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}
	if req.TargetType == "" {
		WriteError(w, http.StatusBadRequest, "target_type is required")
		return
	}

	limit := normalizeImpactListLimit(req.Limit)
	var cypher string
	params := map[string]any{"target_name": req.Target, "limit": limit + 1}

	switch req.TargetType {
	case "repository":
		cypher = `MATCH (source:Repository) WHERE source.name CONTAINS $target_name
			OPTIONAL MATCH path = (source)<-[rels*1..5]-(affected:Repository)
			WHERE all(rel IN rels WHERE type(rel) = 'DEPENDS_ON')
			OPTIONAL MATCH (affected)<-[:CONTAINS]-(tier:Tier)
			RETURN DISTINCT affected.name as repo, tier.name as tier, tier.risk_level as risk, length(path) as hops
			ORDER BY hops, repo
			LIMIT $limit`
	case "terraform_module":
		cypher = `MATCH (mod:TerraformModule) WHERE mod.name CONTAINS $target_name OR mod.source CONTAINS $target_name
			MATCH (f:File)-[:CONTAINS]->(mod) MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
			OPTIONAL MATCH path = (repo)<-[rels*0..5]-(affected:Repository) WHERE all(rel IN rels WHERE type(rel) = 'DEPENDS_ON')
			OPTIONAL MATCH (affected)<-[:CONTAINS]-(tier:Tier)
			RETURN DISTINCT affected.name as repo, tier.name as tier, tier.risk_level as risk
			ORDER BY repo
			LIMIT $limit`
	case "crossplane_xrd":
		cypher = `MATCH (xrd:CrossplaneXRD) WHERE xrd.kind CONTAINS $target_name OR xrd.name CONTAINS $target_name
			OPTIONAL MATCH (claim:CrossplaneClaim)-[:SATISFIED_BY]->(xrd)
			MATCH (f:File)-[:CONTAINS]->(claim) MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
			OPTIONAL MATCH (affected)<-[:CONTAINS]-(tier:Tier)
			RETURN DISTINCT repo.name as repo, tier.name as tier, claim.name as claim
			ORDER BY repo, claim
			LIMIT $limit`
	case "sql_table":
		cypher = `CALL { MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(table) RETURN DISTINCT repo, 0 as hops UNION
				MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:MIGRATES]->(table) RETURN DISTINCT repo, 1 as hops UNION
				MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:Class)-[:MAPS_TO_TABLE]->(table) RETURN DISTINCT repo, 1 as hops UNION
				MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:Function)-[:QUERIES_TABLE]->(table) RETURN DISTINCT repo, 1 as hops UNION
				MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:SqlTable)-[:REFERENCES_TABLE]->(table) RETURN DISTINCT repo, 1 as hops UNION
				MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
				MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(sql_node)
				WHERE (sql_node:SqlView OR sql_node:SqlFunction OR sql_node:SqlTrigger OR sql_node:SqlIndex)
				  AND EXISTS { MATCH (sql_node)-[:READS_FROM|TRIGGERS_ON|INDEXES]->(table) }
				RETURN DISTINCT repo, 1 as hops }
			OPTIONAL MATCH (repo)<-[:CONTAINS]-(tier:Tier)
			RETURN DISTINCT repo.name as repo, repo.id as repo_id, tier.name as tier, tier.risk_level as risk, hops
			ORDER BY hops, repo
			LIMIT $limit`
	default:
		WriteError(w, http.StatusBadRequest, "unsupported target_type: "+req.TargetType)
		return
	}

	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows, truncated := trimImpactRows(rows, limit)
	affected := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{"repo": StringVal(row, "repo")}
		if v := StringVal(row, "tier"); v != "" {
			entry["tier"] = v
		}
		if v := StringVal(row, "risk"); v != "" {
			entry["risk"] = v
		}
		if v := IntVal(row, "hops"); v > 0 {
			entry["hops"] = v
		}
		if v := StringVal(row, "repo_id"); v != "" {
			entry["repo_id"] = v
		}
		if v := StringVal(row, "claim"); v != "" {
			entry["claim"] = v
		}
		affected = append(affected, entry)
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"target":         req.Target,
		"target_type":    req.TargetType,
		"affected":       affected,
		"affected_count": len(affected),
		"limit":          limit,
		"truncated":      truncated,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.blast_radius", TruthBasisHybrid, "resolved from platform graph impact analysis"))
}

// traceResourceToCode traces a resource back to its code repository.
// POST /api/v0/impact/trace-resource-to-code
// Body: {"start": "entity-id", "environment": "production", "max_depth": 8}
func (h *ImpactHandler) traceResourceToCode(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.resource_to_code") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"resource-to-code tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.resource_to_code",
			h.profile(),
			requiredProfile("platform_impact.resource_to_code"),
		)
		return
	}

	var req struct {
		Start       string `json:"start"`
		Environment string `json:"environment"`
		MaxDepth    int    `json:"max_depth"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Start == "" {
		WriteError(w, http.StatusBadRequest, "start is required")
		return
	}

	// Default and clamp max_depth
	if req.MaxDepth <= 0 {
		req.MaxDepth = 8
	}
	if req.MaxDepth > 20 {
		req.MaxDepth = 20
	}
	if req.MaxDepth < 1 {
		req.MaxDepth = 1
	}
	limit := normalizeImpactListLimit(req.Limit)

	cypher := fmt.Sprintf(`MATCH (start) WHERE start.id = $start_id
		OPTIONAL MATCH path = (start)-[rels*1..%d]->(repo:Repository)
		WITH start, path, repo, length(path) as depth, [rel IN relationships(path) | {type: type(rel), confidence: rel.confidence, reason: rel.reason}] as hops
		RETURN DISTINCT start.id as start_id, start.name as start_name, labels(start) as start_labels, repo.id as repo_id, repo.name as repo_name, depth, hops
		ORDER BY depth, repo_name, repo_id
		LIMIT $limit`, req.MaxDepth)

	params := map[string]any{"start_id": req.Start, "limit": limit + 1}
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var start map[string]any
	if len(rows) > 0 {
		start = map[string]any{"id": StringVal(rows[0], "start_id"), "name": StringVal(rows[0], "start_name"), "labels": StringSliceVal(rows[0], "start_labels")}
	} else {
		startRow, err := h.Neo4j.RunSingle(r.Context(), "MATCH (n) WHERE n.id = $id RETURN n.id as id, n.name as name, labels(n) as labels", map[string]any{"id": req.Start})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		start = map[string]any{"id": StringVal(startRow, "id"), "name": StringVal(startRow, "name"), "labels": StringSliceVal(startRow, "labels")}
	}
	rows, truncated := trimImpactRows(rows, limit)
	paths := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repoID := StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		path := map[string]any{"repo_id": repoID, "repo_name": StringVal(row, "repo_name"), "depth": IntVal(row, "depth")}
		if hopsRaw := row["hops"]; hopsRaw != nil {
			if hopsSlice, ok := hopsRaw.([]any); ok {
				hops := make([]map[string]any, 0, len(hopsSlice))
				for _, hopRaw := range hopsSlice {
					if hopMap, ok := hopRaw.(map[string]any); ok {
						hop := map[string]any{"type": StringVal(hopMap, "type")}
						if conf, ok := hopMap["confidence"].(float64); ok {
							hop["confidence"] = conf
						}
						if reason := StringVal(hopMap, "reason"); reason != "" {
							hop["reason"] = reason
						}
						hops = append(hops, hop)
					}
				}
				path["hops"] = hops
			}
		}
		paths = append(paths, path)
	}
	resp := map[string]any{"start": start, "paths": paths, "count": len(paths), "limit": limit, "truncated": truncated}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.resource_to_code", TruthBasisHybrid, "resolved from resource-to-code graph traversal"))
}

// explainDependencyPath finds and explains the shortest path between two entities.
// POST /api/v0/impact/explain-dependency-path
// Body: {"source": "entity-id", "target": "entity-id", "environment": "production"}
func (h *ImpactHandler) explainDependencyPath(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.dependency_path") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dependency path analysis requires full dependency graph truth",
			"unsupported_capability",
			"platform_impact.dependency_path",
			h.profile(),
			requiredProfile("platform_impact.dependency_path"),
		)
		return
	}

	var req struct {
		Source      string `json:"source"`
		Target      string `json:"target"`
		Environment string `json:"environment"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Source == "" {
		WriteError(w, http.StatusBadRequest, "source is required")
		return
	}
	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}

	cypher := `MATCH (source) WHERE source.id = $source_id
		MATCH (target) WHERE target.id = $target_id
		OPTIONAL MATCH path = shortestPath((source)-[*1..8]-(target))
		WITH source, target, path, CASE WHEN path IS NOT NULL THEN [rel IN relationships(path) | {from_id: startNode(rel).id, from_name: startNode(rel).name, to_id: endNode(rel).id, to_name: endNode(rel).name, type: type(rel), confidence: rel.confidence, reason: rel.reason}] ELSE null END as hops
		RETURN source.id as source_id, source.name as source_name, labels(source) as source_labels, target.id as target_id, target.name as target_name, labels(target) as target_labels, CASE WHEN path IS NOT NULL THEN length(path) ELSE -1 END as depth, hops`

	params := map[string]any{
		"source_id": req.Source,
		"target_id": req.Target,
	}

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if row == nil {
		WriteError(w, http.StatusNotFound, "source or target not found")
		return
	}

	source := map[string]any{
		"id":     StringVal(row, "source_id"),
		"name":   StringVal(row, "source_name"),
		"labels": StringSliceVal(row, "source_labels"),
	}

	target := map[string]any{
		"id":     StringVal(row, "target_id"),
		"name":   StringVal(row, "target_name"),
		"labels": StringSliceVal(row, "target_labels"),
	}

	depth := IntVal(row, "depth")
	var pathInfo map[string]any
	var overallConfidence float64
	var overallReason string

	if depth >= 0 {
		pathInfo = map[string]any{"depth": depth}

		// Extract hops
		if hopsRaw := row["hops"]; hopsRaw != nil {
			if hopsSlice, ok := hopsRaw.([]any); ok {
				hops := make([]map[string]any, 0, len(hopsSlice))
				confSum := 0.0
				confCount := 0
				reasons := []string{}

				for _, hopRaw := range hopsSlice {
					if hopMap, ok := hopRaw.(map[string]any); ok {
						hop := map[string]any{
							"from_id":   StringVal(hopMap, "from_id"),
							"from_name": StringVal(hopMap, "from_name"),
							"to_id":     StringVal(hopMap, "to_id"),
							"to_name":   StringVal(hopMap, "to_name"),
							"type":      StringVal(hopMap, "type"),
						}
						if conf, ok := hopMap["confidence"].(float64); ok {
							hop["confidence"] = conf
							confSum += conf
							confCount++
						}
						if reason := StringVal(hopMap, "reason"); reason != "" {
							hop["reason"] = reason
							reasons = append(reasons, reason)
						}
						hops = append(hops, hop)
					}
				}

				pathInfo["hops"] = hops

				// Calculate average confidence
				if confCount > 0 {
					overallConfidence = confSum / float64(confCount)
				}
				// Aggregate reasons
				if len(reasons) > 0 {
					overallReason = reasons[0]
					if len(reasons) > 1 {
						overallReason = fmt.Sprintf("%s (and %d more)", reasons[0], len(reasons)-1)
					}
				}
			}
		}
	}

	resp := map[string]any{
		"source": source,
		"target": target,
	}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	if pathInfo != nil {
		resp["path"] = pathInfo
	}
	if overallConfidence > 0 {
		resp["confidence"] = overallConfidence
	}
	if overallReason != "" {
		resp["reason"] = overallReason
	}

	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.dependency_path", TruthBasisHybrid, "resolved from shortest-path dependency traversal"))
}
