package query

import (
	"context"
	"fmt"
	"strings"
)

func documentationFindingFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationFindingFilter, bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.scoped() {
		return filter, true
	}
	if access.empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	return filter, true
}

func documentationFactFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationFactFilter,
) (documentationFactFilter, bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.scoped() {
		return filter, true
	}
	if access.empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
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
	predicates := []string{
		fmt.Sprintf("%s.scope_id IN (%s)", factAlias, inList),
		fmt.Sprintf("%s.payload->>'repo' IN (%s)", scopeAlias, inList),
		fmt.Sprintf("%s.payload->>'repo_id' IN (%s)", scopeAlias, inList),
		fmt.Sprintf("%s.payload->>'repository_id' IN (%s)", factAlias, inList),
		fmt.Sprintf("%s.payload->>'repo_id' IN (%s)", factAlias, inList),
		fmt.Sprintf("%s.payload->'source'->>'repository_id' IN (%s)", factAlias, inList),
	}
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
