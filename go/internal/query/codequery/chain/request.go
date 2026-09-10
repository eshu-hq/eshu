// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"fmt"
	"strings"
)

// Request describes one call-chain lookup: a start and an end, each by
// entity id or by name, with the repository selectors bounding the
// traversal. It decodes from the route's JSON body unchanged from its
// codequery home; codequery keeps a type alias so every existing
// spelling still resolves.
type Request struct {
	Start         string `json:"start"`
	End           string `json:"end"`
	StartEntityID string `json:"start_entity_id"`
	EndEntityID   string `json:"end_entity_id"`
	RepoID        string `json:"repo_id"`
	CrossRepo     bool   `json:"cross_repo"`
	StartRepoID   string `json:"start_repo_id"`
	EndRepoID     string `json:"end_repo_id"`
	MaxDepth      int    `json:"max_depth"`
}

// Validate rejects requests whose cross-repo selectors disagree with
// the cross-repo flag.
func (r Request) Validate() error {
	if !r.CrossRepo && (strings.TrimSpace(r.StartRepoID) != "" || strings.TrimSpace(r.EndRepoID) != "") {
		return fmt.Errorf("start_repo_id and end_repo_id require cross_repo")
	}
	if !r.CrossRepo {
		return nil
	}
	if strings.TrimSpace(StartRepoID(&r)) == "" {
		return fmt.Errorf("cross_repo call-chain traversal requires start_repo_id or repo_id")
	}
	if strings.TrimSpace(EndRepoID(&r)) == "" {
		return fmt.Errorf("cross_repo call-chain traversal requires end_repo_id or repo_id")
	}
	return nil
}

// StartRepoID resolves the repository the traversal starts from: the
// explicit start selector in cross-repo mode, else the route repo_id.
func StartRepoID(req *Request) string {
	if req != nil && req.CrossRepo && strings.TrimSpace(req.StartRepoID) != "" {
		return req.StartRepoID
	}
	if req == nil {
		return ""
	}
	return req.RepoID
}

// EndRepoID resolves the repository the traversal ends in.
func EndRepoID(req *Request) string {
	if req != nil && req.CrossRepo && strings.TrimSpace(req.EndRepoID) != "" {
		return req.EndRepoID
	}
	if req == nil {
		return ""
	}
	return req.RepoID
}

// TraversalRepoIDs lists the distinct repositories a cross-repo
// traversal may cross, in start/end order.
func TraversalRepoIDs(req *Request) []string {
	if req == nil || !req.CrossRepo {
		return nil
	}
	repos := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, repoID := range []string{StartRepoID(req), EndRepoID(req)} {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repos = append(repos, repoID)
	}
	return repos
}

// AllowedTraversalRepoIDs resolves the repository ids that bound a
// traversal: the cross-repo pair in cross-repo mode, else the single
// route repo_id. A scoped caller still passes the grant check at the
// read; this bound is the request's own scope, not the grant.
func AllowedTraversalRepoIDs(req *Request) []string {
	if req == nil {
		return nil
	}
	if req.CrossRepo {
		return TraversalRepoIDs(req)
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		return []string{repoID}
	}
	return nil
}

// NormalizedMaxDepth floors a nonpositive traversal depth to the
// default. The ceiling lives in the route handler, which clamps above
// 10 after decoding.
func NormalizedMaxDepth(maxDepth int) int {
	if maxDepth <= 0 {
		return 5
	}
	return maxDepth
}
