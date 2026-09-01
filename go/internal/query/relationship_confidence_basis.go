// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// addRelationshipConfidenceBasis forwards to
// querycontract.AddRelationshipConfidenceBasis.
//
// The basis rules live in the contract leaf because a handler family and root
// must label the same row identically: the value is compared across responses,
// so two copies that drift would report the same correlation as resting on
// different evidence depending on which package answered.
func addRelationshipConfidenceBasis(row map[string]any) {
	querycontract.AddRelationshipConfidenceBasis(row)
}

// relationshipConfidenceBasis forwards to
// querycontract.RelationshipConfidenceBasis; see that function for the
// precedence between an assertion override and aggregated evidence.
func relationshipConfidenceBasis(row map[string]any) string {
	return querycontract.RelationshipConfidenceBasis(row)
}
