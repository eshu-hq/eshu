package rust

import "strings"

func rustApplyWhereMetadata(item map[string]any, signature string) {
	predicates := rustWherePredicates(signature)
	if len(predicates) == 0 {
		return
	}
	item["where_predicates"] = predicates

	associatedTypes := make([]string, 0)
	higherRanked := make([]string, 0)
	for _, predicate := range predicates {
		if strings.HasPrefix(predicate, "for<") {
			higherRanked = appendUniqueString(higherRanked, predicate)
		}
		if left, ok := rustWherePredicateSubject(predicate); ok && strings.Contains(left, "::") {
			associatedTypes = appendUniqueString(associatedTypes, predicate)
		}
	}
	if len(associatedTypes) > 0 {
		item["associated_type_constraints"] = associatedTypes
	}
	if len(higherRanked) > 0 {
		item["higher_ranked_trait_bounds"] = higherRanked
	}
}

func rustWherePredicates(signature string) []string {
	whereClause := rustWhereClause(signature)
	if whereClause == "" {
		return nil
	}
	predicates := make([]string, 0)
	for _, part := range rustSplitTopLevel(whereClause, ',') {
		predicate := strings.TrimSpace(part)
		predicate = strings.TrimSuffix(predicate, ";")
		predicate = strings.TrimSpace(predicate)
		if predicate != "" {
			predicates = appendUniqueString(predicates, predicate)
		}
	}
	return predicates
}

func rustWhereClause(signature string) string {
	parts := rustWhereClausePattern.Split(signature, 2)
	if len(parts) < 2 {
		return ""
	}
	clause := strings.TrimSpace(parts[1])
	clause = strings.TrimSuffix(clause, ";")
	return strings.TrimSpace(clause)
}

func rustWherePredicateSubject(predicate string) (string, bool) {
	for idx, r := range predicate {
		if r != ':' {
			continue
		}
		if idx > 0 && predicate[idx-1] == ':' {
			continue
		}
		if idx+1 < len(predicate) && predicate[idx+1] == ':' {
			continue
		}
		return strings.TrimSpace(predicate[:idx]), true
	}
	return "", false
}
