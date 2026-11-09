// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import "strings"

// EntityKeyPrefix prefixes the entity keys that name advisory groups. The
// projection handler derives group ids from these keys with
// GroupIDFromEntityKey; the graph writer never sees the prefixed form.
const EntityKeyPrefix = "advisory_group:"

// EntityKey returns the entity key naming groupID on a projection intent.
func EntityKey(groupID string) string {
	return EntityKeyPrefix + groupID
}

// GroupIDFromEntityKey strips EntityKeyPrefix from key and reports whether
// the prefix was present. The empty remainder is rejected: a bare prefix
// names no group.
func GroupIDFromEntityKey(key string) (string, bool) {
	if !strings.HasPrefix(key, EntityKeyPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(key, EntityKeyPrefix)
	if strings.TrimSpace(id) == "" {
		return "", false
	}
	return id, true
}

// Edge is one advisory-group membership: the repository and package the
// advisory covers, with the package ecosystem and PURL the edge stamps.
// RepositoryID is the canonical repository id (for example
// "repository:<slug>"); PackageID is the package identity within that
// repository. The (RepositoryID, PackageID) pair is the edge identity the
// graph writer MERGEs on.
type Edge struct {
	RepositoryID string
	PackageID    string
	Ecosystem    string
	PURL         string
}

// Group is one reconciled advisory group: the advisories that cover the
// same membership, and the membership edges themselves. ID is the unprefixed
// group id stored on the AdvisoryGroup node. CVEIDs and AdvisoryIDs are the
// CVE and provider advisory identifiers the node carries; Edges is the full
// membership the writer reconciles, including stale-edge retraction.
type Group struct {
	ID          string
	CVEIDs      []string
	AdvisoryIDs []string
	Edges       []Edge
}

// WriteStats counts what one ReconcileAdvisoryGroup call changed, summed
// from the driver result summaries of every statement it executed. A
// no-change reconcile reports zeros; a statement the executor did not report
// counters for contributes nothing.
type WriteStats struct {
	NodesCreated  int
	NodesDeleted  int
	EdgesCreated  int
	EdgesDeleted  int
	PropertiesSet int
}

// WinnerRow is one grouped advisory row the projection handler reconciles:
// the winning (repository, package, advisory) triple plus every member of
// the group it won. Members holds the full membership, including the
// winner itself.
type WinnerRow struct {
	RepositoryID string
	PackageID    string
	Ecosystem    string
	PURL         string
	CVEID        string
	AdvisoryID   string
	Members      []string
}
