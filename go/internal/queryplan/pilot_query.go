// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

// ValidatePilotCaseQuery binds a production-emitted statement to one
// independently required variant/case and its declared authorization mode.
// The all-scopes form must be explicitly registered; text inspection cannot
// upgrade a scoped case to unrestricted access.
func ValidatePilotCaseQuery(kind, query string, contract PilotContract, candidate PilotCase, schemaStatements []string) error {
	if !isSHA256(candidate.QuerySHA256) || fmt.Sprintf("%x", sha256.Sum256([]byte(query))) != candidate.QuerySHA256 {
		return errors.New("emitted query text differs from required variant hash")
	}
	switch candidate.ScopeMode {
	case "scoped":
		if len(contract.AuthorizationAliases) == 0 && len(contract.ScopePredicates) == 0 {
			return errors.New("scoped case has no authorization contract")
		}
		return ValidatePilotQuery(kind, query, contract, schemaStatements)
	case "all_scopes":
		unrestricted := contract
		unrestricted.AuthorizationAliases = nil
		unrestricted.ScopePredicates = nil
		if err := ValidatePilotQuery(kind, query, unrestricted, schemaStatements); err != nil {
			return err
		}
		tokens, err := pilotTokens(query)
		if err != nil {
			return err
		}
		for _, token := range tokens {
			if strings.HasPrefix(token, "$ALLOWED_") || strings.HasPrefix(token, "$GRANT_") {
				return errors.New("all-scopes case unexpectedly references grants")
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported pilot scope mode %q", candidate.ScopeMode)
	}
}

// ValidatePilotQuery applies a dialect-aware structural floor to an emitted
// pilot statement. It recognizes comments, literals, nested clauses, boolean
// precedence, alias bindings, bounds, and final page order. Full database
// semantics and planner behavior remain the live evidence gate's job.
func ValidatePilotQuery(kind, query string, contract PilotContract, schemaStatements []string) error {
	tokens, err := pilotTokens(query)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errors.New("empty pilot query")
	}
	if err := pilotBalanced(tokens); err != nil {
		return err
	}
	switch kind {
	case queryKindCypher:
		if !containsPilotToken(tokens, "MATCH") || !containsPilotToken(tokens, "RETURN") {
			return errors.New("unsupported pilot Cypher: MATCH and RETURN required")
		}
		for _, unsupported := range []string{"UNION", "CALL", "WITH", "CREATE", "DELETE", "SET", "REMOVE"} {
			if containsPilotToken(tokens, unsupported) {
				return fmt.Errorf("unsupported pilot Cypher clause %s", unsupported)
			}
		}
		if err := pilotTraversalBounds(tokens); err != nil {
			return err
		}
	case queryKindSQLReadModel:
		if !containsPilotToken(tokens, "SELECT") || !containsPilotToken(tokens, "FROM") || (tokens[0] != "SELECT" && tokens[0] != "WITH") {
			return errors.New("unsupported pilot SQL: SELECT or WITH SELECT required")
		}
		for _, unsupported := range []string{"UNION", "INSERT", "UPDATE", "DELETE", "MERGE", "DROP", "CREATE", "ALTER"} {
			if containsPilotToken(tokens, unsupported) {
				return fmt.Errorf("unsupported pilot SQL clause %s", unsupported)
			}
		}
	default:
		return fmt.Errorf("unsupported pilot query kind %q", kind)
	}
	for _, name := range contract.RequiredSchema {
		if !pilotSchemaPresent(name, schemaStatements) {
			return fmt.Errorf("missing pilot schema/index %s", name)
		}
	}
	order, limit := pilotFinalOrderLimit(tokens, kind)
	if contract.RequireOrder && order < 0 {
		return errors.New("missing final ORDER BY")
	}
	if contract.RequireLimit && limit < 0 {
		return errors.New("missing final LIMIT")
	}
	if contract.RequireOrder && contract.RequireLimit && order > limit {
		return errors.New("ORDER BY must precede LIMIT")
	}
	for _, alias := range contract.AuthorizationAliases {
		if !pilotValidName(alias) {
			return fmt.Errorf("invalid authorization alias %q", alias)
		}
		if !pilotAliasBound(tokens, alias, kind) {
			return fmt.Errorf("authorization alias %s is not bound", alias)
		}
		if !pilotScopeGuaranteed(tokens, alias, nil, kind, order, limit, contract.CorrelationPredicates) {
			return fmt.Errorf("authorization predicate for %s is absent or can be bypassed", alias)
		}
	}
	for _, scope := range contract.ScopePredicates {
		if !pilotValidName(scope.Alias) || !pilotAliasBound(tokens, scope.Alias, kind) {
			return fmt.Errorf("scope predicate alias %q is not bound", scope.Alias)
		}
		wanted, err := pilotTokens(scope.Expression)
		if err != nil || len(wanted) == 0 {
			return fmt.Errorf("invalid scope predicate for %s", scope.Alias)
		}
		if !pilotScopeGuaranteed(tokens, scope.Alias, wanted, kind, order, limit, contract.CorrelationPredicates) {
			return fmt.Errorf("scope predicate for %s is absent or can be bypassed", scope.Alias)
		}
	}
	return nil
}

func pilotScopeGuaranteed(tokens []string, alias string, wanted []string, kind string, order, limit int, correlations []string) bool {
	if kind == queryKindSQLReadModel {
		return pilotSQLScopeGuaranteed(tokens, alias, wanted, correlations)
	}
	return pilotScopedWhere(tokens, alias, wanted, kind, order, limit)
}

func pilotSQLScopeGuaranteed(tokens []string, alias string, wanted []string, correlations []string) bool {
	depth := 0
	for i, token := range tokens {
		if token == "WHERE" && depth == 0 {
			for _, bounds := range pilotWhereRanges(tokens, queryKindSQLReadModel) {
				if bounds[0] == i+1 {
					return pilotSQLGuarantees(tokens[bounds[0]:bounds[1]], alias, wanted, correlations)
				}
			}
		}
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	return false
}

func pilotSQLGuarantees(expression []string, alias string, wanted []string, correlations []string) bool {
	expression = pilotTrimParens(expression)
	if len(expression) == 0 || expression[0] == "NOT" {
		return false
	}
	if parts := pilotSplitBoolean(expression, "OR"); len(parts) > 1 {
		for _, part := range parts {
			if !pilotSQLGuarantees(part, alias, wanted, correlations) {
				return false
			}
		}
		return true
	}
	if parts := pilotSplitBoolean(expression, "AND"); len(parts) > 1 {
		for _, part := range parts {
			if pilotSQLGuarantees(part, alias, wanted, correlations) {
				return true
			}
		}
		return false
	}
	if len(expression) < 8 || expression[0] != "COALESCE" || expression[len(expression)-3] != "," ||
		expression[len(expression)-2] != "FALSE" || expression[len(expression)-1] != ")" ||
		!containsPilotToken(expression, "SELECT") ||
		!containsPilotToken(expression, "FALSE") || !pilotAliasBound(expression, alias, queryKindSQLReadModel) {
		return false
	}
	if !pilotScopedWhere(expression, alias, wanted, queryKindSQLReadModel, -1, -1) {
		return false
	}
	for _, correlation := range correlations {
		pattern, err := pilotTokens(correlation)
		if err != nil || len(pattern) == 0 || !pilotContainsTokens(expression, pattern) {
			return false
		}
	}
	return true
}

func pilotAliasBound(tokens []string, alias, kind string) bool {
	wanted := strings.ToUpper(alias)
	for i := 0; i < len(tokens); i++ {
		if kind == queryKindCypher && i+2 < len(tokens) && tokens[i] == "(" && tokens[i+1] == wanted && tokens[i+2] == ":" {
			return true
		}
		if kind == queryKindSQLReadModel && (tokens[i] == "FROM" || tokens[i] == "JOIN") {
			for j := i + 1; j < len(tokens) && j < i+6; j++ {
				if tokens[j] == wanted && j > i+1 && tokens[j-1] != "." {
					return true
				}
				if tokens[j] == "WHERE" || tokens[j] == "ON" || tokens[j] == "JOIN" {
					break
				}
			}
		}
	}
	return false
}

func pilotScopedWhere(tokens []string, alias string, wanted []string, kind string, order, limit int) bool {
	for _, bounds := range pilotWhereRanges(tokens, kind) {
		if order >= 0 && bounds[0] > order || limit >= 0 && bounds[0] > limit {
			continue
		}
		if pilotGuarantees(tokens[bounds[0]:bounds[1]], alias, wanted) {
			return true
		}
	}
	return false
}

func pilotWhereRanges(tokens []string, kind string) [][2]int {
	var ranges [][2]int
	depth := 0
	for i, token := range tokens {
		if token == "WHERE" {
			start := i + 1
			end := len(tokens)
			innerDepth := depth
			for j := start; j < len(tokens); j++ {
				switch tokens[j] {
				case "(", "[", "{":
					innerDepth++
				case ")", "]", "}":
					innerDepth--
				}
				if innerDepth < depth || innerDepth == depth && pilotClauseBoundary(tokens, j, kind) {
					end = j
					break
				}
			}
			if end > start {
				ranges = append(ranges, [2]int{start, end})
			}
		}
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	return ranges
}

func pilotClauseBoundary(tokens []string, index int, kind string) bool {
	token := tokens[index]
	if token == "ORDER" && index+1 < len(tokens) && tokens[index+1] == "BY" {
		return true
	}
	if token == "LIMIT" || token == "OFFSET" || token == "RETURN" || token == "WITH" || token == "UNION" || token == "SKIP" {
		return true
	}
	if kind == queryKindCypher && (token == "MATCH" || token == "OPTIONAL" || token == "CALL") {
		return true
	}
	if kind == queryKindSQLReadModel && (token == "GROUP" || token == "HAVING" || token == "WINDOW" || token == "SELECT") {
		return true
	}
	return false
}

// pilotGuarantees implements the monotone boolean rule for a required scope
// predicate: AND needs one guaranteed operand; OR needs every operand. A
// projection, comment, literal, or predicate after LIMIT is never inspected.
func pilotGuarantees(expression []string, alias string, wanted []string) bool {
	expression = pilotTrimParens(expression)
	if len(expression) == 0 || expression[0] == "NOT" {
		return false
	}
	if len(wanted) > 0 && pilotSameTokens(expression, pilotTrimParens(wanted)) {
		return true
	}
	if parts := pilotSplitBoolean(expression, "OR"); len(parts) > 1 {
		for _, part := range parts {
			if !pilotGuarantees(part, alias, wanted) {
				return false
			}
		}
		return true
	}
	if parts := pilotSplitBoolean(expression, "AND"); len(parts) > 1 {
		for _, part := range parts {
			if pilotGuarantees(part, alias, wanted) {
				return true
			}
		}
		return false
	}
	if len(wanted) > 0 {
		return pilotContainsTokens(expression, pilotTrimParens(wanted))
	}
	upperAlias := strings.ToUpper(alias)
	aliasProperty := false
	grantParameter := false
	for i := 0; i < len(expression); i++ {
		if i+2 < len(expression) && expression[i] == upperAlias && expression[i+1] == "." && pilotValidName(expression[i+2]) {
			aliasProperty = true
		}
		if strings.HasPrefix(expression[i], "$ALLOWED_") || strings.HasPrefix(expression[i], "$GRANT_") {
			grantParameter = true
		}
	}
	return aliasProperty && grantParameter
}

func pilotTrimParens(tokens []string) []string {
	for len(tokens) >= 2 && tokens[0] == "(" && tokens[len(tokens)-1] == ")" {
		depth := 0
		whole := true
		for i, token := range tokens {
			if token == "(" {
				depth++
			} else if token == ")" {
				depth--
				if depth == 0 && i != len(tokens)-1 {
					whole = false
					break
				}
			}
		}
		if !whole {
			break
		}
		tokens = tokens[1 : len(tokens)-1]
	}
	return tokens
}

func pilotSplitBoolean(tokens []string, operator string) [][]string {
	depth := 0
	start := 0
	var parts [][]string
	for i, token := range tokens {
		switch token {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 && token == operator {
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}
	if len(parts) > 0 {
		parts = append(parts, tokens[start:])
	}
	return parts
}

func pilotSameTokens(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if right[i] == "$PARAM" && len(left[i]) > 1 && left[i][0] == '$' {
			continue
		}
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func pilotContainsTokens(haystack, needle []string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if pilotSameTokens(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}
