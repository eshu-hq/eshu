// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
)

// CloudResourceCurrentInventoryFilter answers which of a bounded candidate set of
// CloudResource uids are BOTH current — materialized from an active-generation,
// non-tombstoned source fact, so a node left stale by a resource that vanished
// from a later scan is excluded — AND authorized for the caller's scope grants.
// It is the current-inventory + authorization gate the supply-chain runtime probe
// applies to its digest-matched graph nodes, so neither a stale CloudResource node
// nor a cross-scope resource can become runtime_confirmed evidence (#5452 codex
// P1a staleness + #5787 scoped-caller authorization). The candidate set is the
// probe's already-bounded digest matches, so the read stays bounded.
type CloudResourceCurrentInventoryFilter interface {
	CurrentAuthorizedCloudResourceUIDs(
		ctx context.Context,
		candidateUIDs []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]string, error)
}

// CloudResourceRuntimeDigestMatch is one current, authorized cloud resource
// whose reducer-owned winning row reports a requested running image digest.
type CloudResourceRuntimeDigestMatch struct {
	UID    string
	Digest string
	ARN    string
}

// CloudResourceRuntimeDigestResolver resolves current, authorized runtime image
// evidence directly from the graph owner ledger. The ledger carries the exact
// winning CloudResource row, so this avoids an unindexed graph label scan and a
// second candidate-authorization round trip.
type CloudResourceRuntimeDigestResolver interface {
	CurrentAuthorizedCloudResourcesByDigest(
		ctx context.Context,
		digests []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]CloudResourceRuntimeDigestMatch, error)
}
