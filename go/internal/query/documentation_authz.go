// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func documentationFindingFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationFindingFilter, bool) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if !access.Scoped() {
		return filter, true
	}
	if access.Empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter, true
}

func documentationFactFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationFactFilter,
) (documentationFactFilter, bool) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if !access.Scoped() {
		return filter, true
	}
	if access.Empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter, true
}

func appendDocumentationAuthorizationClause(
	clauses []string,
	args []any,
	factAlias string,
	scopeAlias string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]string, []any) {
	ids := uniqueDocumentationAccessIDs(allowedRepositoryIDs, allowedScopeIDs)
	if len(ids) == 0 {
		return clauses, args
	}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	inList := strings.Join(placeholders, ", ")
	predicates := documentationScopeGrantPredicates(factAlias+".scope_id", scopeAlias, inList)
	predicates = append(predicates,
		fmt.Sprintf("%s.payload->>'repository_id' IN (%s)", factAlias, inList),
		fmt.Sprintf("%s.payload->>'repo_id' IN (%s)", factAlias, inList),
		fmt.Sprintf("%s.payload->'source'->>'repository_id' IN (%s)", factAlias, inList),
	)
	for _, placeholder := range placeholders {
		predicates = append(
			predicates,
			documentationAuthJSONRefPredicate(factAlias, "candidate_refs", "kind", "id", placeholder),
			documentationAuthJSONRefPredicate(factAlias, "evidence_refs", "kind", "id", placeholder),
			documentationAuthJSONRefPredicate(factAlias, "linked_entities", "entity_type", "entity_id", placeholder),
		)
	}
	return append(clauses, "("+strings.Join(predicates, " OR ")+")"), args
}

// appendDocumentationScopeGrantClause appends the scope-level half of the
// documentation authorization predicate for statements that read a scope or
// generation row rather than fact rows: the generation labels of a facts page
// (#7128). A scope is granted when its id, or its payload repo or repo_id, is
// in the caller's grants; these are the same three predicates the facts read
// applies through appendDocumentationAuthorizationClause. The fact-payload
// predicates of that clause describe individual facts, not a scope, so they
// have no scope-level meaning here. With no grants the clauses are returned
// unchanged, which is the unscoped (shared key, admin) path.
func appendDocumentationScopeGrantClause(
	clauses []string,
	args []any,
	scopeIDColumn string,
	scopeAlias string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]string, []any) {
	ids := uniqueDocumentationAccessIDs(allowedRepositoryIDs, allowedScopeIDs)
	if len(ids) == 0 {
		return clauses, args
	}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	predicates := documentationScopeGrantPredicates(scopeIDColumn, scopeAlias, strings.Join(placeholders, ", "))
	return append(clauses, "("+strings.Join(predicates, " OR ")+")"), args
}

// documentationScopeGrantPredicates is the scope-level grant: the scope id, or
// the scope payload's repo or repo_id, is one of the granted ids in inList.
func documentationScopeGrantPredicates(scopeIDColumn string, scopeAlias string, inList string) []string {
	return []string{
		fmt.Sprintf("%s IN (%s)", scopeIDColumn, inList),
		fmt.Sprintf("%s.payload->>'repo' IN (%s)", scopeAlias, inList),
		fmt.Sprintf("%s.payload->>'repo_id' IN (%s)", scopeAlias, inList),
	}
}

func documentationAuthorizationApplies(allowedRepositoryIDs []string, allowedScopeIDs []string) bool {
	return len(uniqueDocumentationAccessIDs(allowedRepositoryIDs, allowedScopeIDs)) > 0
}

func documentationAuthJSONRefPredicate(
	factAlias string,
	field string,
	kindKey string,
	idKey string,
	placeholder string,
) string {
	return fmt.Sprintf(
		"%s.payload->'%s' @> jsonb_build_array(jsonb_build_object('%s', 'repository', '%s', %s::text))",
		factAlias,
		field,
		kindKey,
		idKey,
		placeholder,
	)
}

func uniqueDocumentationAccessIDs(repoIDs []string, scopeIDs []string) []string {
	values := make([]string, 0, len(repoIDs)+len(scopeIDs))
	seen := make(map[string]struct{}, len(repoIDs)+len(scopeIDs))
	for _, raw := range append(append([]string{}, repoIDs...), scopeIDs...) {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
