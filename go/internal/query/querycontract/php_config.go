// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// PHPConfigCursor is the ordered content-entity position of the last row in
// one PHP configuration page. Repository grants are applied again on resume.
type PHPConfigCursor struct {
	Name         string `json:"name"`
	FilterRepoID string `json:"filter_repo_id,omitempty"`
	RepoID       string `json:"repo_id"`
	RelativePath string `json:"relative_path"`
	StartLine    int    `json:"start_line"`
	EntityID     string `json:"entity_id"`
}
