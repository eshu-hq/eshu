// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"strings"
	"testing"
)

// setListEndWords end a SET list when they appear at its own nesting level.
var setListEndWords = map[string]bool{"WHERE": true, "WHEN": true, "RETURNING": true, "FROM": true}

// setListItems returns the top-level assignments of the SET list that starts
// at statement[start:]. The list ends at WHERE, WHEN, RETURNING or FROM at its
// own level, at a closing parenthesis that closes the group the SET sits in
// (a CTE body), or at the end of the text. Single-quoted strings (a doubled
// quote is an escape), parenthesized groups and CASE ... END are skipped, so a keyword or
// comma inside them neither ends the list nor splits an assignment.
// Dollar-quoted text is read as SQL on purpose: a plpgsql body is where a
// trigger's statements live.
func setListItems(statement string, start int) []string {
	var items []string
	depth, caseDepth, itemStart := 0, 0, start
	quoted := false
	i := start
	for i < len(statement) {
		c := statement[i]
		switch {
		case quoted:
			if c == '\'' {
				if i+1 < len(statement) && statement[i+1] == '\'' {
					i++
				} else {
					quoted = false
				}
			}
		case c == '\'':
			quoted = true
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 && caseDepth == 0 {
				return append(items, statement[itemStart:i])
			}
			depth--
		case c == ',' && depth == 0 && caseDepth == 0:
			items = append(items, statement[itemStart:i])
			itemStart = i + 1
		case isIdentStart(c) && (i == 0 || !isIdentChar(statement[i-1])):
			end := i
			for end < len(statement) && isIdentChar(statement[end]) {
				end++
			}
			switch word := strings.ToUpper(statement[i:end]); {
			case word == "CASE":
				caseDepth++
			case word == "END" && caseDepth > 0:
				caseDepth--
			case depth == 0 && caseDepth == 0 && setListEndWords[word]:
				return append(items, statement[itemStart:i])
			}
			i = end
			continue
		}
		i++
	}
	return append(items, statement[itemStart:])
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

func TestSetListItemsSplitsTopLevelAssignments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want []string
	}{
		{"SET a = 1, version = 0 WHERE x", []string{"a = 1", "version = 0"}},
		{"SET a = CASE WHEN true THEN 1 END, version = 0 WHERE x", []string{"a = CASE WHEN true THEN 1 END", "version = 0"}},
		{"SET a = (SELECT b FROM c WHERE d), version = 0", []string{"a = (SELECT b FROM c WHERE d)", "version = 0"}},
		{"SET a = 'x where y, z', version = 0", []string{"a = 'x where y, z'", "version = 0"}},
		{"SET a = 'it''s, where', version = 0", []string{"a = 'it''s, where'", "version = 0"}},
		{"SET (version, b) = (v.version + 1, now()) RETURNING x", []string{"(version, b) = (v.version + 1, now())"}},
		{"SET version = 0 WHERE r = 1) SELECT 1", []string{"version = 0"}},
		{"SET version = v.version + 1 FROM src", []string{"version = v.version + 1"}},
	}
	for _, tt := range tests {
		got := setListItems(tt.text, len("SET"))
		for i := range got {
			got[i] = strings.TrimSpace(got[i])
		}
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("setListItems(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}
