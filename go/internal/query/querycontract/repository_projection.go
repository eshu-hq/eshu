// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"strings"
)

// RepoProjection returns the standard Cypher RETURN clause for repository
// nodes. It lives here (not in a handler family) so the repository routes
// and the catalog, entity, code-relationship, and service-story stayers
// project repository rows identically without importing each other (#6060,
// lane B B3). It returns a RETURN-list fragment the caller splices into its
// own query -- the same carve-out as the authorization seam's WHERE
// fragments -- never a complete query.
func RepoProjection(alias string) string {
	return fmt.Sprintf(
		"%s.id as id, %s.name as name, %s.path as path, "+
			"coalesce(%s.local_path, %s.path) as local_path, "+
			"%s.remote_url as remote_url, "+
			"%s.repo_slug as repo_slug, "+
			"coalesce(%s.has_remote, false) as has_remote",
		alias, alias, alias, alias, alias, alias, alias, alias,
	)
}

// RepoRef is the canonical repository reference returned by query endpoints.
// It lives here (not in a handler family) so the repository routes and the
// staying Neo4j adapter share one row shape without importing each other
// (#6060, lane B B3).
type RepoRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	RemoteURL string `json:"remote_url,omitempty"`
	RepoSlug  string `json:"repo_slug,omitempty"`
	HasRemote bool   `json:"has_remote"`
}

// RepoRefFromRow converts a graph result row to a RepoRef. It lives here for
// the same reason as RepoRef; root keeps a wrapper so its callers are
// unchanged.
func RepoRefFromRow(row map[string]any) RepoRef {
	localPath := StringVal(row, "local_path")
	if localPath == "" {
		localPath = StringVal(row, "path")
	}
	name := StringVal(row, "name")
	if name == "" && localPath != "" {
		parts := strings.Split(localPath, "/")
		name = parts[len(parts)-1]
	}
	return RepoRef{
		ID:        StringVal(row, "id"),
		Name:      name,
		LocalPath: localPath,
		RemoteURL: StringVal(row, "remote_url"),
		RepoSlug:  StringVal(row, "repo_slug"),
		HasRemote: BoolVal(row, "has_remote"),
	}
}

// RepositoryDependencyMarkerProjection returns the Cypher fragment marking
// repositories that have depending-repository nodes eligible under access.
// It lives here (not in a handler family) so the repository routes and the
// catalog routes project the marker identically without importing each
// other (#6060, lane B B3). Like RepoProjection it returns a fragment the
// caller splices into its own query, never a complete query.
func RepositoryDependencyMarkerProjection(alias string, access RepositoryAccessFilter) string {
	const depAlias = "dep"
	predicate := access.GraphPredicate(depAlias)
	return fmt.Sprintf(
		"EXISTS { MATCH (%s)<-[:DEPENDS_ON]-(%s:Repository)%s } as is_dependency",
		alias, depAlias, predicate,
	)
}
