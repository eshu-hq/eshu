// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"net/url"
)

// repositoryFilesRoute maps a list_repository_files call to the bounded
// GET /api/v0/repositories/{repo_id}/tree endpoint. The optional language
// query parameter is forwarded verbatim; the API applies alias expansion
// (e.g. typescript also matches tsx) and returns an empty listing for an
// unrecognised token rather than an error, which is the correct bounded
// behaviour for an open-token language filter.
func repositoryFilesRoute(toolName string, args map[string]any) (*route, bool, error) {
	if toolName != "list_repository_files" {
		return nil, false, nil
	}

	repoID := str(args, "repo_id")
	if repoID == "" {
		return nil, true, fmt.Errorf("repo_id is required")
	}

	q := map[string]string{}
	if lang := str(args, "language"); lang != "" {
		q["language"] = lang
	}
	if path := str(args, "path"); path != "" {
		q["path"] = path
	}
	if boolOr(args, "recursive", false) {
		q["recursive"] = "true"
	}
	if ref := str(args, "ref"); ref != "" {
		q["ref"] = ref
	}

	return &route{
		method: "GET",
		path:   "/api/v0/repositories/" + url.PathEscape(repoID) + "/tree",
		query:  q,
	}, true, nil
}
