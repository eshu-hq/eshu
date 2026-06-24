// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchpostgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const maxBackendLimit = 100

// ContentStore is the Postgres content-search subset used by Backend.
type ContentStore interface {
	// SearchFileContent searches indexed file content in one repository.
	SearchFileContent(context.Context, string, string, int) ([]postgres.FileContentRow, error)
	// SearchEntityContent searches indexed entity source snippets in one repository.
	SearchEntityContent(context.Context, string, string, int) ([]postgres.EntityContentRow, error)
}

// Backend converts Postgres content-search rows into benchmark candidates.
type Backend struct {
	Store ContentStore
}

// Search implements searchretrieval.Backend for the Postgres keyword baseline.
func (backend Backend) Search(
	ctx context.Context,
	req searchretrieval.Request,
) ([]searchretrieval.Candidate, error) {
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return nil, err
	}
	if req.Mode != searchbench.ModeKeyword {
		return nil, fmt.Errorf("postgres content retrieval requires keyword mode: mode=%q", req.Mode)
	}
	if backend.Store == nil {
		return nil, errors.New("postgres content search store is required")
	}
	repoID := strings.TrimSpace(req.Scope.RepoID)
	if req.Scope.Anchor().Kind != searchretrieval.ScopeKindRepo || repoID == "" {
		return nil, fmt.Errorf(
			"postgres content retrieval requires repository scope: anchor=%s:%s",
			req.Scope.Anchor().Kind,
			req.Scope.Anchor().ID,
		)
	}

	limit := backendLimit(req.Limit)
	entities, err := backend.Store.SearchEntityContent(ctx, strings.TrimSpace(req.Query), repoID, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres entity content search: %w", err)
	}
	files, err := backend.Store.SearchFileContent(ctx, strings.TrimSpace(req.Query), repoID, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres file content search: %w", err)
	}

	candidates := make([]searchretrieval.Candidate, 0, len(entities)+len(files))
	rank := 0
	for _, entity := range entities {
		doc, decision := searchdocs.ProjectContentEntity(searchdocs.ContentEntity{
			EntityID:     entity.EntityID,
			RepoID:       entity.RepoID,
			RelativePath: entity.RelativePath,
			EntityType:   entity.EntityType,
			EntityName:   entity.EntityName,
			StartLine:    entity.StartLine,
			EndLine:      entity.EndLine,
			Language:     entity.Language,
			ArtifactType: entity.ArtifactType,
			SourceCache:  entity.SourceCache,
		})
		if !decision.Include {
			continue
		}
		candidates = append(candidates, candidate(doc, rank, "content_entities"))
		rank++
	}
	for _, file := range files {
		doc, decision := searchdocs.ProjectContentFile(searchdocs.ContentFile{
			RepoID:       file.RepoID,
			RelativePath: file.RelativePath,
			Language:     file.Language,
			ArtifactType: file.ArtifactType,
			Content:      file.Content,
		})
		if !decision.Include {
			continue
		}
		candidates = append(candidates, candidate(doc, rank, "content_files"))
		rank++
	}
	return candidates, nil
}

func backendLimit(limit int) int {
	if limit < maxBackendLimit {
		return limit + 1
	}
	return maxBackendLimit
}

func candidate(doc searchdocs.Document, rank int, sourceTable string) searchretrieval.Candidate {
	return searchretrieval.Candidate{
		Document: doc,
		Score:    scoreForRank(rank),
		Metadata: map[string]string{
			"backend":      string(searchbench.BackendPostgresContentSearch),
			"source_table": sourceTable,
		},
	}
}

func scoreForRank(rank int) float64 {
	return 1 / float64(rank+1)
}
