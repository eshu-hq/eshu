// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"regexp"
	"strconv"
	"strings"
)

// This file reads the write shape of one Cypher statement for the
// oversized-index-key guard: which variables are bound to which labels, and
// which expressions each write assigns to which property. It recognizes the
// shapes Eshu writers emit (UNWIND $rows AS row, MERGE/CREATE inline maps,
// SET n.p = expr, SET n += row.map); anything else is simply not tracked.

// indexWriteExpr is one assigned expression, reduced to the value references
// the guard can measure.
type indexWriteExpr struct {
	rowVar string   // UNWIND variable the row fields belong to, empty for none
	fields []string // row fields referenced, in order
	params []string // scalar $params referenced
	sum    bool     // true when a top-level + concatenates the references
}

// indexWriteVar is one graph variable with the index-relevant writes the
// statement performs on it.
type indexWriteVar struct {
	labels  []string
	assigns map[string]indexWriteExpr // property -> expression
	merges  []indexWriteExpr          // SET v += <map expression>
	keys    []labeledIndexKey         // schema index keys the var writes
}

type labeledIndexKey struct {
	label string
	props []string
}

// indexWritePlan is the cached analysis of one Cypher statement.
type indexWritePlan struct {
	rowParams map[string]string // UNWIND variable -> $param name
	vars      []*indexWriteVar  // only vars that write an indexed property
}

type cypherKeyword struct {
	pos, end int
	word     string
}

var (
	cypherClausePattern = regexp.MustCompile(
		`\b(OPTIONAL MATCH|DETACH DELETE|ON CREATE|ON MATCH|ORDER BY|MATCH|MERGE|CREATE|SET|WHERE|WITH|UNWIND|RETURN|DELETE|REMOVE|FOREACH|CALL|UNION|LIMIT|SKIP|YIELD)\b`,
	)
	cypherNodePattern   = regexp.MustCompile(`\(\s*(\w*)\s*((?::\s*\w+(?:\s*[|&]\s*\w+)*\s*)+)(\{)?`)
	cypherUnwindParam   = regexp.MustCompile(`\bUNWIND\s+\$(\w+)\s+AS\s+(\w+)`)
	cypherPropRef       = regexp.MustCompile(`\b(\w+)\.(\w+)\b`)
	cypherParamRef      = regexp.MustCompile(`\$(\w+)`)
	cypherPropAssign    = regexp.MustCompile(`(?s)^(\w+)\.(\w+)\s*=\s*(.+)$`)
	cypherMapAssign     = regexp.MustCompile(`(?s)^(\w+)\s*\+?=\s*(.+)$`)
	cypherMapEntryKey   = regexp.MustCompile(`(?s)^\s*(\w+)\s*:\s*(.+)$`)
	cypherLabelSplitter = regexp.MustCompile(`[:|&\s]+`)
)

