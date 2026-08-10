// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// errChangeSurfaceRepoNotGranted is returned by changeSurfaceCodeSurface when
// the caller supplied an explicit repo_id (required alongside changed_paths)
// that is outside their scoped-token/browser-session grant. Callers translate
// it to a not-found response rather than a 503/500 so a repository outside
// the grant renders identically to an unknown repository.
var errChangeSurfaceRepoNotGranted = errors.New("repository is outside the caller's grant")

func changeSurfaceNoTargetResolution(req changeSurfaceInvestigationRequest) map[string]any {
	return map[string]any{
		"status":      "not_requested",
		"input":       req.graphTarget(),
		"target_type": req.graphTargetType(),
		"candidates":  []map[string]any{},
		"truncated":   false,
	}
}

// changeSurfaceInvestigateCypher shares the NornicDB-safe, single-anchor-clause
// outgoing traversal with the legacy endpoint. Both callers decode raw path
// relationships in Go so dependency direction is validated consistently.
const changeSurfaceInvestigateCypher = changeSurfaceLegacyCypher

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
	// #5167 W3: the start target was already bound to the grant by
	// resolveChangeSurfaceTarget, but the traversal reaches nodes several hops
	// away and impacted.repo_id is projected below, so every impacted row is
	// re-bound to the caller's grant independently -- an in-grant target can
	// still transitively impact a repository the caller does not hold.
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, false, nil
	}
	rows, rawTruncated, err := h.changeSurfaceTraversalRows(
		ctx, target, req.Environment, req.MaxDepth, req.Limit, access,
	)
	if err != nil {
		return nil, false, err
	}
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
		seen[id] = struct{}{}
		filtered = append(filtered, row)
	}
	truncated := rawTruncated || len(filtered) > req.Limit
	if len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}
	return filtered, truncated, nil
}

// changeSurfaceImpactedRowRepoID resolves the owning repository id for an
// impacted row projected by changeSurfaceInvestigateCypher/
// changeSurfaceLegacyCypher. A Repository-labeled impacted node carries no
// repo_id property of its own (its id IS the repository id), so the
// Repository label is checked first because a Repository's canonical grant key
// is its id. This prevents a malformed Repository node carrying another
// tenant's repo_id from being authorized by that colliding property. Other
// supported impact labels bind through repo_id.
func changeSurfaceImpactedRowRepoID(row map[string]any) string {
	for _, label := range StringSliceVal(row, "labels") {
		if label == "Repository" {
			return StringVal(row, "id")
		}
	}
	return StringVal(row, "repo_id")
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
