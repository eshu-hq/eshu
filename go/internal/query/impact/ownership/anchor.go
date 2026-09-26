// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FromAnchor converts a resolved impact anchor into a Node.
func FromAnchor(anchor *deployment.ResolvedImpactAnchor) Node {
	if anchor == nil {
		return Node{}
	}
	labels := anchor.Labels
	if len(labels) == 0 && anchor.Label != "" {
		labels = []string{anchor.Label}
	}
	return Node{ID: anchor.ID, UID: anchor.UID, Name: anchor.Name, RepoID: anchor.RepoID, Labels: labels}
}

// FromIdentities converts decoded nodes(path) identities into Nodes, keeping
// order and length (a zero identity stays in place and is denied).
func FromIdentities(identities []deployment.ImpactNodeIdentity) []Node {
	out := make([]Node, 0, len(identities))
	for _, identity := range identities {
		out = append(out, Node{ID: identity.ID, UID: identity.UID, Name: identity.Name, RepoID: identity.RepoID, Labels: identity.Labels})
	}
	return out
}

// ResolveAnchor resolves an impact anchor for access. An unscoped caller gets
// whatever resolve returns. An empty grant returns nil without a graph call.
// A scoped caller's identifier is resolved by candidates (every node carrying
// it, bounded and in a deterministic order); all candidates are judged by one
// Check and the first one the grant owns is the anchor, so a name another
// tenant shares cannot shadow the caller's own node. When no candidate is
// owned the result is nil, the same value an unknown anchor resolves to, so
// the caller renders both identically and issues no traversal.
func (c Checker) ResolveAnchor(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	resolve func() (*deployment.ResolvedImpactAnchor, error),
	candidates func() ([]deployment.ResolvedImpactAnchor, error),
) (*deployment.ResolvedImpactAnchor, error) {
	if access.Empty() {
		return nil, nil
	}
	if !access.Scoped() {
		return resolve()
	}
	found, err := candidates()
	if err != nil || len(found) == 0 {
		return nil, err
	}
	nodes := make([]Node, 0, len(found))
	for i := range found {
		nodes = append(nodes, FromAnchor(&found[i]))
	}
	verdict, err := c.Check(ctx, access, nodes)
	if err != nil {
		return nil, err
	}
	for i, node := range nodes {
		if verdict.Admits(node) {
			anchor := found[i]
			return &anchor, nil
		}
	}
	RecordWithheld(ctx, c.Instruments, c.Route, ReasonAnchorUngranted, 1)
	return nil, nil
}

// FilterRows keeps the rows of one bounded page whose every path node the
// grant owns, judged by one FilterPaths call; nodesOf decodes a row's path.
// It returns the kept rows in order and whether the ownership budget capped
// the check (the caller then reports truncated). An unscoped caller's rows
// are returned unchanged.
func (c Checker) FilterRows(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	rows []map[string]any,
	nodesOf func(map[string]any) []Node,
) ([]map[string]any, bool, error) {
	if !access.Scoped() {
		return rows, false, nil
	}
	paths := make([][]Node, len(rows))
	for i, row := range rows {
		paths[i] = nodesOf(row)
	}
	filter, err := c.FilterPaths(ctx, access, paths)
	if err != nil {
		return nil, false, err
	}
	kept := make([]map[string]any, 0, filter.Kept())
	for i, row := range rows {
		if filter.Keep[i] {
			kept = append(kept, row)
		}
	}
	return kept, filter.Capped, nil
}

// TerminalGrantParams returns the two grant lists the scoped resource-to-code
// statement binds its terminal Repository to, never nil.
func TerminalGrantParams(access querycontract.RepositoryAccessFilter) (repositoryIDs, scopeIDs []string) {
	repositoryIDs = append([]string{}, access.AllowedRepositoryIDs...)
	scopeIDs = append([]string{}, access.AllowedScopeIDs...)
	return repositoryIDs, scopeIDs
}
