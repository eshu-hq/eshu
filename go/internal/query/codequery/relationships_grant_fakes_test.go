// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// relGrantContentStore is the content-store double for the #5167
// POST /api/v0/code/relationships grant proofs. It answers the four reads the
// route's content fallback makes, with the same predicate order the shipped
// SQL uses (repository filter, then name, then LIMIT), and records every
// repository it was asked about so a test can prove a scoped caller never
// reached a corpus-wide read.
type relGrantContentStore struct {
	content.FakePortContentStore

	entities []EntityContent

	mu           sync.Mutex
	anyRepoReads int
	repoReads    []string
	reads        int
}

func (s *relGrantContentStore) record(repoID string, anyRepo bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if anyRepo {
		s.anyRepoReads++
		return
	}
	s.repoReads = append(s.repoReads, repoID)
}

// GetEntityContent returns the seeded entity with the requested id.
func (s *relGrantContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	for _, entity := range s.entities {
		if entity.EntityID == entityID {
			found := entity
			return &found, nil
		}
	}
	return nil, nil
}

// SearchEntitiesByName mirrors `repo_id = $1 AND entity_name ILIKE %name%`.
func (s *relGrantContentStore) SearchEntitiesByName(_ context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error) {
	s.record(repoID, false)
	return s.match(repoID, entityType, name, limit, false), nil
}

// SearchEntitiesByNameAnyRepo mirrors the corpus-wide substring read.
func (s *relGrantContentStore) SearchEntitiesByNameAnyRepo(_ context.Context, entityType, name string, limit int) ([]EntityContent, error) {
	s.record("", true)
	return s.match("", entityType, name, limit, false), nil
}

// SearchEntitiesByExactName mirrors `repo_id = $1 AND entity_name = $2`.
func (s *relGrantContentStore) SearchEntitiesByExactName(_ context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error) {
	s.record(repoID, false)
	return s.match(repoID, entityType, name, limit, true), nil
}

// SearchEntitiesByExactNameAnyRepo mirrors the corpus-wide exact read.
func (s *relGrantContentStore) SearchEntitiesByExactNameAnyRepo(_ context.Context, entityType, name string, limit int) ([]EntityContent, error) {
	s.record("", true)
	return s.match("", entityType, name, limit, true), nil
}

func (s *relGrantContentStore) match(repoID, entityType, name string, limit int, exact bool) []EntityContent {
	out := make([]EntityContent, 0, limit)
	for _, entity := range s.entities {
		if repoID != "" && entity.RepoID != repoID {
			continue
		}
		if entityType != "" && entity.EntityType != entityType {
			continue
		}
		if exact && entity.EntityName != name {
			continue
		}
		if !exact && !strings.Contains(strings.ToLower(entity.EntityName), strings.ToLower(name)) {
			continue
		}
		out = append(out, entity)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// relGrantContentBuilder stands in for root's ContentIndexRelationshipBuilder.
// Every production neighbour read it makes is bound to the anchor entity's own
// repo_id (`WHERE repo_id = $1` in SearchEntitiesByName,
// SearchEntitiesReferencingComponent, ListRepoEntitiesByType and ListRepoFiles),
// so this double emits one neighbour in the anchor's repository and names the
// anchor, which is what makes a fallback leak visible in the response body.
type relGrantContentBuilder struct {
	mu    sync.Mutex
	built []string
}

// BuildContentRelationships returns one same-repository neighbour.
func (b *relGrantContentBuilder) BuildContentRelationships(
	_ context.Context,
	_ querycontract.ContentStore,
	entity EntityContent,
	_ *slog.Logger,
) (querycontract.ContentRelationshipSet, error) {
	b.mu.Lock()
	b.built = append(b.built, entity.EntityID)
	b.mu.Unlock()
	return querycontract.ContentRelationshipSet{
		Outgoing: []map[string]any{{
			"type":        "REFERENCES",
			"target_name": entity.EntityName + "Neighbour",
			"target_id":   entity.EntityID + ":neighbour",
			"reason":      "jsx_component_usage",
		}},
	}, nil
}

func (b *relGrantContentBuilder) builtCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.built)
}

// relGrantCountingGraph answers every graph read with no rows and counts the
// calls, which is the graph state an ungranted anchor produces once the
// metadata read is grant-bound: the handler then falls through to the content
// fallback under test.
type relGrantCountingGraph struct {
	mu    sync.Mutex
	calls int
}

func (g *relGrantCountingGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return nil, nil
}

func (g *relGrantCountingGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return nil, nil
}

func (g *relGrantCountingGraph) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}
