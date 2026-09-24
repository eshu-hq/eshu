// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// This file reads the write shape of one Cypher statement for the
// oversized-index-key guard: which variables are bound to which labels, and
// which expressions each write assigns to which property. It recognizes the
// shapes Eshu writers emit (UNWIND $rows AS row, WITH row AS alias, MERGE and
// CREATE inline maps, SET n.p = expr, SET n += row.map, SET n:Label, any
// keyword case, backtick labels). A write it cannot read is not tracked and
// not dropped; index_key_guard_report.go surfaces it so the gap is loud.

// indexWriteExpr is one assigned expression, reduced to the value references
// the guard can measure.
type indexWriteExpr struct {
	rowVar string   // UNWIND variable the row fields belong to, empty for none
	fields []string // row fields referenced, in order
	params []string // scalar $params referenced
	sum    bool     // true when a top-level + concatenates the references
	// constBytes is the byte length of string literals joined by a top-level
	// + ('prefix:' + row.name), which count toward the key.
	constBytes int
	// wholeRow is true for SET n += row, where the row itself is the map.
	wholeRow bool
	// unknown lists identifiers the expression reads that are neither an
	// UNWIND row variable nor a resolvable alias (a nested UNWIND element, a
	// WITH-bound map). The value they carry cannot be measured.
	unknown []string
}

// indexWriteVar is one graph variable with the index-relevant writes the
// statement performs on it.
type indexWriteVar struct {
	labels  []string
	assigns map[string]indexWriteExpr // property -> expression
	merges  []indexWriteExpr          // SET v += <map expression>
	keys    []labeledIndexKey         // schema index keys the var writes
	// written is true when the var is created (MERGE/CREATE) or modified
	// (SET) by the statement, as opposed to only matched.
	written bool
	// opaque is true when a write to the var was not readable: a property
	// map entry or SET item outside the recognized forms.
	opaque bool
}

type labeledIndexKey struct {
	label string
	props []string
}

// indexWritePlan is the cached analysis of one Cypher statement.
type indexWritePlan struct {
	rowParams map[string]string // UNWIND variable -> $param name
	vars      []*indexWriteVar  // only vars that write an indexed property
	// unanalyzed lists the schema labels the statement writes in a way the
	// analyzer could not read, for UnanalyzedIndexWrites.
	unanalyzed []UnanalyzedIndexWrite
	// warned flips once, when the first caller is told about unanalyzed.
	warned atomic.Bool
}

// fieldAlias is a WITH row.field AS name binding.
type fieldAlias struct{ rowVar, field string }

// exprScope is what parseIndexWriteExpr can resolve identifiers against.
type exprScope struct {
	rowNames map[string]string     // row variable or alias -> canonical row variable
	fields   map[string]fieldAlias // WITH row.field AS name
}

type cypherKeyword struct {
	pos, end int
	word     string
}

const cypherLabelExpr = "(?:\\w+|`[^`]+`)"

