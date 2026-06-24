// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/codeprovenance"

// instantiatesTargetTypes are the entity types that can be the target of an
// INSTANTIATES edge: a constructor call instantiates a concrete type.
var instantiatesTargetTypes = map[string]struct{}{
	"Class":  {},
	"Struct": {},
	"Enum":   {},
}

// appendInstantiatesRow emits an INSTANTIATES edge from the caller to the
// constructed type when the call is a constructor call resolved to a
// class/struct/enum (issue #2229). It is additive: the existing CALLS edges to
// the type and its constructor are unchanged, so call-graph reachability is
// preserved while construction becomes separately queryable. The edge is
// type-inferred provenance because it rides the constructor resolution.
func appendInstantiatesRow(
	rows []map[string]any,
	seenRows map[string]struct{},
	repositoryID string,
	entityIndex codeEntityIndex,
	callerID string,
	calleeID string,
	callerFilePath string,
	calleeFilePath string,
	callLine int,
	edge map[string]any,
) []map[string]any {
	if anyToString(edge["call_kind"]) != "constructor_call" {
		return rows
	}
	calleeType := codeCallEndpointEntityType(entityIndex, repositoryID, calleeID)
	if _, ok := instantiatesTargetTypes[calleeType]; !ok {
		return rows
	}

	key := codeCallRowKey(repositoryID, callerID, calleeID, "INSTANTIATES", callLine)
	if _, exists := seenRows[key]; exists {
		return rows
	}
	seenRows[key] = struct{}{}

	row := map[string]any{
		"repo_id":            repositoryID,
		"caller_entity_id":   callerID,
		"caller_entity_type": codeCallEndpointEntityType(entityIndex, repositoryID, callerID),
		"callee_entity_id":   calleeID,
		"callee_entity_type": calleeType,
		"caller_file":        callerFilePath,
		"callee_file":        calleeFilePath,
		"ref_line":           callLine,
		"relationship_type":  "INSTANTIATES",
		"resolution_method":  codeprovenance.MethodTypeInferred,
		"action":             IntentActionUpsert,
	}
	copyOptionalCodeCallField(row, edge, "full_name")
	copyOptionalCodeCallField(row, edge, "call_kind")
	return append(rows, row)
}
