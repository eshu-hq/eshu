// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// semanticSearchLanguages parses, normalises, and validates a raw languages
// slice from the request. Each value is lowercased and trimmed; empty entries
// are dropped. An unknown language key (not in the default parser registry)
// is rejected with a descriptive error so the caller can return HTTP 400.
//
// An empty input returns a nil slice meaning "no language filter".
func semanticSearchLanguages(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	registry := parser.DefaultRegistry()
	langs := make([]string, 0, len(raw))
	for _, value := range raw {
		lang := strings.ToLower(strings.TrimSpace(value))
		if lang == "" {
			continue
		}
		if !registry.IsRegisteredLanguage(lang) {
			return nil, fmt.Errorf("languages contains unknown value %q", value)
		}
		langs = append(langs, lang)
	}
	return langs, nil
}

func normalizeSemanticSearchRequest(req semanticSearchRequest) semanticSearchRequest {
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.Query = strings.TrimSpace(req.Query)
	req.Mode = strings.TrimSpace(req.Mode)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.WorkloadID = strings.TrimSpace(req.WorkloadID)
	req.Environment = strings.TrimSpace(req.Environment)
	for i, kind := range req.SourceKinds {
		req.SourceKinds[i] = strings.TrimSpace(kind)
	}
	for i, lang := range req.Languages {
		req.Languages[i] = strings.TrimSpace(lang)
	}
	return req
}

func semanticSearchRetrievalRequest(body semanticSearchRequest) (searchretrieval.Request, error) {
	if body.RepoID == "" {
		return searchretrieval.Request{}, fmt.Errorf("repo_id is required")
	}
	req := searchretrieval.Request{
		Query: body.Query,
		Scope: searchretrieval.Scope{
			ServiceID:   body.ServiceID,
			WorkloadID:  body.WorkloadID,
			RepoID:      body.RepoID,
			Environment: body.Environment,
		},
		Mode:    searchbench.Mode(body.Mode),
		Limit:   body.Limit,
		Timeout: time.Duration(body.TimeoutMS) * time.Millisecond,
	}
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return searchretrieval.Request{}, err
	}
	return req, nil
}

func semanticSearchSourceKinds(raw []string) ([]searchdocs.SourceKind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	kinds := make([]searchdocs.SourceKind, 0, len(raw))
	for _, value := range raw {
		switch kind := searchdocs.SourceKind(strings.TrimSpace(value)); kind {
		case searchdocs.SourceKindCodeEntity,
			searchdocs.SourceKindRepositoryFile,
			searchdocs.SourceKindRuntimeSummary,
			searchdocs.SourceKindSemanticContext:
			kinds = append(kinds, kind)
		case "":
			continue
		default:
			return nil, fmt.Errorf("source_kinds contains unsupported value %q", value)
		}
	}
	return kinds, nil
}

func emptySemanticSearchResponse(req searchretrieval.Request) semanticSearchResponse {
	return semanticSearchResponse{
		Query:          req.Query,
		RepoID:         req.Scope.RepoID,
		Anchor:         req.Scope.Anchor(),
		Mode:           req.Mode,
		SearchMode:     string(req.Mode),
		Limit:          req.Limit,
		TimeoutMS:      int(req.Timeout / time.Millisecond),
		Results:        []semanticSearchResult{},
		CorpusLimit:    0,
		Truncated:      false,
		RetrievalState: defaultSemanticSearchRetrievalState(req.Mode),
		Facets:         semanticSearchFacets{Languages: map[string]int{}},
	}
}
