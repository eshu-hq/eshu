// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// A graph fake that judges CLAUSE ATTACHMENT for the #5167 batch-2b story and
// call-chain reads.
//
// evaluatingRepositoryGraph (code_graph_grant_evaluating_fake_test.go) answers
// the same question for the batch-1 routes, but it reads the statement against a
// `<alias>:Repository` binding, and these reads do not bind their grant there.
// They bind it on the driving row's own repo_id, because the Repository aliases
// only ever appear inside OPTIONAL MATCH clauses whose WHERE decides nothing --
// measured against the pinned backend and written up in
// docs/internal/evidence/5167-code-family-batch-2b.md.
//
// storyClauseGraph models the backend's actual behaviour rather than the
// statement's apparent meaning:
//
//   - a predicate in the WHERE attached to the ANCHORING MATCH decides row
//     membership;
//   - a predicate in a WHERE that follows an OPTIONAL MATCH decides nothing. It
//     nulls that optional pattern's own columns and the row survives.
//
// Move a grant predicate back onto the trailing WHERE and the out-of-grant row
// reappears in the response body, which is what the route tests assert against.
// A substring assertion cannot tell those two statements apart.

// storyGrantSeed is one row the fake can return. repoByAlias says which
// repository each node alias in the statement resolves to, so one seed can
// answer for `anchor`/`target`, `source`/`anchor`, or `class`/`method`. An
// alias the seed does not name is treated as unconstrained, matching the fake's
// rule that an unrecognised predicate admits the row.
type storyGrantSeed struct {
	repoByAlias map[string]string
	row         map[string]any
}

// storyClauseGraph is a GraphQuery whose answers depend on where the repository
// predicates sit in the statement it is handed.
type storyClauseGraph struct {
	seeds []storyGrantSeed
	// optionalColumns are the projected columns bound by the OPTIONAL MATCH
	// clauses. A predicate stranded on one of those clauses nulls exactly these
	// and keeps the row.
	optionalColumns []string

	// mu guards the recorded statements and params. One story request reaches
	// this fake from several goroutines at once: relationshipStoryGraphRows
	// runs the incoming and outgoing reads in parallel when direction is
	// "both", and relationshipStoryClassHierarchy runs its three enrichment
	// reads in parallel. seeds and optionalColumns are set at construction and
	// only read afterwards, so they stay outside the lock.
	mu         sync.Mutex
	statements []string
	params     []map[string]any
}

func (g *storyClauseGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.mu.Lock()
	g.statements = append(g.statements, cypher)
	g.params = append(g.params, params)
	g.mu.Unlock()
	return g.evaluate(cypher, params), nil
}

// recordedStatements returns the statements the fake has been handed so far.
// Callers that read the record while any read may still be in flight must go
// through this rather than the field. The parallel directions finish in
// whichever order the scheduler picks, so an assertion must not depend on the
// position of a statement in this slice.
func (g *storyClauseGraph) recordedStatements() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.statements)
}

