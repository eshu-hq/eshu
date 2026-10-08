// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// contentSearchRequest is the decoded body of the two content search routes.
type contentSearchRequest struct {
	RepoID  string   `json:"repo_id"`
	RepoIDs []string `json:"repo_ids"`
	Query   string   `json:"query"`
	Pattern string   `json:"pattern"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

func readContentSearchRequest(r *http.Request) (contentSearchRequest, error) {
	var req contentSearchRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		return contentSearchRequest{}, err
	}
	return req, nil
}

func (req contentSearchRequest) validate() error {
	if req.pattern() == "" {
		return errors.New("query is required")
	}
	if req.Offset > ContentSearchMaxOffset {
		return fmt.Errorf("offset exceeds maximum of %d", ContentSearchMaxOffset)
	}
	return nil
}

func (req contentSearchRequest) repoID() string {
	if req.RepoID != "" {
		return req.RepoID
	}
	if len(req.RepoIDs) == 1 {
		return req.RepoIDs[0]
	}
	return ""
}

func (req contentSearchRequest) pattern() string {
	if req.Query != "" {
		return req.Query
	}
	return req.Pattern
}

func (req contentSearchRequest) limit() int {
	if req.Limit <= 0 {
		return ContentSearchDefaultLimit
	}
	if req.Limit > ContentSearchMaxLimit {
		return ContentSearchMaxLimit
	}
	return req.Limit
}

func (req contentSearchRequest) offset() int {
	if req.Offset < 0 {
		return 0
	}
	return req.Offset
}

func (req contentSearchRequest) explicitRepoIDs() []string {
	if req.RepoID != "" {
		return nil
	}

	repoIDs := make([]string, 0, len(req.RepoIDs))
	seen := make(map[string]struct{}, len(req.RepoIDs))
	for _, repoID := range req.RepoIDs {
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	return repoIDs
}
