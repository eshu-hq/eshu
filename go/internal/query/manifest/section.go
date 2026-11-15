// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"net/url"
)

// maxContextSectionDependencies bounds the manifest_dependencies block of the
// repository context response. A repository declares far more than this; the
// cap keeps a populated context inside the response budget (#10654) and the
// full list is one paged read away.
const maxContextSectionDependencies = 50

// ManifestDependencySection is the bounded manifest_dependencies block of the
// repository context response: the declared dependencies the section lists,
// the truncation marker, and a pointer to the paged read for the full list.
//
// Count is the number of dependencies the section lists (bounded by
// maxContextSectionDependencies), not the repository's total; Truncated is
// true when more declared dependencies exist beyond the bound, and FullList
// names the paged read that returns them all. Runtime dependencies are the
// default; dev and test dependencies appear only when the caller opted in.
type ManifestDependencySection struct {
	Count        int                     `json:"count"`
	Dependencies []ManifestDependencyRow `json:"dependencies"`
	Truncated    bool                    `json:"truncated"`
	FullList     string                  `json:"full_list,omitempty"`
}

// LoadContextSection reads the bounded manifest_dependencies section for one
// repository. The caller has already resolved and authorized repositoryID, so
// this applies no grant of its own. refused reports a refused delta base: the
// caller degrades with a named partial reason instead of rendering a
// delta-only section as authoritative.
func LoadContextSection(
	ctx context.Context,
	store ManifestDependencyStore,
	repositoryID string,
	includeDev bool,
) (ManifestDependencySection, bool, error) {
	page, refused, err := store.ListRepositoryManifestDependencyFacts(
		ctx, repositoryID, nil, "", maxContextSectionDependencies+1, includeDev)
	if err != nil {
		return ManifestDependencySection{Dependencies: []ManifestDependencyRow{}}, refused, err
	}
	rows := page.Rows
	truncated := len(rows) > maxContextSectionDependencies
	if truncated {
		rows = rows[:maxContextSectionDependencies]
	}
	fullList := "/api/v0/repository/manifest-dependencies?repository_id=" + url.QueryEscape(repositoryID)
	if includeDev {
		fullList += "&include_dev_dependencies=true"
	}
	return ManifestDependencySection{
		Count:        len(rows),
		Dependencies: rows,
		Truncated:    truncated,
		FullList:     fullList,
	}, refused, nil
}
