// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Predicate evaluators for the grant fake, split out of grantfake.go to keep
// both files under the repository's 500-line cap.

// callChainHopAdmits evaluates one hop predicate. The request's own hop bounds
// coalesce node.repo_id against the empty string, the grant's reads the bare
// property, and neither matches storyPredicateAdmits' per-alias keys, so they
// need their own matcher.
//
// The Cypher literal is spelled out rather than quoted because gofmt reformats
// doc comments and turns a pair of single quotes into a typographic quote pair.
//
// A grant term is judged on the property it names (#7220): `.id IN $allowed_*`
// against the node's own id, anything else against its repo_id. Reading an id
// term as a repo_id test would hide a predicate that admits a non-Repository
// node whose id collides with a granted repository id.
func callChainHopAdmits(predicate string, node GrantEntity, params map[string]any) bool {
	repoID := node.RepoID
	switch {
	case strings.Contains(predicate, ".id IN $allowed_repository_ids"):
		return querycontract.GraphParamContains(params, "allowed_repository_ids", node.UID) ||
			querycontract.GraphParamContains(params, "allowed_scope_ids", node.UID)
	case strings.Contains(predicate, "IN $allowed_repository_ids"):
		return querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) ||
			querycontract.GraphParamContains(params, "allowed_scope_ids", repoID)
	case strings.Contains(predicate, "IN $traversal_repo_ids"):
		return querycontract.GraphParamContains(params, "traversal_repo_ids", repoID)
	case strings.Contains(predicate, "= $repo_id"):
		bound, _ := params["repo_id"].(string)
		return repoID == bound && repoID != ""
	default:
		return true
	}
}

// storyPredicateAdmits evaluates one repository predicate against a seed. A
// predicate this fake does not recognise admits the row. Copy of
// story_grant_clause_fake_test.go.
func storyPredicateAdmits(predicate string, repoByAlias map[string]string, params map[string]any) bool {
	for alias, repoID := range repoByAlias {
		switch {
		case strings.Contains(predicate, alias+".repo_id IN $allowed_repository_ids"):
			return querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) ||
				querycontract.GraphParamContains(params, "allowed_scope_ids", repoID)
		case strings.Contains(predicate, alias+".repo_id = $repo_id"):
			bound, _ := params["repo_id"].(string)
			return repoID == bound && repoID != ""
		case strings.Contains(predicate, alias+".repo_id, '') IN $traversal_repo_ids"):
			return querycontract.GraphParamContains(params, "traversal_repo_ids", repoID)
		}
	}
	return true
}

// repoIDPredicate reports whether any predicate tests alias.id against either
// grant list. The alias must start at a word boundary, so `source_repo.id` does
// not count as `repo.id`.
func repoIDPredicate(predicates []string, alias string) bool {
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(alias) + `\.id IN \$allowed_(?:repository|scope)_ids`)
	for _, predicate := range predicates {
		if pattern.MatchString(predicate) {
			return true
		}
	}
	return false
}

// callChainRepoAliasAdmits evaluates the predicates on a Repository alias, whose
// grant key is its own id rather than a repo_id property.
func callChainRepoAliasAdmits(predicates []string, alias, repoID string, params map[string]any) bool {
	for _, predicate := range predicates {
		switch {
		case strings.Contains(predicate, alias+".id IN $allowed_repository_ids"):
			if !querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) &&
				!querycontract.GraphParamContains(params, "allowed_scope_ids", repoID) {
				return false
			}
		case strings.Contains(predicate, alias+".id = $repo_id"):
			bound, _ := params["repo_id"].(string)
			if repoID != bound || repoID == "" {
				return false
			}
		}
	}
	return true
}
