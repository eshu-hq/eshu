package query

import "net/http"

// findChangeSurface analyzes the legacy entity-anchored change surface.
// Prefer investigateChangeSurface for prompt-facing code-topic and path flows.
func (h *ImpactHandler) findChangeSurface(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.change_surface") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"change surface analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.change_surface",
			h.profile(),
			requiredProfile("platform_impact.change_surface"),
		)
		return
	}

	var req struct {
		Target      string `json:"target"`
		Environment string `json:"environment"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}

	limit := normalizeImpactListLimit(req.Limit)
	cypher := `MATCH (start) WHERE start.id = $target_id
		OPTIONAL MATCH path = (start)-[rels*1..8]->(impacted)
		WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN ['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset'])
			AND ($environment = '' OR coalesce(impacted.environment, '') = '' OR impacted.environment = $environment)
		UNWIND relationships(path) as rel
		WITH impacted, rel, startNode(rel) as hop_from, endNode(rel) as hop_to, length(path) as depth
		RETURN DISTINCT impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
			type(rel) as rel_type, rel.confidence as confidence, rel.reason as reason, depth
		ORDER BY depth, impacted.name, impacted.id
		LIMIT $limit`

	rows, err := h.Neo4j.Run(r.Context(), cypher, map[string]any{
		"target_id":   req.Target,
		"environment": req.Environment,
		"limit":       limit + 1,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	targetRow, err := h.Neo4j.RunSingle(r.Context(), "MATCH (n) WHERE n.id = $id RETURN n.id as id, n.name as name", map[string]any{"id": req.Target})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	target := map[string]any{"id": req.Target, "name": StringVal(targetRow, "name")}
	rows, truncated := trimImpactRows(rows, limit)
	impacted := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		env := StringVal(row, "environment")
		if req.Environment != "" && env != "" && env != req.Environment {
			continue
		}
		entry := map[string]any{"id": StringVal(row, "id"), "name": StringVal(row, "name"), "labels": StringSliceVal(row, "labels"), "depth": IntVal(row, "depth")}
		if env != "" {
			entry["environment"] = env
		}
		if conf, ok := row["confidence"].(float64); ok {
			entry["confidence"] = conf
		}
		if reason := StringVal(row, "reason"); reason != "" {
			entry["reason"] = reason
		}
		impacted = append(impacted, entry)
	}
	resp := map[string]any{"target": target, "impacted": impacted, "count": len(impacted), "limit": limit, "truncated": truncated}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.change_surface", TruthBasisHybrid, "resolved from graph and impact relationships"))
}