// analyzeIndexWrites builds the write plan for cypher. It returns a plan with
// no vars when the statement writes no indexed property.
func analyzeIndexWrites(cypher string) *indexWritePlan {
	plan := &indexWritePlan{rowParams: map[string]string{}}
	if !strings.Contains(cypher, "MERGE") && !strings.Contains(cypher, "CREATE") && !strings.Contains(cypher, "SET") {
		return plan
	}
	for _, m := range cypherUnwindParam.FindAllStringSubmatch(cypher, -1) {
		plan.rowParams[m[2]] = m[1]
	}
	keywords := cypherKeywords(cypher)
	vars := map[string]*indexWriteVar{}
	var order []string // first-appearance order keeps drop records deterministic
	anon := 0
	byLabel := SchemaIndexKeysByLabel()

	for _, loc := range cypherNodePattern.FindAllStringSubmatchIndex(cypher, -1) {
		name := cypher[loc[2]:loc[3]]
		if name == "" {
			anon++
			name = "\x00anon" + strconv.Itoa(anon)
		}
		v := vars[name]
		if v == nil {
			v = &indexWriteVar{assigns: map[string]indexWriteExpr{}}
			vars[name] = v
			order = append(order, name)
		}
		for _, label := range cypherLabelSplitter.Split(cypher[loc[4]:loc[5]], -1) {
			if label != "" && !containsString(v.labels, label) {
				v.labels = append(v.labels, label)
			}
		}
		if loc[6] < 0 {
			continue
		}
		clause := precedingClause(keywords, loc[0])
		if clause != "MERGE" && clause != "CREATE" {
			continue
		}
		closeAt := matchingBrace(cypher, loc[6])
		if closeAt < 0 {
			continue
		}
		for _, entry := range splitTopLevel(cypher[loc[6]+1 : closeAt]) {
			if em := cypherMapEntryKey.FindStringSubmatch(entry); em != nil {
				v.assigns[em[1]] = parseIndexWriteExpr(em[2], plan.rowParams)
			}
		}
	}

	for i, kw := range keywords {
		if kw.word != "SET" {
			continue
		}
		end := len(cypher)
		if i+1 < len(keywords) {
			end = keywords[i+1].pos
		}
		for _, item := range splitTopLevel(cypher[kw.end:end]) {
			item = strings.TrimSpace(item)
			if pm := cypherPropAssign.FindStringSubmatch(item); pm != nil {
				if v := vars[pm[1]]; v != nil {
					v.assigns[pm[2]] = parseIndexWriteExpr(pm[3], plan.rowParams)
				}
				continue
			}
			if mm := cypherMapAssign.FindStringSubmatch(item); mm != nil {
				v := vars[mm[1]]
				if v == nil {
					continue
				}
				expr := strings.TrimSpace(mm[2])
				if strings.HasPrefix(expr, "{") && strings.HasSuffix(expr, "}") {
					for _, entry := range splitTopLevel(expr[1 : len(expr)-1]) {
						if em := cypherMapEntryKey.FindStringSubmatch(entry); em != nil {
							v.assigns[em[1]] = parseIndexWriteExpr(em[2], plan.rowParams)
						}
					}
					continue
				}
				v.merges = append(v.merges, parseIndexWriteExpr(expr, plan.rowParams))
			}
		}
	}

	for _, name := range order {
		v := vars[name]
		for _, label := range v.labels {
			for _, key := range byLabel[label] {
				if len(v.merges) > 0 || writesAny(v.assigns, key) {
					v.keys = append(v.keys, labeledIndexKey{label: label, props: key})
				}
			}
		}
		if len(v.keys) > 0 {
			plan.vars = append(plan.vars, v)
		}
	}
	return plan
}

func cypherKeywords(cypher string) []cypherKeyword {
	locs := cypherClausePattern.FindAllStringSubmatchIndex(cypher, -1)
	out := make([]cypherKeyword, 0, len(locs))
	for _, loc := range locs {
		// A property or parameter named like a keyword (n.set, $match) is
		// not a clause.
		if loc[0] > 0 && (cypher[loc[0]-1] == '.' || cypher[loc[0]-1] == '$') {
			continue
		}
		out = append(out, cypherKeyword{pos: loc[0], end: loc[1], word: cypher[loc[2]:loc[3]]})
	}
	return out
}

func precedingClause(keywords []cypherKeyword, pos int) string {
	clause := ""
	for _, kw := range keywords {
		if kw.pos >= pos {
			break
		}
		clause = kw.word
	}
	return clause
}

func parseIndexWriteExpr(expr string, rowParams map[string]string) indexWriteExpr {
	var out indexWriteExpr
	for _, m := range cypherPropRef.FindAllStringSubmatch(expr, -1) {
		if _, ok := rowParams[m[1]]; !ok {
			continue
		}
		if out.rowVar == "" {
			out.rowVar = m[1]
		}
		if m[1] == out.rowVar {
			out.fields = append(out.fields, m[2])
		}
	}
	for _, m := range cypherParamRef.FindAllStringSubmatch(expr, -1) {
		out.params = append(out.params, m[1])
	}
	for _, part := range splitTopLevelOn(expr, '+') {
		if part != expr {
			out.sum = true
			break
		}
	}
	return out
}

func writesAny(assigns map[string]indexWriteExpr, key []string) bool {
	for _, prop := range key {
		if _, ok := assigns[prop]; ok {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// matchingBrace returns the index of the brace closing the one at open, or -1.
func matchingBrace(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTopLevel(s string) []string { return splitTopLevelOn(s, ',') }

// splitTopLevelOn splits s on sep outside parentheses, brackets, braces, and
// quoted strings.
func splitTopLevelOn(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
