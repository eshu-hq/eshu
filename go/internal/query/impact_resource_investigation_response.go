package query

import "fmt"

func resourceInvestigationResponse(
	req resourceInvestigationRequest,
	resolution map[string]any,
	selected *resourceInvestigationCandidate,
	workloads []map[string]any,
	incomingPaths []map[string]any,
	outgoingPaths []map[string]any,
	truncated bool,
) map[string]any {
	resp := map[string]any{
		"scope": map[string]any{
			"query":         req.Query,
			"resource_id":   req.ResourceID,
			"resource_type": req.ResourceType,
			"environment":   req.Environment,
		},
		"target_resolution":  resolution,
		"workloads":          workloads,
		"workload_count":     len(workloads),
		"provisioning_paths": append(append([]map[string]any{}, incomingPaths...), outgoingPaths...),
		"source_handles":     resourceSourceHandles(selected, incomingPaths, outgoingPaths),
		"recommended_next_calls": resourceInvestigationNextCalls(
			selected,
			workloads,
			incomingPaths,
			outgoingPaths,
		),
		"limitations": resourceInvestigationLimitations(resolution, selected),
		"coverage": map[string]any{
			"query_shape":    resourceInvestigationShape(selected),
			"max_depth":      req.MaxDepth,
			"limit":          req.Limit,
			"truncated":      truncated || BoolVal(resolution, "truncated"),
			"workload_count": len(workloads),
			"path_count":     len(incomingPaths) + len(outgoingPaths),
		},
		"limit":          req.Limit,
		"max_depth":      req.MaxDepth,
		"truncated":      truncated || BoolVal(resolution, "truncated"),
		"source_backend": "graph",
	}
	if selected != nil {
		resp["resource"] = selected.Map()
		resp["story"] = resourceInvestigationStory(selected, workloads, incomingPaths, outgoingPaths)
	}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	return resp
}

func resourceInvestigationCandidates(rows []map[string]any) []resourceInvestigationCandidate {
	candidates := make([]resourceInvestigationCandidate, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, resourceInvestigationCandidate{
			ID:            id,
			Name:          StringVal(row, "name"),
			Labels:        StringSliceVal(row, "labels"),
			ResourceType:  StringVal(row, "resource_type"),
			Provider:      StringVal(row, "provider"),
			Environment:   StringVal(row, "environment"),
			RepoID:        StringVal(row, "repo_id"),
			ConfigPath:    StringVal(row, "config_path"),
			Source:        StringVal(row, "source"),
			ResourceID:    StringVal(row, "resource_id"),
			ResourceKind:  StringVal(row, "resource_kind"),
			ResourceClass: StringVal(row, "resource_class"),
		})
	}
	return candidates
}

func resourceInvestigationCandidateMaps(candidates []resourceInvestigationCandidate) []map[string]any {
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Map())
	}
	return values
}

func (c resourceInvestigationCandidate) Map() map[string]any {
	return compactStringMap(map[string]any{
		"id":             c.ID,
		"name":           c.Name,
		"labels":         c.Labels,
		"resource_type":  c.ResourceType,
		"provider":       c.Provider,
		"environment":    c.Environment,
		"repo_id":        c.RepoID,
		"config_path":    c.ConfigPath,
		"source":         c.Source,
		"resource_id":    c.ResourceID,
		"resource_kind":  c.ResourceKind,
		"resource_class": c.ResourceClass,
	})
}

func compactStringMap(value map[string]any) map[string]any {
	for key, raw := range value {
		switch typed := raw.(type) {
		case string:
			if typed == "" {
				delete(value, key)
			}
		case []string:
			if len(typed) == 0 {
				delete(value, key)
			}
		}
	}
	return value
}

func resourceInvestigationStory(
	resource *resourceInvestigationCandidate,
	workloads []map[string]any,
	incomingPaths []map[string]any,
	outgoingPaths []map[string]any,
) string {
	name := resource.Name
	if name == "" {
		name = resource.ID
	}
	return fmt.Sprintf(
		"%s resolves to %s and has %d workload usage rows plus %d repository provenance paths.",
		name,
		resource.ID,
		len(workloads),
		len(incomingPaths)+len(outgoingPaths),
	)
}

func resourceSourceHandles(
	selected *resourceInvestigationCandidate,
	incomingPaths []map[string]any,
	outgoingPaths []map[string]any,
) []map[string]any {
	handles := []map[string]any{}
	if selected != nil && selected.RepoID != "" && selected.ConfigPath != "" {
		handles = append(handles, map[string]any{"repo_id": selected.RepoID, "relative_path": selected.ConfigPath, "reason": "resource_config_path"})
	}
	for _, path := range append(append([]map[string]any{}, incomingPaths...), outgoingPaths...) {
		if repoID := StringVal(path, "repo_id"); repoID != "" {
			handles = append(handles, map[string]any{"repo_id": repoID, "reason": "repository_path"})
		}
	}
	return handles
}

func resourceInvestigationNextCalls(
	selected *resourceInvestigationCandidate,
	workloads []map[string]any,
	incomingPaths []map[string]any,
	outgoingPaths []map[string]any,
) []map[string]any {
	calls := []map[string]any{}
	if selected != nil {
		calls = append(calls, map[string]any{"tool": "trace_resource_to_code", "arguments": map[string]any{"start": selected.ID}})
	}
	for _, workload := range workloads {
		if name := StringVal(workload, "workload_name"); name != "" {
			calls = append(calls, map[string]any{"tool": "trace_deployment_chain", "arguments": map[string]any{"service_name": name}})
			break
		}
	}
	for _, path := range append(append([]map[string]any{}, incomingPaths...), outgoingPaths...) {
		if repoID := StringVal(path, "repo_id"); repoID != "" {
			calls = append(calls, map[string]any{"tool": "get_repo_context", "arguments": map[string]any{"repo_id": repoID}})
			break
		}
	}
	return calls
}

func resourceInvestigationLimitations(
	resolution map[string]any,
	selected *resourceInvestigationCandidate,
) []string {
	status := StringVal(resolution, "status")
	switch {
	case status == "ambiguous":
		return []string{"resource name matched multiple graph nodes; rerun with resource_id from candidates"}
	case status == "no_match":
		return []string{"resource was not found in the authoritative graph"}
	case selected != nil:
		return []string{"repository paths are graph provenance handles; read source files for exact line citations"}
	default:
		return nil
	}
}

func resourceInvestigationShape(selected *resourceInvestigationCandidate) string {
	if selected == nil {
		return "resource_resolution_only"
	}
	return "resolved_resource_investigation"
}
