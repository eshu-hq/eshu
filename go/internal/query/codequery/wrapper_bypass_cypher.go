// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// wrapperBypassAnchorLabels is the label set wrapper-bypass builders seed
// anchors with. It mirrors the CALLS-source label set the canonical edge
// writer projects (Function|Class|File), so every call-chain endpoint
// resolves while the planner seeds from a label/index scan.
const wrapperBypassAnchorLabels = "Function|Class|File"

// BuildWrapperCallersCypher renders the one-hop callers-of-target read for
// wrapper-bypass qualification, binding the request's repository scope and
// the caller's grant in the anchoring WHERE (never after an OPTIONAL MATCH,
// where they would filter nothing — see the #5167 lesson recorded on the
// chain one-hop rows). Both backends return the same columns: id, name,
// file_path, edge_method, edge_confidence, complexity.
func BuildWrapperCallersCypher(
	targetEntityID, repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"target_entity_id": strings.TrimSpace(targetEntityID)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(caller.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("caller", "repo_id"))
	}
	if backend == querycontract.GraphBackendNornicDB {
		return buildNornicDBWrapperCallers(params, predicates), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (target:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (caller)-[rel:CALLS]->(target)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("target", "$target_entity_id"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(target.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString("\n" + wrapperCallerReturns("caller", "rel"))
	return cypher.String(), params
}

// buildNornicDBWrapperCallers renders the NornicDB dialect: the anchor sits
// in the node pattern (MATCH-plus-WHERE id/uid predicates can scan or hang
// on the pinned build), and the same repo/grant predicates bind the caller.
func buildNornicDBWrapperCallers(params map[string]any, predicates []string) string {
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH " + relationships.NornicDBNodePattern("target", "Function", "$target_entity_id") + "\n")
	cypher.WriteString("\t\tMATCH (caller)-[rel:CALLS]->(target)")
	if len(predicates) > 0 {
		cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
	}
	cypher.WriteString("\n" + wrapperCallerReturns("caller", "rel"))
	return cypher.String()
}

// wrapperCallerReturns projects the caller row both backends return.
func wrapperCallerReturns(caller, rel string) string {
	return "\t\tOPTIONAL MATCH (" + caller + ")<-[:CONTAINS]-(callerFile:File)\n" +
		"\t\tRETURN coalesce(" + caller + ".id, " + caller + ".uid) as id,\n" +
		"\t\t       " + caller + ".name as name,\n" +
		"\t\t       callerFile.relative_path as file_path,\n" +
		"\t\t       " + rel + ".resolution_method as edge_method,\n" +
		"\t\t       coalesce(" + rel + ".confidence, 0) as edge_confidence,\n" +
		"\t\t       coalesce(" + caller + ".cyclomatic_complexity, 0) as complexity\n"
}

// BuildWrapperFanInCypher renders the caller-count read for one entity: the
// fan-in floor check without fetching caller rows.
func BuildWrapperFanInCypher(
	entityID, repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"entity_id": strings.TrimSpace(entityID)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(caller.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("caller", "repo_id"))
	}
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tMATCH " + relationships.NornicDBNodePattern("target", "Function", "$entity_id") + "\n")
		cypher.WriteString("\t\tMATCH (caller)-[:CALLS]->(target)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString("\n\t\tRETURN count(caller) as fan_in\n")
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (target:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (caller)-[:CALLS]->(target)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("target", "$entity_id"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(target.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString("\n\t\tRETURN count(caller) as fan_in\n")
	return cypher.String(), params
}

// BuildWrapperCalleesCypher renders the outgoing-callee-id read for one
// entity: the thinness check counts distinct callees and calls to the
// target in Go from these ids.
func BuildWrapperCalleesCypher(
	entityID, targetID, repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"entity_id": strings.TrimSpace(entityID), "target_entity_id": strings.TrimSpace(targetID)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(callee.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("callee", "repo_id"))
	}
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tMATCH " + relationships.NornicDBNodePattern("source", "Function", "$entity_id") + "\n")
		cypher.WriteString("\t\tMATCH (source)-[:CALLS]->(callee)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString("\n\t\tRETURN coalesce(callee.id, callee.uid) as id\n")
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (source:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (source)-[:CALLS]->(callee)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("source", "$entity_id"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(source.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString("\n\t\tRETURN coalesce(callee.id, callee.uid) as id\n")
	return cypher.String(), params
}

// BuildWrapperFamilyCallersCypher renders the batched one-hop callers read
// for wrapper-bypass enumeration: UNWIND over target entity ids, one row
// per (target, caller) with the single-target columns plus the target id
// and name for demux. Every target stays anchored on its indexed entity id
// with repo/grant in the anchoring WHERE on both dialects, like the
// single-target read.
func BuildWrapperFamilyCallersCypher(
	targetIDs []string,
	repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"target_ids": dedupeEntityIDs(targetIDs)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(caller.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("caller", "repo_id"))
	}
	returns := "\n\t\tOPTIONAL MATCH (caller)<-[:CONTAINS]-(callerFile:File)\n" +
		"\t\tRETURN tid as target_id,\n" +
		"\t\t       target.name as target_name,\n" +
		"\t\t       coalesce(caller.id, caller.uid) as id,\n" +
		"\t\t       caller.name as name,\n" +
		"\t\t       callerFile.relative_path as file_path,\n" +
		"\t\t       rel.resolution_method as edge_method,\n" +
		"\t\t       coalesce(rel.confidence, 0) as edge_confidence,\n" +
		"\t\t       coalesce(caller.cyclomatic_complexity, 0) as complexity\n"
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tUNWIND $target_ids AS tid\n")
		cypher.WriteString("\t\tMATCH " + relationships.NornicDBNodePattern("target", "Function", "tid") + "\n")
		cypher.WriteString("\t\tMATCH (caller)-[rel:CALLS]->(target)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString(returns)
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tUNWIND $target_ids AS tid\n")
	cypher.WriteString("\t\tMATCH (target:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (caller)-[rel:CALLS]->(target)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("target", "tid"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(target.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString(returns)
	return cypher.String(), params
}

// BuildWrapperFamilyFanInCypher renders the batched caller-count read: one
// (id, fan_in) row per candidate entity id, so ranking a target's direct
// callers costs one round trip no matter how large D grows.
func BuildWrapperFamilyFanInCypher(
	entityIDs []string,
	repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"entity_ids": dedupeEntityIDs(entityIDs)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(caller.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("caller", "repo_id"))
	}
	returns := "\n\t\tRETURN eid as id, count(caller) as fan_in\n"
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tUNWIND $entity_ids AS eid\n")
		cypher.WriteString("\t\tMATCH " + relationships.NornicDBNodePattern("target", "Function", "eid") + "\n")
		cypher.WriteString("\t\tMATCH (caller)-[:CALLS]->(target)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString(returns)
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tUNWIND $entity_ids AS eid\n")
	cypher.WriteString("\t\tMATCH (target:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (caller)-[:CALLS]->(target)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("target", "eid"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(target.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString(returns)
	return cypher.String(), params
}

// dedupeEntityIDs trims, drops empties, and dedupes an id list so UNWIND
// bindings stay minimal and deterministic.
func dedupeEntityIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// BuildWrapperFamilyCalleesCypher renders the batched outgoing-callee read
// for wrapper-bypass enumeration: UNWIND over source entity ids, one
// (source_id, id) row per outgoing CALLS edge. The track demuxes the pairs
// into per-wrapper delegation targets and per-winner thinness inputs, so
// one round trip serves the whole nominated family.
func BuildWrapperFamilyCalleesCypher(
	sourceIDs []string,
	repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"source_ids": dedupeEntityIDs(sourceIDs)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(callee.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("callee", "repo_id"))
	}
	returns := "\n\t\tRETURN sid as source_id, coalesce(callee.id, callee.uid) as id\n"
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tUNWIND $source_ids AS sid\n")
		cypher.WriteString("\t\tMATCH " + relationships.NornicDBNodePattern("source", "Function", "sid") + "\n")
		cypher.WriteString("\t\tMATCH (source)-[:CALLS]->(callee)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString(returns)
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tUNWIND $source_ids AS sid\n")
	cypher.WriteString("\t\tMATCH (source:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (source)-[:CALLS]->(callee)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("source", "sid"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(source.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString(returns)
	return cypher.String(), params
}
