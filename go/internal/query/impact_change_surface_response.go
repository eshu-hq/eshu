// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

func changeSurfaceNoTargetResolution(req changeSurfaceInvestigationRequest) map[string]any {
	return map[string]any{
		"status":      "not_requested",
		"input":       req.graphTarget(),
		"target_type": req.graphTargetType(),
		"candidates":  []map[string]any{},
		"truncated":   false,
	}
}

// changeSurfaceInvestigateCypher is the single-anchor-clause impact traversal
// for the investigate endpoint. Verbs: %s = the label-anchored start node
// pattern, %d = max traversal depth, %s = the optional environment predicate
// (empty or an ` AND (…)` clause), %d = LIMIT (over-fetched by one). It folds
// the start anchor into the path MATCH and groups distinct impacted nodes with
// min(length(path)) in the same clause — the pinned NornicDB build corrupts the
// old separate-MATCH + RETURN DISTINCT + length(path) shape (#5287). The
// environment filter is applied server-side (before LIMIT) so an
// environment-scoped read cannot under-report when the limit is reached.
const changeSurfaceInvestigateCypher = `MATCH path = %s-[*1..%d]->(impacted)
WHERE impacted.id <> $target_id
  AND any(label IN labels(impacted) WHERE label IN ['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset'])%s
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels,
       impacted.environment as environment, impacted.repo_id as repo_id, min(length(path)) as depth
ORDER BY depth, name, id
LIMIT %d`

// changeSurfaceEnvironmentClause returns the NornicDB-safe server-side
// environment predicate to append to a change-surface WHERE clause, or an empty
// string when no environment scope is requested. It keeps impacted nodes whose
// environment matches or is unset (null/empty), mirroring the Go-side filter. The
// predicate deliberately avoids the empty-parameter-disjunct form
// (`$environment = ” OR ...`), which silently drops every row when combined with
// a relationships(path) projection on the pinned NornicDB build (#5287); it is
// only added when an environment is requested so the empty-scope read carries no
// predicate at all.
func changeSurfaceEnvironmentClause(environment string) string {
	if environment == "" {
		return ""
	}
	return "\n  AND (impacted.environment = $environment OR coalesce(impacted.environment, '') = '')"
}

func (h *ImpactHandler) changeSurfaceImpactRows(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
	target changeSurfaceTargetCandidate,
) ([]map[string]any, bool, error) {
	startPattern, err := changeSurfaceTraversalStartPattern(target)
	if err != nil {
		return nil, false, err
	}
	// Single anchoring clause: the pinned NornicDB build mis-executes a read that
	// places a second MATCH (or RETURN DISTINCT / length(path)) between the anchor
	// and the projection — the old two-MATCH shape returned zero rows (#5287,
	// proven live). Fold the start anchor into the path pattern and group the
	// distinct impacted nodes with min(length(path)) in the same clause.
	cypher := fmt.Sprintf(changeSurfaceInvestigateCypher, startPattern, req.MaxDepth, changeSurfaceEnvironmentClause(req.Environment), req.Limit+1)
	params := map[string]any{"target_id": target.ID}
	if req.Environment != "" {
		params["environment"] = req.Environment
	}
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, false, fmt.Errorf("query change surface impact: %w", err)
	}
	rawTruncated := len(rows) > req.Limit
	filtered := make([]map[string]any, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		env := StringVal(row, "environment")
		if req.Environment != "" && env != "" && env != req.Environment {
			continue
		}
		seen[id] = struct{}{}
		filtered = append(filtered, row)
	}
	truncated := rawTruncated || len(filtered) > req.Limit
	if len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}
	return filtered, truncated, nil
}

// changeSurfaceTraversalStartPattern returns the label-anchored start node
// pattern (e.g. `(start:Workload {id: $target_id})`) so callers can fold it into
// a single-clause path MATCH. A single inline-property anchor on one label is the
// NornicDB-safe shape; a separate `MATCH (start:Label {...})` clause before the
// path MATCH makes the read multi-clause and corrupts on the pinned build.
func changeSurfaceTraversalStartPattern(target changeSurfaceTargetCandidate) (string, error) {
	switch {
	case target.hasLabel("Workload"):
		return "(start:Workload {id: $target_id})", nil
	case target.hasLabel("WorkloadInstance"):
		return "(start:WorkloadInstance {id: $target_id})", nil
	case target.hasLabel("Repository"):
		return "(start:Repository {id: $target_id})", nil
	case target.hasLabel("CloudResource"):
		return "(start:CloudResource {id: $target_id})", nil
	case target.hasLabel("TerraformModule"):
		return "(start:TerraformModule {uid: $target_id})", nil
	case target.hasLabel("DataAsset"):
		return "(start:DataAsset {uid: $target_id})", nil
	default:
		return "", fmt.Errorf("change surface traversal cannot anchor unsupported target labels %v", target.Labels)
	}
}

