// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

func (h *CodeHandler) resolveRelationshipStoryTarget(
	ctx context.Context,
	req relationshipStoryRequest,
) (relationshipStoryResolution, *EntityContent, error) {
	target := req.EffectiveTarget()
	if relationshipStoryGrantBlocked(ctx, req) {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	if entityID := strings.TrimSpace(req.EntityID); entityID != "" {
		resolution := relationshipStoryResolution{
			Status:   "resolved",
			Target:   target,
			EntityID: entityID,
			RepoID:   strings.TrimSpace(req.RepoID),
			Language: strings.TrimSpace(req.Language),
		}
		if h != nil && h.Content != nil {
			entity, err := h.Content.GetEntityContent(ctx, entityID)
			if err != nil {
				return resolution, nil, err
			}
			if entity != nil {
				access := codeGrantAccessFilter(ctx)
				if strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(entity.RepoID) != strings.TrimSpace(req.RepoID) {
					return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
				if !access.AllowsRepositoryID(strings.TrimSpace(entity.RepoID)) {
					return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
				resolution.Name = entity.EntityName
				resolution.RepoID = entity.RepoID
				resolution.Language = entity.Language
				return resolution, entity, nil
			}
		}
		return resolution, &EntityContent{EntityID: entityID, EntityName: target, RepoID: req.RepoID}, nil
	}
	if h == nil || h.Content == nil {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}

	candidates, err := h.relationshipStoryCandidates(ctx, req)
	if err != nil {
		return relationshipStoryResolution{}, nil, err
	}
	if len(candidates) == 0 {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	candidates = exactEntityNameMatches(candidates, target)
	if req.NormalizedQueryType() == "class_hierarchy" {
		candidates = relationshipStoryClassHierarchyCandidates(candidates)
	}
	if len(candidates) == 0 {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	sortRelationshipStoryCandidates(candidates)
	limit := req.NormalizedLimit()
	truncated := len(candidates) > limit
	if len(candidates) != 1 {
		return relationshipStoryResolution{
			Status:     "ambiguous",
			Target:     target,
			RepoID:     strings.TrimSpace(req.RepoID),
			Language:   strings.TrimSpace(req.Language),
			Candidates: relationshipStoryCandidateMaps(candidates, limit),
			Truncated:  truncated,
		}, nil, nil
	}
	entity := candidates[0]
	return relationshipStoryResolution{
		Status:   "resolved",
		Target:   target,
		EntityID: entity.EntityID,
		Name:     entity.EntityName,
		RepoID:   entity.RepoID,
		Language: entity.Language,
	}, &entity, nil
}

func relationshipStoryClassHierarchyCandidates(candidates []EntityContent) []EntityContent {
	out := make([]EntityContent, 0, len(candidates))
	for _, candidate := range candidates {
		if relationshipStoryClassHierarchyEntityType(candidate.EntityType) {
			out = append(out, candidate)
		}
	}
	return out
}

func relationshipStoryClassHierarchyEntityType(entityType string) bool {
	switch strings.ToLower(strings.TrimSpace(entityType)) {
	case "class", "interface", "trait", "struct", "enum", "protocol":
		return true
	default:
		return false
	}
}

func (h *CodeHandler) relationshipStoryCandidates(
	ctx context.Context,
	req relationshipStoryRequest,
) ([]EntityContent, error) {
	allowed, blocked := codeContentGrantScope(ctx, req.RepoID)
	if blocked {
		return nil, nil
	}
	return relationshipStoryGrantedCandidates(ctx, h.Content, req, allowed)
}

// The candidate sort/map shapers moved to
// codemodel/code_relationships_resolution.go (#6060 lane A L1) with the
// name-target resolver that renders through them; the staying resolver
// calls them through the family_code_shim.go forwards.

// relationshipStoryGrantedCandidates resolves the target-name lookup for
// POST /api/v0/code/relationships/story with the caller's repository grant
// bound at the read.
//
// The lookup used to end at SearchEntitiesByNameAnyRepo whenever the request
// named no repo_id, and its rows become the `ambiguous` response's candidate
// list -- entity ids, names, file paths and repository ids, one per match. A
// scoped caller who named a symbol that exists in more than one tenant read the
// other tenant's copy straight out of that list, without ever resolving a
// story.
//
// The three branches match what the route already did, each with the grant
// added:
//
//   - language named: the shared grant-bound entity search, which pushes the
//     granted repository ids into the statement's own WHERE when the store can
//     take them and otherwise asks one granted repository at a time.
//   - repo_id named: applyRepositorySelectorForCapability already resolved it
//     against the grant, so an ungranted one never reaches here.
//   - neither: a scoped caller reads its granted repositories one at a time
//     rather than the whole corpus, the same fallback shape
//     symbolNameFallbackEntities (code_symbol.go) uses. An unscoped caller
//     keeps the corpus-wide read.
//
// The two branches that carry no language ask for the target name EXACTLY
// (#6555). They used to ask for it as a substring and let the caller discard
// everything that was not the name, which is a page of rows spent on an
// answer nobody wanted: a repository holding more than `limit` near-misses
// -- PaymentGatewayFactory, PaymentGatewayBuilder, PaymentGatewayAdapter --
// filled the page and left the exact PaymentGateway unread, and the route
// answered not_found for a symbol in the caller's own granted repository.
// #6553 gave each granted repository its own budget, which fixed the same
// defect ACROSS repositories; it could not fix it inside one, because there
// the page is full before the exact row is reached no matter how the budget
// is divided.
//
// The language branch still reads substrings. Its store contract is shared
// with POST /api/v0/code/language-query, where matching a substring is the
// feature -- see the note on searchEntitiesForGrant's fallback loop below.
func relationshipStoryGrantedCandidates(
	ctx context.Context,
	content ContentStore,
	req relationshipStoryRequest,
	allowed []string,
) ([]EntityContent, error) {
	limit := req.NormalizedLimit() + 1
	target := req.EffectiveTarget()
	repoID := strings.TrimSpace(req.RepoID)
	// SCOPE LIMIT, stated because this branch is NOT fixed by #6555.
	//
	// When the request names a language, resolution still goes through
	// SearchEntitiesByLanguageAndTypeForAccess, whose statement is
	// `entity_name ILIKE $n` (content_reader_entity_search.go:159) under one
	// shared LIMIT. That is the original #6555 defect shape: a page of
	// near-misses can fill the budget before the exact symbol is reached, and
	// exactEntityNameMatches then discards them all, so the caller is told the
	// target does not exist when it does.
	//
	// The exact-name reads below cover the other three routes (repo-bound,
	// any-repo, and per-granted-repository). Extending them here needs an
	// exact-name variant of the language+type read that keeps the language and
	// entity-type filters, which is a wider change than this one; it is a known
	// follow-up rather than an oversight. Raised independently by two reviewers
	// on PR #6605, both scoring it non-blocking.
	if language := strings.TrimSpace(req.Language); language != "" {
		return searchEntitiesForGrant(ctx, content, languageEntitySearch{
			RepoID:               repoID,
			Language:             language,
			Query:                target,
			Limit:                limit,
			AllowedRepositoryIDs: allowed,
		})
	}
	if repoID != "" {
		return content.SearchEntitiesByExactName(ctx, repoID, "", target, limit)
	}
	if len(allowed) == 0 {
		return content.SearchEntitiesByExactNameAnyRepo(ctx, "", target, limit)
	}
	return relationshipStoryExactCandidatesPerRepository(ctx, content, target, allowed, limit)
}

// relationshipStoryExactCandidatesPerRepository reads the granted repositories
// one at a time, asking each for the exact target name.
//
// Two bounds hold it together, and they were added by different changes.
// #6553 gave each repository its own budget rather than a share of one, so a
// first repository could no longer spend the whole page and leave a later
// repository unread. #6555 changed what is read: SearchEntitiesByExactName
// asks `entity_name = $n` instead of `entity_name ILIKE '%' || $n || '%'`, so
// the rows that come back are the rows the caller keeps. Before it, a single
// repository with more than `limit` near-misses returned a full page of them
// and the exact symbol was never read at all -- a case no budgeting reaches,
// because there is only one repository to budget.
//
// The bound, stated because it is looser than the pre-#6553 one: at most
// `limit` rows are read per granted repository, and the walk stops as soon as
// `limit` matches are in hand, so the worst case is `limit` rows times the
// number of granted repositories read, and at most `limit` returned. The old
// shape read at most `limit` rows in total, and answered wrongly. In practice
// an exact name resolves in the first repository that holds it.
//
// exactEntityNameMatches still runs on each page, and it is what keeps the
// per-repository budget honest for a store that does not answer the question it
// was asked -- resolveRelationshipStoryTarget's own filter would catch those
// rows, but only after they had already spent the budget.
//
// PRECONDITION, not a guarantee: the filter compares
// `strings.TrimSpace(EntityName)`, while the read now compares
// `entity_name = $1` with no trim. Those agree only while stored names carry no
// surrounding whitespace, and the write path does NOT enforce that --
// `content/shape/materialize.go:219` stores `EntityName: indexed.item.Name`
// verbatim, though its sibling fields on the same struct (RepoID:87,
// SourceSystem:94, path:121, Digest:129) are all TrimSpace'd. So a parser
// emitting " Foo " persists it, the old ILIKE read plus TrimSpace filter matched
// it, and this exact-equality read does not return it at all -- no post-filter
// can rescue a row SQL never yields.
//
// An earlier version of this comment said the read makes the filter "a no-op",
// which asserted that precondition as a fact. Raised in review of #6605 and
// confirmed by tracing the write path. Resolving it means either trimming at
// write (matching those four siblings), or a functional index on
// btrim(entity_name); a btrim predicate alone would defeat the plain-equality
// index seek this change exists to get, so it is not done here.
//
// searchEntitiesForGrant's legacy-store fallback below spends a shared budget
// on substring rows and deliberately keeps them unfiltered -- see the note on
// that loop for why the same fix does not belong there.
func relationshipStoryExactCandidatesPerRepository(
	ctx context.Context,
	content ContentStore,
	target string,
	allowed []string,
	limit int,
) ([]EntityContent, error) {
	candidates := make([]EntityContent, 0, limit)
	for _, allowedRepoID := range allowed {
		if len(candidates) >= limit {
			break
		}
		rows, err := content.SearchEntitiesByExactName(ctx, allowedRepoID, "", target, limit)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, exactEntityNameMatches(rows, target)...)
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

// searchEntitiesForGrant runs one grant-bound content-store entity lookup for
// the relationship-story target resolution above.
//
// It lives here, and not beside the read it mirrors, for a gate reason rather
// than a design one. Its twin is LanguageQueryHandler.searchLanguageEntities in
// language_query_metadata.go, and the two dispatch identically.
// scripts/verify-parser-relationship-kit.sh classifies every
// go/internal/query/language*.go path as Language Query DSL source and fails
// any change to one that does not also update
// docs/public/reference/language-query-dsl.md. That classification is by path,
// not by content, so even reducing the DSL read to a one-line call into a
// shared helper would demand a DSL-doc update for a change the DSL contract
// never saw. Keeping the story route's copy on this side of the line leaves
// language_query_metadata.go byte-identical to main.
//
// The duplication that buys is real and deliberate, so it is pinned rather than
// trusted: TestSearchEntitiesForGrantMatchesTheLanguageQueryRead drives both
// functions over one fake store across all three branches below and fails if
// either side changes alone. A new file would have been the tidier home, but
// internal/query is grandfathered in the dirgate ledger at exactly 787 non-test
// .go files, and a 788th fails that gate with no //nolint escape.
//
// The three branches are the store's, not this route's. A store that satisfies
// languageEntityContentSearcher takes the grant into its own statement, so one
// read serves the whole granted set and the LIMIT page is taken from it. A
// store that does not -- a test fake, or an older implementation -- can only be
// asked about one repository at a time, so a corpus-wide scoped search iterates
// the granted repositories rather than asking for repository "", which the
// unrestricted statement answers with every tenant's rows.
func searchEntitiesForGrant(
	ctx context.Context,
	content ContentStore,
	search languageEntitySearch,
) ([]EntityContent, error) {
	if content == nil {
		return nil, fmt.Errorf("content reader is required for %s queries", search.EntityType)
	}
	if searcher, ok := content.(languageEntityContentSearcher); ok {
		return searcher.SearchEntitiesByLanguageAndTypeForAccess(ctx, search)
	}
	if search.RepoID != "" || len(search.AllowedRepositoryIDs) == 0 {
		return content.SearchEntitiesByLanguageAndType(
			ctx, search.RepoID, search.Language, search.EntityType, search.Query, search.Limit,
		)
	}
	// This loop spends a shared budget the way the no-language branch used to,
	// and appends substring rows unfiltered. So a first granted repository
	// holding more than Limit near-misses can hide an exact symbol further down
	// the grant -- for the ONE caller that goes on to filter to exact names,
	// which is relationshipStoryGrantedCandidates with a language named.
	//
	// Mirroring the exact pre-filter here would be wrong, and the twin's callers
	// are what say so. queryContentByLanguage hands these rows straight to
	// POST /api/v0/code/language-query as results, where matching a substring is
	// the feature: filtering to exact names would stop a search for "Handler"
	// returning "HandlerFactory". enrichLanguageResultsWithContentMetadata keys
	// them into a merge map by (repository, path, type, name, line) to attach
	// metadata to rows the GRAPH returned, so narrowing the read would drop
	// merges for every result whose name is not the query. Both are in
	// language_query_metadata.go, where this function's byte-identical twin
	// lives; the parity test pins the two together, and editing that file also
	// re-trips the parser-relationship-kit lane.
	//
	// So the shape is disclosed rather than mirrored, and the fix belongs to the
	// caller that wants exact names rather than to this shared read. #6555 made
	// that fix for the two no-language branches, which have a read of their own
	// to change; this loop keeps the substring rows its twin's callers need.
	// Its reach today is nil -- *ContentReader satisfies
	// languageEntityContentSearcher and takes the branch above, so this loop
	// runs only for a fake or an older store.
	entities := make([]EntityContent, 0, search.Limit)
	for _, repoID := range search.AllowedRepositoryIDs {
		if len(entities) >= search.Limit {
			break
		}
		rows, err := content.SearchEntitiesByLanguageAndType(
			ctx, repoID, search.Language, search.EntityType, search.Query, search.Limit-len(entities),
		)
		if err != nil {
			return nil, err
		}
		entities = append(entities, rows...)
	}
	return entities, nil
}

// relationshipStoryGrantBlocked reports whether the caller's grant admits
// nothing, so the route must answer its own not-found story without reading a
// backend.
//
// not_found rather than an error or an empty-but-distinguishable shape: it is
// the same answer a target that does not exist produces, so a grantless caller
// cannot use this route to probe which symbols the index holds.
func relationshipStoryGrantBlocked(ctx context.Context, req relationshipStoryRequest) bool {
	_, blocked := codeContentGrantScope(ctx, req.RepoID)
	return blocked
}
