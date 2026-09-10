// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// NornicDBCallChainNode projects one raw traversal row onto the node
// shape the response path normalizes, carrying the extra context keys
// a row carries when present.
func NornicDBCallChainNode(row map[string]any) map[string]any {
	node := map[string]any{
		"id":          querycontract.StringVal(row, "id"),
		"name":        querycontract.StringVal(row, "name"),
		"labels":      querycontract.StringSliceVal(row, "labels"),
		"language":    row["language"],
		"docstring":   row["docstring"],
		"method_kind": row["method_kind"],
	}
	for _, key := range []string{"decorators", "async", "class_context", "impl_context", "semantic_kind"} {
		if value, ok := row[key]; ok {
			node[key] = value
		}
	}
	return node
}

// EndpointCandidates narrows name-matched candidates to the requested
// entity when one is named, or synthesizes a single Function candidate
// so an unknown id still probes the graph once.
func EndpointCandidates(entityID string, candidates []querycontract.EntityContent) []querycontract.EntityContent {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return candidates
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.EntityID) == entityID {
			return []querycontract.EntityContent{candidate}
		}
	}
	return []querycontract.EntityContent{{EntityID: entityID, EntityType: "Function"}}
}

// CandidateLabel reads a candidate's entity type, defaulting to
// Function for the traversal's label anchor.
func CandidateLabel(candidate querycontract.EntityContent) string {
	if label := strings.TrimSpace(candidate.EntityType); label != "" {
		return label
	}
	return "Function"
}