func (c changeSurfaceTargetCandidate) hasLabel(label string) bool {
	for _, candidate := range c.Labels {
		if candidate == label {
			return true
		}
	}
	return false
}

func (h *ImpactHandler) changeSurfaceCodeSurface(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) (map[string]any, error) {
	files := changeSurfaceFileMaps(req.ChangedPaths, req.RepoID)
	symbols := make([]map[string]any, 0)
	evidenceGroups := make([]map[string]any, 0)
	truncated := false
	sourceBackends := []string{}

	if req.Topic != "" {
		rows, err := h.changeSurfaceTopicRows(ctx, req)
		if err != nil {
			return nil, err
		}
		truncated = len(rows) > req.Limit
		if truncated {
			rows = rows[:req.Limit]
		}
		sourceBackends = append(sourceBackends, "postgres_content_store")
		for index, row := range rows {
			files = appendMatchedFile(files, row)
			if row.EntityID != "" {
				symbols = append(symbols, codeTopicSymbol(row, index+1))
			}
			evidenceGroups = append(evidenceGroups, codeTopicEvidenceGroup(row, index+1))
		}
	}
	pathSymbolsTruncated := false
	if len(req.ChangedPaths) > 0 && h != nil && h.Content != nil {
		pathSymbols, symbolsTruncated, err := h.changeSurfacePathSymbols(ctx, req)
		if err != nil {
			return nil, err
		}
		symbols = appendUniqueSymbolMaps(symbols, pathSymbols)
		pathSymbolsTruncated = symbolsTruncated
		sourceBackends = append(sourceBackends, "postgres_content_store")
	}
	truncated = truncated || pathSymbolsTruncated

	return map[string]any{
		"topic":              req.Topic,
		"changed_files":      files,
		"matched_file_count": len(files),
		"touched_symbols":    symbols,
		"symbol_count":       len(symbols),
		"evidence_groups":    evidenceGroups,
		"truncated":          truncated,
		"source_backends":    uniqueStrings(sourceBackends),
		"coverage": map[string]any{
			"query_shape":         "content_topic_and_changed_path_surface",
			"changed_path_count":  len(req.ChangedPaths),
			"changed_path_lookup": "path_scoped",
			"returned_symbols":    len(symbols),
			"limit":               req.Limit,
			"offset":              req.Offset,
			"truncated":           truncated,
		},
	}, nil
}

