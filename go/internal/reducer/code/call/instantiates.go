// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

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
	entityIndex shared.EntityIndex,
	callerID string,
	calleeID string,
	callerFilePath string,
	calleeFilePath string,
	callLine int,
	edge map[string]any,
) []map[string]any {
	if payloadcore.AnyToString(edge["call_kind"]) != "constructor_call" {
		return rows
	}
	calleeType := shared.EndpointEntityType(entityIndex, repositoryID, calleeID)
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
		"caller_entity_type": shared.EndpointEntityType(entityIndex, repositoryID, callerID),
		"callee_entity_id":   calleeID,
		"callee_entity_type": calleeType,
		"caller_file":        callerFilePath,
		"callee_file":        calleeFilePath,
		"ref_line":           callLine,
		"relationship_type":  "INSTANTIATES",
		"resolution_method":  codeprovenance.MethodTypeInferred,
		"action":             reducercontract.IntentActionUpsert,
	}
	shared.CopyOptionalField(row, edge, "full_name")
	shared.CopyOptionalField(row, edge, "call_kind")
	return append(rows, row)
}