var (
	cypherClausePattern = regexp.MustCompile(
		`(?i)\b(OPTIONAL\s+MATCH|DETACH\s+DELETE|ON\s+CREATE|ON\s+MATCH|ORDER\s+BY|MATCH|MERGE|CREATE|SET|WHERE|WITH|UNWIND|RETURN|DELETE|REMOVE|FOREACH|CALL|UNION|LIMIT|SKIP|YIELD)\b`,
	)
	cypherWritesPattern = regexp.MustCompile(`(?i)\b(?:MERGE|CREATE|SET)\b`)
	// cypherNodePattern matches a node pattern opening: an optional
	// variable, optional labels, and an optional property map brace.
	cypherNodePattern = regexp.MustCompile(
		`\(\s*(\w*)\s*((?::\s*` + cypherLabelExpr + `(?:\s*[|&]\s*` + cypherLabelExpr + `)*\s*)+)?(\{)?`,
	)
	cypherLabelName    = regexp.MustCompile(cypherLabelExpr)
	cypherSetLabel     = regexp.MustCompile(`(?s)^(\w+)\s*((?::\s*` + cypherLabelExpr + `\s*)+)$`)
	cypherWriteLabel   = regexp.MustCompile(`:\s*(` + cypherLabelExpr + `)`)
	cypherUnwindParam  = regexp.MustCompile(`(?i)\bUNWIND\s+\$(\w+)\s+AS\s+(\w+)`)
	cypherWithAlias    = regexp.MustCompile(`(?is)^(\w+)(?:\.(\w+))?\s+AS\s+(\w+)$`)
	cypherPropAssign   = regexp.MustCompile(`(?s)^(\w+)\.(\w+)\s*=\s*(.+)$`)
	cypherMapAssign    = regexp.MustCompile(`(?s)^(\w+)\s*\+?=\s*(.+)$`)
	cypherMapEntryKey  = regexp.MustCompile(`(?s)^\s*(\w+)\s*:\s*(.+)$`)
	cypherLeadingIdent = regexp.MustCompile(`^(\w+)`)
	cypherClauseSpaces = regexp.MustCompile(`\s+`)
)