func (h *ImpactHandler) changeSurfaceTopicRows(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	if h == nil || h.Content == nil {
		return nil, errCodeTopicBackendUnavailable
	}
	investigator, ok := h.Content.(codeTopicContentInvestigator)
	if !ok {
		return nil, errCodeTopicBackendUnavailable
	}
	topicReq := codeTopicInvestigationRequest{
		Topic:  req.Topic,
		RepoID: req.RepoID,
		Limit:  req.Limit + 1,
		Offset: req.Offset,
		Intent: "change_surface",
		Terms:  codeTopicSearchTerms(req.Topic, "change_surface", nil),
	}
	rows, err := investigator.investigateCodeTopic(ctx, topicReq)
	if err != nil {
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	return rows, nil
}

func (h *ImpactHandler) changeSurfacePathSymbols(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) ([]map[string]any, bool, error) {
	entities, err := h.Content.ListRepoEntitiesByPaths(ctx, req.RepoID, req.ChangedPaths, req.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list repo entities by changed paths: %w", err)
	}
	symbols := make([]map[string]any, 0)
	for _, entity := range entities {
		symbols = append(symbols, map[string]any{
			"entity_id":     entity.EntityID,
			"entity_name":   entity.EntityName,
			"entity_type":   entity.EntityType,
			"repo_id":       entity.RepoID,
			"relative_path": entity.RelativePath,
			"language":      entity.Language,
			"start_line":    entity.StartLine,
			"end_line":      entity.EndLine,
			"source_handle": map[string]any{
				"repo_id":       entity.RepoID,
				"relative_path": entity.RelativePath,
				"start_line":    entity.StartLine,
				"end_line":      entity.EndLine,
			},
		})
	}
	truncated := len(symbols) > req.Limit
	if truncated {
		symbols = symbols[:req.Limit]
	}
	return symbols, truncated, nil
}

func (h *ImpactHandler) changeSurfaceResponse(
	req changeSurfaceInvestigationRequest,
	resolution map[string]any,
	codeSurface map[string]any,
	impactRows []map[string]any,
	graphTruncated bool,
) map[string]any {
	direct, transitive := splitImpactRows(impactRows)
	truncated := graphTruncated || boolMapValue(codeSurface, "truncated") || boolMapValue(resolution, "truncated")
	resp := map[string]any{
		"scope":                  changeSurfaceScope(req),
		"target_resolution":      resolution,
		"code_surface":           codeSurface,
		"direct_impact":          direct,
		"transitive_impact":      transitive,
		"recommended_next_calls": changeSurfaceRecommendedNextCalls(req, resolution, codeSurface, direct, transitive),
		"impact_summary": map[string]any{
			"direct_count":     len(direct),
			"transitive_count": len(transitive),
			"total_count":      len(direct) + len(transitive),
		},
		"coverage": map[string]any{
			"query_shape":       changeSurfaceQueryShape(resolution),
			"max_depth":         req.MaxDepth,
			"limit":             req.Limit,
			"offset":            req.Offset,
			"truncated":         truncated,
			"direct_count":      len(direct),
			"transitive_count":  len(transitive),
			"code_symbol_count": intMapValue(codeSurface, "symbol_count"),
		},
		"limit":          req.Limit,
		"offset":         req.Offset,
		"truncated":      truncated,
		"source_backend": "hybrid_graph_and_content",
	}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	return attachAnswerMetadata(resp)
}

func splitImpactRows(rows []map[string]any) ([]map[string]any, []map[string]any) {
	direct := make([]map[string]any, 0)
	transitive := make([]map[string]any, 0)
	for _, row := range rows {
		entry := changeSurfaceImpactEntry(row)
		if IntVal(row, "depth") <= 1 {
			direct = append(direct, entry)
			continue
		}
		transitive = append(transitive, entry)
	}
	return direct, transitive
}

func changeSurfaceImpactEntry(row map[string]any) map[string]any {
	entry := map[string]any{
		"id":              StringVal(row, "id"),
		"name":            StringVal(row, "name"),
		"labels":          StringSliceVal(row, "labels"),
		"depth":           IntVal(row, "depth"),
		"evidence_handle": map[string]any{"entity_id": StringVal(row, "id")},
	}
	if env := StringVal(row, "environment"); env != "" {
		entry["environment"] = env
	}
	if repoID := StringVal(row, "repo_id"); repoID != "" {
		entry["repo_id"] = repoID
	}
	return entry
}

func changeSurfaceScope(req changeSurfaceInvestigationRequest) map[string]any {
	return map[string]any{
		"repo_id":       req.RepoID,
		"environment":   req.Environment,
		"target":        req.graphTarget(),
		"target_type":   req.graphTargetType(),
		"changed_paths": req.ChangedPaths,
		"topic":         req.Topic,
		"limit":         req.Limit,
		"offset":        req.Offset,
		"max_depth":     req.MaxDepth,
	}
}

func changeSurfaceFileMaps(paths []string, repoID string) []map[string]any {
	files := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		files = append(files, map[string]any{
			"repo_id":       repoID,
			"relative_path": path,
			"source_handle": map[string]any{"repo_id": repoID, "relative_path": path},
		})
	}
	return files
}

func appendUniqueSymbolMaps(existing, incoming []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	for _, symbol := range existing {
		seen[symbolDedupeKey(symbol)] = struct{}{}
	}
	for _, symbol := range incoming {
		key := symbolDedupeKey(symbol)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		existing = append(existing, symbol)
	}
	return existing
}

func symbolDedupeKey(symbol map[string]any) string {
	parts := []string{
		StringVal(symbol, "entity_id"),
		StringVal(symbol, "repo_id"),
		StringVal(symbol, "relative_path"),
		StringVal(symbol, "entity_name"),
	}
	return strings.Join(parts, "|")
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func changeSurfaceRecommendedNextCalls(
	req changeSurfaceInvestigationRequest,
	resolution map[string]any,
	codeSurface map[string]any,
	direct []map[string]any,
	transitive []map[string]any,
) []map[string]any {
	calls := make([]map[string]any, 0, 3)
	if req.Topic != "" {
		calls = append(calls, map[string]any{"tool": "investigate_code_topic", "args": map[string]any{"topic": req.Topic, "repo_id": req.RepoID, "limit": req.Limit}})
	}
	for _, symbol := range mapSliceValue(codeSurface, "touched_symbols") {
		if entityID := StringVal(symbol, "entity_id"); entityID != "" {
			calls = append(calls, map[string]any{"tool": "get_code_relationship_story", "args": map[string]any{"entity_id": entityID, "limit": req.Limit}})
			break
		}
	}
	if status := StringVal(resolution, "status"); status == "resolved" && (len(direct) > 0 || len(transitive) > 0) {
		selected, _ := resolution["selected"].(map[string]any)
		calls = append(calls, map[string]any{"tool": "find_change_surface", "args": map[string]any{"target": StringVal(selected, "id"), "limit": req.Limit}})
	}
	return calls
}

func changeSurfaceQueryShape(resolution map[string]any) string {
	switch StringVal(resolution, "status") {
	case "resolved":
		return "resolved_change_surface_traversal"
	case "ambiguous":
		return "target_resolution_ambiguity"
	case "no_match":
		return "target_resolution_no_match"
	default:
		return "code_surface_only"
	}
}

func boolMapValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func intMapValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	default:
		return 0
	}
}
