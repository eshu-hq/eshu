// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// corpusStats summarizes the loaded, curated corpus for one repository.
type corpusStats struct {
	RepoID           string
	EntityRows       int
	FileRows         int
	Documents        int
	SkippedSensitive int
	SkippedExcluded  int
	SkippedNoHandle  int
}

// topRepo returns the repository id with the most indexed content entities,
// giving the benchmark its largest available corpus by default.
func topRepo(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var repoID string
	err := pool.QueryRow(ctx, `
		SELECT repo_id FROM content_entities
		GROUP BY repo_id ORDER BY count(*) DESC LIMIT 1`).Scan(&repoID)
	if err != nil {
		return "", fmt.Errorf("select top repo: %w", err)
	}
	return repoID, nil
}

// loadCorpus reads content entities and files for one repository and projects
// them into curated search documents using the shared searchdocs projection, so
// the benchmark indexes exactly the documents the reducer read-model would.
func loadCorpus(ctx context.Context, pool *pgxpool.Pool, repoID string, maxRows int) ([]searchdocs.Document, corpusStats, error) {
	stats := corpusStats{RepoID: repoID}
	docs := make([]searchdocs.Document, 0, maxRows)

	record := func(doc searchdocs.Document, decision searchdocs.Decision) {
		if decision.Include {
			docs = append(docs, doc)
			return
		}
		switch decision.Reason {
		case searchdocs.ReasonSensitiveContext:
			stats.SkippedSensitive++
		case searchdocs.ReasonExcludedSourceKind:
			stats.SkippedExcluded++
		case searchdocs.ReasonMissingStableHandle:
			stats.SkippedNoHandle++
		}
	}

	entityRows, err := pool.Query(ctx, `
		SELECT entity_id, repo_id,
		       COALESCE(relative_path, ''), COALESCE(entity_type, ''), COALESCE(entity_name, ''),
		       COALESCE(start_line, 0), COALESCE(end_line, 0),
		       COALESCE(language, ''), COALESCE(artifact_type, ''), COALESCE(source_cache, ''),
		       indexed_at
		FROM content_entities WHERE repo_id = $1 ORDER BY entity_id LIMIT $2`, repoID, maxRows)
	if err != nil {
		return nil, stats, fmt.Errorf("query entities: %w", err)
	}
	for entityRows.Next() {
		var e searchdocs.ContentEntity
		var indexedAt time.Time
		if err := entityRows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType, &e.EntityName,
			&e.StartLine, &e.EndLine, &e.Language, &e.ArtifactType, &e.SourceCache, &indexedAt); err != nil {
			entityRows.Close()
			return nil, stats, fmt.Errorf("scan entity: %w", err)
		}
		e.IndexedAt = indexedAt
		stats.EntityRows++
		record(searchdocs.ProjectContentEntity(e))
	}
	entityRows.Close()
	if err := entityRows.Err(); err != nil {
		return nil, stats, fmt.Errorf("iterate entities: %w", err)
	}

	fileRows, err := pool.Query(ctx, `
		SELECT repo_id, COALESCE(relative_path, ''), COALESCE(language, ''),
		       COALESCE(artifact_type, ''), COALESCE(content, ''), indexed_at
		FROM content_files WHERE repo_id = $1 ORDER BY relative_path LIMIT $2`, repoID, maxRows)
	if err != nil {
		return nil, stats, fmt.Errorf("query files: %w", err)
	}
	for fileRows.Next() {
		var f searchdocs.ContentFile
		var indexedAt time.Time
		if err := fileRows.Scan(&f.RepoID, &f.RelativePath, &f.Language, &f.ArtifactType, &f.Content, &indexedAt); err != nil {
			fileRows.Close()
			return nil, stats, fmt.Errorf("scan file: %w", err)
		}
		f.IndexedAt = indexedAt
		stats.FileRows++
		record(searchdocs.ProjectContentFile(f))
	}
	fileRows.Close()
	if err := fileRows.Err(); err != nil {
		return nil, stats, fmt.Errorf("iterate files: %w", err)
	}

	stats.Documents = len(docs)
	return docs, stats, nil
}

// deriveQueries builds a deterministic set of representative single-term queries
// from the most frequent entity-name tokens in the corpus, so both backends are
// measured against terms that actually occur.
func deriveQueries(docs []searchdocs.Document, count int) []string {
	freq := make(map[string]int)
	for _, doc := range docs {
		for _, token := range splitTokens(doc.Title) {
			if len(token) >= 4 {
				freq[token]++
			}
		}
	}
	type tokenFreq struct {
		token string
		count int
	}
	ranked := make([]tokenFreq, 0, len(freq))
	for token, c := range freq {
		ranked = append(ranked, tokenFreq{token: token, count: c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].token < ranked[j].token
	})
	queries := make([]string, 0, count)
	for _, tf := range ranked {
		if len(queries) >= count {
			break
		}
		queries = append(queries, tf.token)
	}
	return queries
}

func splitTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
