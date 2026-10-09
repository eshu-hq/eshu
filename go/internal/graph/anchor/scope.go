// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"regexp"
	"strings"
)

// labelOp is one label added by SET n:L or removed by REMOVE n:L.
type labelOp struct {
	variable string
	labels   []string
	pos      int
}

var (
	// labelItem matches a SET or REMOVE item that adds or removes labels:
	// n:Label, n:A:B.
	labelItem = regexp.MustCompile("^([A-Za-z_]\\w*)\\s*((?::\\s*(?:\\w+|`[^`]+`)\\s*)+)$")
	// bareName matches a plain or backtick-quoted name.
	bareName = regexp.MustCompile("^(?:([A-Za-z_]\\w*)|`([^`]+)`)$")
	// aliasProjection matches `expr AS name`.
	aliasProjection = regexp.MustCompile(`(?is)^(.+?)\s+AS\s+(\w+)$`)
)

// parseLabelItems reads the SET or REMOVE items of one clause span that add or
// remove labels. pos is the span's offset in the statement.
func parseLabelItems(span string, pos int) []labelOp {
	var out []labelOp
	offset := pos
	for _, item := range splitTopLevel(span) {
		if m := labelItem.FindStringSubmatch(strings.TrimSpace(item)); m != nil {
			out = append(out, labelOp{variable: m[1], labels: parseLabels(m[2]), pos: offset})
		}
		offset += len(item) + 1
	}
	return out
}

// addScopeResets records where top-level WITH and UNION end a variable's scope.
// A WITH keeps only the names it projects bare (or `n AS n`), plus everything
// for `WITH *`; a UNION starts a fresh scope. Each dropped name gets a reset
// occurrence, so a later unlabeled re-declaration does not inherit the labels,
// or the relationship role, the name had before. Clauses inside brackets (a
// CALL subquery) are not modelled and leave the scope as it was.
func addScopeResets(parsed *parsedStatement, text string, clauses []clause) {
	for i, c := range clauses {
		if c.depth != 0 {
			continue
		}
		switch c.name {
		case "UNION":
			resetNames(parsed, c.start, nil)
		case "WITH":
			kept, all := projectedNames(text[c.end:clauseSpanEnd(text, clauses, i)])
			if !all {
				resetNames(parsed, c.start, kept)
			}
		}
	}
}

// resetNames appends a reset occurrence at pos for every known name not in kept.
func resetNames(parsed *parsedStatement, pos int, kept map[string]bool) {
	for name := range parsed.occurrences {
		if !kept[name] {
			parsed.occurrences[name] = append(parsed.occurrences[name], occurrence{pos: pos, reset: true})
		}
	}
}

// projectedNames returns the names a WITH projection keeps, and whether it is
// `WITH *`.
func projectedNames(span string) (map[string]bool, bool) {
	kept := make(map[string]bool)
	span = strings.TrimSpace(span)
	if len(span) >= 8 && strings.EqualFold(span[:8], "DISTINCT") {
		span = strings.TrimSpace(span[8:])
	}
	for _, item := range splitTopLevel(span) {
		item = strings.TrimSpace(item)
		if item == "*" {
			return nil, true
		}
		if name, ok := bareIdentifier(item); ok {
			kept[name] = true
			continue
		}
		if m := aliasProjection.FindStringSubmatch(item); m != nil {
			expr, alias := strings.TrimSpace(m[1]), m[2]
			if name, ok := bareIdentifier(expr); ok && name == alias {
				kept[alias] = true
			}
		}
	}
	return kept, false
}

func bareIdentifier(s string) (string, bool) {
	m := bareName.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1], true
	}
	return m[2], true
}

// nearestIsRelationship reports whether the nearest occurrence of the write's
// variable at or before the write is a relationship, not a node. Only then is
// the write a relationship property write the anchor does not care about.
func (p parsedStatement) nearestIsRelationship(write idWrite) bool {
	nearest := false
	for _, occ := range p.occurrences[write.variable] {
		if occ.pos > write.pos {
			break
		}
		nearest = occ.relationship
	}
	return nearest
}

// backtickMask marks every byte inside a backtick-quoted identifier, quotes
// included, so keyword matching can skip identifier text.
func backtickMask(text string) []bool {
	mask := make([]bool, len(text)+1)
	for i := 0; i < len(text); i++ {
		if text[i] != '`' {
			continue
		}
		start := i
		i++
		for i < len(text) {
			if text[i] == '`' {
				if i+1 < len(text) && text[i+1] == '`' {
					i += 2
					continue
				}
				break
			}
			i++
		}
		for k := start; k <= i && k < len(text); k++ {
			mask[k] = true
		}
	}
	return mask
}

// removalFindings returns a finding for each REMOVE n:L of an anchor label that
// leaves n with no anchor label: labels still on n from its pattern, plus labels
// the statement adds, minus every label the statement removes from n.
func removalFindings(p parsedStatement, statement Statement, anchorLabels map[string]bool) []Finding {
	removed := make(map[string]map[string]bool)
	for _, op := range p.removals {
		if removed[op.variable] == nil {
			removed[op.variable] = make(map[string]bool)
		}
		for _, label := range op.labels {
			removed[op.variable][label] = true
		}
	}
	var findings []Finding
	reported := make(map[string]bool)
	for _, op := range p.removals {
		stripsAnchor := false
		for _, label := range op.labels {
			stripsAnchor = stripsAnchor || anchorLabels[label]
		}
		if !stripsAnchor || reported[op.variable] {
			continue
		}
		remaining := p.labelsOf(idWrite{variable: op.variable, pos: op.pos})
		for _, add := range p.adds {
			if add.variable == op.variable {
				remaining = append(remaining, add.labels...)
			}
		}
		stillAnchored := false
		for _, label := range remaining {
			if anchorLabels[label] && !removed[op.variable][label] {
				stillAnchored = true
			}
		}
		if stillAnchored {
			continue
		}
		reported[op.variable] = true
		findings = append(findings, Finding{
			Variable: op.variable, Labels: op.labels, Kind: KindLabelRemoval,
			Statement: statement.Text, Callsite: statement.Callsite,
		})
	}
	return findings
}
