// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// RowRepoID extracts the repo id a symbol→runtime intent row keys its
// readiness on, using the same precedence the presence-gate key functions
// use (handlesRouteEndpointPresenceKey / runsInRepoWorkloadPresenceKey): the
// payload repo_id first, then RepositoryID. Sharing this precedence keeps the
// readiness repo id and the presence repo id the SAME string for the same
// row, so the phase gate and the presence gate agree on which repo a row
// belongs to. This is also the string the workload-materialization handler
// publishes its repo-keyed phase row under (the APIEndpointRow/WorkloadRow
// RepoID), so consumer and publisher reconstruct an identical key.
func RowRepoID(row Row) string {
	repoID := payloadcore.PayloadStr(row.Payload, "repo_id")
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	return repoID
}

// CarriesNoEdge reports whether a shared-projection row exists to drive a
// retract or another control action rather than to write an edge of its own.
// A repo refresh intent is the canonical case: its whole job is to issue one
// repo-wide retract, and it has no endpoints a write statement could match.
//
// FilterUpsertRows already keeps these rows out of the write phase (they
// carry action="refresh"). Canonical edge writers consult this predicate as
// well so the two layers agree on what "no edge to write" means: a control
// row must never be counted as an unroutable edge, because that is the
// signal #5984 uses to refuse to complete an intent, and a false one there
// would wedge a partition on work that was correct all along.
func CarriesNoEdge(row Row) bool {
	if IsRepoRefreshRow(row) {
		return true
	}
	action, ok := row.Payload["action"]
	if !ok {
		return false
	}
	s, isStr := action.(string)
	return isStr && s != "upsert"
}

// FilterUpsertRows returns rows whose payload action is "upsert" or absent.
func FilterUpsertRows(rows []Row) []Row {
	var result []Row
	for _, row := range rows {
		action, ok := row.Payload["action"]
		if ok {
			if s, isStr := action.(string); isStr && s != "upsert" {
				continue
			}
		}
		result = append(result, row)
	}
	return result
}

// UniqueRepositoryIDs returns the distinct, sorted, non-empty repository IDs
// carried by a set of rows.
func UniqueRepositoryIDs(rows []Row) []string {
	seen := make(map[string]struct{}, len(rows))
	repositoryIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		repositoryID := strings.TrimSpace(row.RepositoryID)
		if repositoryID == "" {
			continue
		}
		if _, ok := seen[repositoryID]; ok {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}