func (g *storyClauseGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (g *storyClauseGraph) evaluate(cypher string, params map[string]any) []map[string]any {
	anchoring, stranded := storyClausePredicates(cypher)
	rows := make([]map[string]any, 0, len(g.seeds))
	for _, seed := range g.seeds {
		if !storySeedAdmits(seed, anchoring, params) {
			continue
		}
		row := cloneGraphRow(seed.row)
		if !storySeedAdmits(seed, stranded, params) {
			for _, column := range g.optionalColumns {
				row[column] = nil
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func storySeedAdmits(seed storyGrantSeed, predicates []string, params map[string]any) bool {
	for _, predicate := range predicates {
		if !storyPredicateAdmits(predicate, seed.repoByAlias, params) {
			return false
		}
	}
	return true
}

// storyClausePredicates splits a statement's repository predicates by the clause
// they are attached to: the WHERE on the anchoring MATCH, and any WHERE that
// follows an OPTIONAL MATCH. The second list is what the pinned backend
// evaluates against the optional pattern only.
func storyClausePredicates(cypher string) (anchoring []string, stranded []string) {
	normalized := normalizeCypherWhitespace(cypher)
	seenOptional := false
	for _, token := range storyClauseTokens(normalized) {
		switch {
		case strings.HasPrefix(token, "OPTIONAL MATCH "):
			seenOptional = true
		case strings.HasPrefix(token, "WHERE "):
			predicates := storySplitPredicates(strings.TrimPrefix(token, "WHERE "))
			if seenOptional {
				stranded = append(stranded, predicates...)
				continue
			}
			anchoring = append(anchoring, predicates...)
		}
	}
	return anchoring, stranded
}

// storyClauseTokens cuts a normalized statement at every clause keyword.
func storyClauseTokens(normalized string) []string {
	keywords := []string{"OPTIONAL MATCH ", "MATCH ", "WHERE ", "WITH ", "RETURN ", "ORDER BY ", "SKIP ", "LIMIT "}
	tokens := make([]string, 0, 8)
	for index := 0; index < len(normalized); {
		next, keyword := storyNextClause(normalized, index+1, keywords)
		if next < 0 {
			tokens = append(tokens, normalized[index:])
			break
		}
		tokens = append(tokens, normalized[index:next])
		index = next
		_ = keyword
	}
	return tokens
}

func storyNextClause(normalized string, from int, keywords []string) (int, string) {
	earliest, found := -1, ""
	for _, keyword := range keywords {
		at := strings.Index(normalized[from:], keyword)
		if at < 0 {
			continue
		}
		at += from
		// A MATCH that is the tail of OPTIONAL MATCH is not its own clause.
		if keyword == "MATCH " && at >= len("OPTIONAL ") &&
			strings.HasSuffix(normalized[:at], "OPTIONAL ") {
			continue
		}
		if earliest < 0 || at < earliest {
			earliest, found = at, keyword
		}
	}
	return earliest, found
}

// storySplitPredicates splits an AND-joined predicate list, keeping a
// parenthesised grant condition -- which contains its own OR -- in one piece.
func storySplitPredicates(block string) []string {
	parts := strings.Split(block, " AND ")
	predicates := make([]string, 0, len(parts))
	for _, part := range parts {
		predicates = append(predicates, strings.TrimSpace(part))
	}
	return predicates
}

// storyPredicateAdmits evaluates one repository predicate against a seed. A
// predicate this fake does not recognise admits the row: seeded rows are built
// to satisfy every non-repository predicate the builders emit, so guessing at
// those would invent a filter the backend does not apply.
func storyPredicateAdmits(predicate string, repoByAlias map[string]string, params map[string]any) bool {
	for alias, repoID := range repoByAlias {
		switch {
		case strings.Contains(predicate, alias+".repo_id IN $allowed_repository_ids"):
			return graphParamContains(params, "allowed_repository_ids", repoID) ||
				graphParamContains(params, "allowed_scope_ids", repoID)
		case strings.Contains(predicate, alias+".repo_id = $repo_id"):
			bound, _ := params["repo_id"].(string)
			return repoID == bound && repoID != ""
		case strings.Contains(predicate, alias+".repo_id, '') IN $traversal_repo_ids"):
			return graphParamContains(params, "traversal_repo_ids", repoID)
		}
	}
	return true
}

// storyGrantContentStore answers the story route's target resolution. It records
// every repository the candidate lookup asked about, so a test can assert the
// grant reached the read rather than only that the answer looked right.
type storyGrantContentStore struct {
	fakePortContentStore
	entities    map[string]EntityContent
	byName      []EntityContent
	askedRepo   []string
	askedEntity []string
	anyRepo     bool
}

func (s *storyGrantContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	s.askedEntity = append(s.askedEntity, entityID)
	entity, ok := s.entities[entityID]
	if !ok {
		return nil, nil
	}
	return &entity, nil
}

func (s *storyGrantContentStore) SearchEntitiesByName(
	_ context.Context,
	repoID, _, name string,
	limit int,
) ([]EntityContent, error) {
	s.askedRepo = append(s.askedRepo, repoID)
	return s.matches(repoID, name, limit), nil
}

func (s *storyGrantContentStore) SearchEntitiesByNameAnyRepo(
	_ context.Context,
	_, name string,
	limit int,
) ([]EntityContent, error) {
	s.anyRepo = true
	return s.matches("", name, limit), nil
}

func (s *storyGrantContentStore) SearchEntitiesByLanguageAndType(
	_ context.Context,
	repoID, _, _, query string,
	limit int,
) ([]EntityContent, error) {
	s.askedRepo = append(s.askedRepo, repoID)
	return s.matches(repoID, query, limit), nil
}

// reachedTheStore reports whether any content read was issued, including the
// entity-id lookup. An empty-grant caller must not reach any of them.
func (s *storyGrantContentStore) reachedTheStore() bool {
	return len(s.askedRepo) > 0 || len(s.askedEntity) > 0 || s.anyRepo
}

// matches mirrors the shipped SQL: an explicit repository anchors the scan and
// an empty one does not restrict it at all. That is what makes the leak
// assertion fail when the handler stops binding the grant.
func (s *storyGrantContentStore) matches(repoID, name string, limit int) []EntityContent {
	rows := make([]EntityContent, 0, len(s.byName))
	for _, entity := range s.byName {
		if repoID != "" && entity.RepoID != repoID {
			continue
		}
		if name != "" && entity.EntityName != name {
			continue
		}
		if len(rows) >= limit {
			break
		}
		rows = append(rows, entity)
	}
	return rows
}

func (s *storyGrantContentStore) askedRepositories() []string {
	asked := slices.Clone(s.askedRepo)
	slices.Sort(asked)
	return slices.Compact(asked)
}
