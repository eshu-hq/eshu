// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudjoin

import "github.com/eshu-hq/eshu/go/internal/facts"

// ResolveSource resolves an AWS relationship's source endpoint to a
// CloudResource uid against the given join index. The scanner sets
// source_resource_id to the ARN or the bare id consistently, so source
// resolution tries the ARN index first, then the resource-id index.
//
// This lives in [cloudjoin] rather than in a single family package (issue
// #6061) because both the reducer root's AWS relationship edge projection
// (aws_relationship_join.go) and the [awscloud] cloud-image edge projection
// resolve their source endpoint through this identical index and logic, and a
// family package may never import the reducer root.
func ResolveSource(index CloudResourceJoinIndex, sourceARN, sourceResourceID string) (string, bool) {
	if sourceARN != "" {
		if uid, ok := index.ByARN[sourceARN]; ok {
			return uid, true
		}
	}
	if sourceResourceID != "" {
		if uid, ok := index.ByARN[sourceResourceID]; ok {
			return uid, true
		}
		if uid, ok := index.ByResourceID[sourceResourceID]; ok {
			return uid, true
		}
	}
	return "", false
}

// SplitAWSFactEnvelopes partitions a mixed envelope slice into resource and
// relationship facts in one pass so a join index and edge facts are built from
// a single bounded load. It lives in [cloudjoin] (issue #6061) because both
// the reducer root's AWS relationship edge materialization
// (aws_relationship_materialization.go) and the [awscloud] cloud-image edge
// materialization split the same envelope shape before building a
// [CloudResourceJoinIndex] from it.
func SplitAWSFactEnvelopes(envelopes []facts.Envelope) (resources, relationships []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSRelationshipFactKind:
			relationships = append(relationships, env)
		}
	}
	return resources, relationships
}
