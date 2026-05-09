package dbtsql

import (
	"regexp"
	"strings"
)

var (
	dbtIdentifierRe         = regexp.MustCompile(`\b(?P<identifier>[A-Za-z_][A-Za-z0-9_]*)\b`)
	dbtSingleQuotedStringRe = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	dbtIgnoredIdentifiers   = map[string]struct{}{
		"and": {}, "as": {}, "asc": {}, "case": {}, "cast": {}, "coalesce": {}, "count": {}, "desc": {}, "distinct": {}, "else": {},
		"end": {}, "false": {}, "from": {}, "group": {}, "in": {}, "is": {}, "join": {}, "left": {}, "like": {}, "limit": {},
		"lower": {}, "not": {}, "null": {}, "on": {}, "or": {}, "order": {}, "over": {}, "partition": {}, "by": {}, "rows": {},
		"range": {}, "preceding": {}, "following": {}, "current": {}, "row": {}, "right": {}, "select": {}, "sum": {}, "then": {},
		"true": {}, "upper": {}, "when": {}, "where": {}, "with": {},
	}
)

func unqualifiedIdentifiers(expression string, matchedIdentifiers map[string]struct{}) []string {
	sanitized := dbtSingleQuotedStringRe.ReplaceAllStringFunc(expression, func(value string) string {
		return regexp.MustCompile(`.`).ReplaceAllString(value, " ")
	})
	identifiers := make([]string, 0)
	seen := make(map[string]struct{})
	for _, match := range dbtIdentifierRe.FindAllStringSubmatchIndex(sanitized, -1) {
		identifier := sanitized[match[2]:match[3]]
		lowered := strings.ToLower(identifier)
		if _, ok := matchedIdentifiers[identifier]; ok {
			continue
		}
		if _, ok := dbtIgnoredIdentifiers[lowered]; ok {
			continue
		}
		if _, ok := seen[identifier]; ok {
			continue
		}
		if next := nextNonSpaceCharacter(sanitized, match[1]); next == "(" || next == "." {
			continue
		}
		seen[identifier] = struct{}{}
		identifiers = append(identifiers, identifier)
	}
	return identifiers
}

func nextNonSpaceCharacter(text string, index int) string {
	for index < len(text) {
		if text[index] != ' ' && text[index] != '\t' && text[index] != '\n' && text[index] != '\r' {
			return string(text[index])
		}
		index++
	}
	return ""
}