// analyzeIndexWrites builds the write plan for cypher. It returns a plan with
// no vars when the statement writes no indexed property.
func analyzeIndexWrites(cypher string) *indexWritePlan {
	plan := &indexWritePlan{rowParams: map[string]string{}}
	if !cypherWritesPattern.MatchString(cypher) {
		return plan
	}
	scope := exprScope{rowNames: map[string]string{}, fields: map[string]fieldAlias{}}
	for _, m := range cypherUnwindParam.FindAllStringSubmatch(cypher, -1) {
		plan.rowParams[m[2]] = m[1]
		scope.rowNames[m[2]] = m[2]
	}
	keywords := cypherKeywords(cypher)
	bindWithAliases(cypher, keywords, &scope)

	vars := map[string]*indexWriteVar{}
	var order []string // first-appearance order keeps drop records deterministic
	anon := 0
	byLabel := SchemaIndexKeysByLabel()
	varFor := func(name string) *indexWriteVar {
		v := vars[name]
		if v == nil {
			v = &indexWriteVar{assigns: map[string]indexWriteExpr{}}
			vars[name] = v
			order = append(order, name)
		}
		return v
	}

	for _, loc := range cypherNodePattern.FindAllStringSubmatchIndex(cypher, -1) {
		if loc[4] < 0 && loc[6] < 0 {
			continue // a bare (x) or a call parenthesis says nothing about labels
		}
		name := cypher[loc[2]:loc[3]]
		if name == "" {
			anon++
			name = "\x00anon" + strconv.Itoa(anon)
		}
		v := varFor(name)
		if loc[4] >= 0 {
			addLabels(v, cypher[loc[4]:loc[5]])
		}
		clause := precedingClause(keywords, loc[0])
		if clause != "MERGE" && clause != "CREATE" {
			continue
		}
		v.written = true
		if loc[6] < 0 {
			continue
		}
		closeAt := matchingBrace(cypher, loc[6])
		if closeAt < 0 {
			v.opaque = true
			continue
		}
		for _, entry := range splitTopLevel(cypher[loc[6]+1 : closeAt]) {
			if strings.TrimSpace(entry) == "" {
				continue
			}
			em := cypherMapEntryKey.FindStringSubmatch(entry)
			if em == nil {
				v.opaque = true
				continue
			}
			v.assigns[em[1]] = parseIndexWriteExpr(em[2], scope)
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
			applySetItem(strings.TrimSpace(item), vars, varFor, scope)
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
	plan.unanalyzed = unanalyzedWrites(cypher, keywords, vars, order)
	return plan
}

// applySetItem reads one SET item and records it on the variable it writes.
// An item outside the recognized forms marks the variable it starts with (or
// every labeled variable when it names none) opaque.
func applySetItem(item string, vars map[string]*indexWriteVar, varFor func(string) *indexWriteVar, scope exprScope) {
	if item == "" {
		return
	}
	if pm := cypherPropAssign.FindStringSubmatch(item); pm != nil {
		if v := vars[pm[1]]; v != nil {
			v.written = true
			v.assigns[pm[2]] = parseIndexWriteExpr(pm[3], scope)
		}
		return
	}
	if mm := cypherMapAssign.FindStringSubmatch(item); mm != nil {
		v := vars[mm[1]]
		if v == nil {
			return
		}
		v.written = true
		expr := strings.TrimSpace(mm[2])
		if strings.HasPrefix(expr, "{") && strings.HasSuffix(expr, "}") {
			for _, entry := range splitTopLevel(expr[1 : len(expr)-1]) {
				if strings.TrimSpace(entry) == "" {
					continue
				}
				if em := cypherMapEntryKey.FindStringSubmatch(entry); em != nil {
					v.assigns[em[1]] = parseIndexWriteExpr(em[2], scope)
				} else {
					v.opaque = true
				}
			}
			return
		}
		v.merges = append(v.merges, parseIndexWriteExpr(expr, scope))
		return
	}
	if lm := cypherSetLabel.FindStringSubmatch(item); lm != nil {
		v := varFor(lm[1])
		v.written = true
		addLabels(v, lm[2])
		return
	}
	if id := cypherLeadingIdent.FindString(item); id != "" && vars[id] != nil {
		vars[id].opaque = true
		return
	}
	for _, v := range vars {
		if len(v.labels) > 0 {
			v.opaque = true
		}
	}
}

// bindWithAliases records WITH row AS alias and WITH row.field AS alias
// bindings so a renamed row variable or lifted map stays measurable.
func bindWithAliases(cypher string, keywords []cypherKeyword, scope *exprScope) {
	for i, kw := range keywords {
		if kw.word != "WITH" {
			continue
		}
		end := len(cypher)
		if i+1 < len(keywords) {
			end = keywords[i+1].pos
		}
		for _, item := range splitTopLevel(cypher[kw.end:end]) {
			m := cypherWithAlias.FindStringSubmatch(strings.TrimSpace(item))
			if m == nil {
				continue
			}
			canonical, ok := scope.rowNames[m[1]]
			if !ok {
				continue
			}
			if m[2] == "" {
				scope.rowNames[m[3]] = canonical
			} else {
				scope.fields[m[3]] = fieldAlias{rowVar: canonical, field: m[2]}
			}
		}
	}
}

// addLabels adds the labels of one node-pattern label group (":A:B",
// ":`A`|B") to v, unquoting backtick labels.
func addLabels(v *indexWriteVar, group string) {
	for _, label := range cypherLabelName.FindAllString(group, -1) {
		label = strings.Trim(label, "`")
		if label != "" && !containsString(v.labels, label) {
			v.labels = append(v.labels, label)
		}
	}
}

// cypherKeywords returns the clause keywords of cypher in order, in any case.
// A keyword inside a quoted string, after a property dot or parameter sigil,
// or used as a map key (limit: 1) is not a clause.
func cypherKeywords(cypher string) []cypherKeyword {
	locs := cypherClausePattern.FindAllStringSubmatchIndex(cypher, -1)
	if len(locs) == 0 {
		return nil
	}
	quoted := quotedMask(cypher)
	out := make([]cypherKeyword, 0, len(locs))
	for _, loc := range locs {
		if quoted[loc[0]] || (loc[0] > 0 && (cypher[loc[0]-1] == '.' || cypher[loc[0]-1] == '$')) {
			continue
		}
		if rest := strings.TrimLeft(cypher[loc[1]:], " \t\r\n"); strings.HasPrefix(rest, ":") {
			continue
		}
		word := strings.ToUpper(cypherClauseSpaces.ReplaceAllString(cypher[loc[2]:loc[3]], " "))
		out = append(out, cypherKeyword{pos: loc[0], end: loc[1], word: word})
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
