// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"regexp"
	"sort"
	"strings"
)

// idWrite is one place a statement writes, or may write, a node id.
type idWrite struct {
	variable      string
	kind          string
	patternLabels []string
	// expression is the right-hand side of a dynamic map write.
	expression string
	// keyExpression is the bracketed key of a dynamic property write,
	// SET n[<key>] = ...
	keyExpression string
	// pos is where the write sits in the scanned text. Variable labels are
	// read from the nearest preceding labeled occurrence, so a variable
	// rebound later in the statement does not inherit an earlier label.
	pos int
}

// occurrence is one place a variable appears in a node pattern, with the
// labels the pattern gives it (possibly none).
type occurrence struct {
	pos    int
	labels []string
	// reset marks `AS name`: the name is bound again to a value whose labels
	// the scan does not know, so earlier labels no longer apply.
	reset bool
	// relationship marks a relationship variable, -[r]-: not a node.
	relationship bool
}

// parsedStatement is the part of a statement the id-write check reads.
type parsedStatement struct {
	writes        []idWrite
	occurrences   map[string][]occurrence
	unwindAliases map[string]string
	// adds and removals are the SET n:L and REMOVE n:L label operations.
	adds, removals []labelOp
}

// labelsOf returns the sorted labels of the write's variable: the labels of
// its nearest preceding labeled occurrence, plus the write's own pattern
// labels. A variable rebound by a later pattern takes the later labels, so an
// earlier anchor label never covers a write on the rebound node.
func (p parsedStatement) labelsOf(write idWrite) []string {
	set := make(map[string]bool)
	var nearest []string
	for _, occ := range p.occurrences[write.variable] {
		if occ.pos > write.pos {
			break
		}
		if occ.reset {
			nearest = nil
		} else if len(occ.labels) > 0 {
			nearest = occ.labels
		}
	}
	for _, label := range nearest {
		set[label] = true
	}
	for _, label := range write.patternLabels {
		set[label] = true
	}
	out := make([]string, 0, len(set))
	for label := range set {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

var (
	// clauseKeyword finds the Cypher clause keywords that decide whether a
	// pattern or an assignment is a read or a write. Keywords match any case.
	clauseKeyword = regexp.MustCompile(`(?i)\b(ON\s+CREATE|ON\s+MATCH|OPTIONAL\s+MATCH|MATCH|MERGE|CREATE|SET|WHERE|WITH|UNWIND|RETURN|DETACH\s+DELETE|DELETE|REMOVE|CALL|UNION|FOREACH|ORDER\s+BY|LIMIT|SKIP)\b`)
	// nodePattern matches a node pattern: (var:Label1:Label2 {map}). The label
	// part is everything up to the property map or the closing parenthesis, so
	// a label expression (A&B, A|B, !A) reaches parseLabels instead of hiding
	// the pattern.
	nodePattern = regexp.MustCompile(`\(\s*([A-Za-z_]\w*)?\s*((?::[^{)]*)?)(\{[^{}]*\}|\$\w+)?\s*\)`)
	// trailingParameterMap splits a parameter property map, (n:L $props), off
	// the end of the label part the node pattern swallowed.
	trailingParameterMap = regexp.MustCompile(`^(.*?)\s+(\$\w+)\s*$`)
	// procedureWrite finds procedures that create or merge nodes from arguments
	// the parser cannot read.
	procedureWrite = regexp.MustCompile(`(?i)\bapoc\.(create|merge|cypher|do|periodic|refactor)\.`)
	// aliasRebind finds names bound again after an UNWIND: AS name.
	aliasRebind = regexp.MustCompile(`(?i)\bAS\s+(\w+)`)
	// aliasShadow finds names bound by a comprehension or FOREACH: (name IN and
	// [name IN.
	aliasShadow = regexp.MustCompile(`(?i)[(\[]\s*(\w+)\s+IN\b`)
	// setTarget captures the left side of a SET item up to its first assignment.
	setTarget = regexp.MustCompile(`^(.*?)\s*(?:\+=|=)(?:[^=]|$)`)
	// plainProperty matches the target of a non-id property assignment on a plain
	// variable: n.name, n.`my prop`, n.é.
	plainProperty = regexp.MustCompile("^[A-Za-z_]\\w*\\s*\\.\\s*(?:[^.\\[\\]()\\s`]+|`[^`]+`)$")
	// idProperty matches a target that ends in the id property.
	idProperty = regexp.MustCompile("\\.\\s*(?:id|`id`)\\s*$")
	// relationshipPattern matches a relationship variable, [r:TYPE] or [r],
	// only when an arrow dash opens the bracket, so a list literal [n] or a
	// list index x[i] is never taken for one.
	relationshipPattern = regexp.MustCompile(`-\s*\[\s*([A-Za-z_]\w*)\s*(?:[:\]*{|])`)
	unwindPattern       = regexp.MustCompile(`(?i)\bUNWIND\s+\$(\w+)\s+AS\s+(\w+)`)
	idKeyInMap          = regexp.MustCompile("(?:^|[{,])\\s*(?:id|`id`)\\s*:")
	setPropertyID       = regexp.MustCompile("^(\\w+)\\s*\\.\\s*(?:id|`id`)\\s*=(?:[^=]|$)")
	setWholeMap         = regexp.MustCompile(`^(\w+)\s*(\+=|=)\s*(.+)$`)
	setDynamicKey       = regexp.MustCompile(`^(\w+)\s*\[(.+)\]\s*\+?=[^=]`)
)

// parseStatement extracts the id writes of one statement.
func parseStatement(raw string) parsedStatement {
	text := blankLiteralsAndComments(raw)
	parsed := parsedStatement{
		occurrences:   make(map[string][]occurrence),
		unwindAliases: unwindAliasesOf(text),
	}
	for _, loc := range relationshipPattern.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		parsed.occurrences[name] = append(parsed.occurrences[name], occurrence{pos: loc[0], relationship: true})
	}
	if loc := procedureWrite.FindStringIndex(text); loc != nil {
		// A procedure that creates or merges a node takes labels and properties
		// as arguments the scan does not read: an unplaced write.
		parsed.writes = append(parsed.writes, idWrite{variable: "procedure", kind: KindDynamicMap, pos: loc[0]})
	}
	for _, loc := range aliasRebind.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		parsed.occurrences[name] = append(parsed.occurrences[name], occurrence{pos: loc[0], reset: true})
	}
	clauses := clausePositions(text)
	depths := bracketDepths(text)
	var nodeRanges [][2]int
	for _, loc := range nodePattern.FindAllStringSubmatchIndex(text, -1) {
		if !isPatternStart(text, loc[0]) {
			continue
		}
		nodeRanges = append(nodeRanges, [2]int{loc[0], loc[1]})
		variable := slice(text, loc[2], loc[3])
		labelPart := slice(text, loc[4], loc[5])
		propertyMap := slice(text, loc[6], loc[7])
		if m := trailingParameterMap.FindStringSubmatch(labelPart); m != nil && propertyMap == "" {
			labelPart, propertyMap = m[1], m[2]
		}
		labels := parseLabels(labelPart)
		if variable != "" {
			parsed.occurrences[variable] = append(parsed.occurrences[variable], occurrence{pos: loc[0], labels: labels})
		}
		context := clauseAt(clauses, loc[0], depths[loc[0]])
		if propertyMap == "" || (context != "MERGE" && context != "CREATE") {
			continue
		}
		switch {
		case strings.HasPrefix(propertyMap, "$"):
			parsed.writes = append(parsed.writes, idWrite{
				variable: variable, kind: KindDynamicMap, patternLabels: labels, expression: propertyMap, pos: loc[0],
			})
		case idKeyInMap.MatchString(propertyMap):
			parsed.writes = append(parsed.writes, idWrite{
				variable: variable, kind: KindMapKey, patternLabels: labels, pos: loc[0],
			})
		}
	}
	addScopeResets(&parsed, text, clauses)
	for _, occs := range parsed.occurrences {
		sort.SliceStable(occs, func(i, j int) bool { return occs[i].pos < occs[j].pos })
	}
	for i, clause := range clauses {
		end := clauseSpanEnd(text, clauses, i)
		switch clause.name {
		case "SET":
			offset := clause.end
			for _, item := range splitTopLevel(text[clause.end:end]) {
				for _, write := range setItemWrites(strings.TrimSpace(item)) {
					write.pos = offset
					parsed.writes = append(parsed.writes, write)
				}
				offset += len(item) + 1
			}
			parsed.adds = append(parsed.adds, parseLabelItems(text[clause.end:end], clause.end)...)
		case "REMOVE":
			parsed.removals = append(parsed.removals, parseLabelItems(text[clause.end:end], clause.end)...)
		case "MERGE", "CREATE":
			// Fail closed: an id key left in a write clause after the node
			// patterns and relationship brackets are masked sits in a shape the
			// parser did not place, so no label can be proven for it.
			if idKeyInMap.MatchString(maskedSpan(text, clause.end, end, nodeRanges)) {
				parsed.writes = append(parsed.writes, idWrite{kind: KindMapKey, pos: clause.end})
			}
		}
	}
	return parsed
}

// unwindAliasesOf maps each `UNWIND $param AS alias` to its parameter, minus any
// alias bound again later in the statement (a second `AS alias`, a
// comprehension variable, a FOREACH variable): after a rebind the parameter no
// longer says what the alias holds.
func unwindAliasesOf(text string) map[string]string {
	aliases := make(map[string]string)
	for _, m := range unwindPattern.FindAllStringSubmatch(text, -1) {
		aliases[m[2]] = m[1]
	}
	bound := make(map[string]int)
	for _, m := range aliasRebind.FindAllStringSubmatch(text, -1) {
		bound[m[1]]++
	}
	for name := range aliases {
		if bound[name] > 1 {
			delete(aliases, name)
		}
	}
	for _, m := range aliasShadow.FindAllStringSubmatch(text, -1) {
		delete(aliases, m[1])
	}
	return aliases
}

// clauseSpanEnd returns where the clause at index i stops: the next clause
// keyword at the same or a shallower bracket depth. A keyword inside brackets,
// such as the WHERE of a list comprehension or the MATCH of an EXISTS
// subquery, is part of the clause, not the start of the next one.
func clauseSpanEnd(text string, clauses []clause, i int) int {
	for j := i + 1; j < len(clauses); j++ {
		if clauses[j].depth <= clauses[i].depth {
			return clauses[j].start
		}
	}
	return len(text)
}

// setItemWrites classifies one SET assignment item by its target. A plain
// variable takes the whole-map forms, a plain variable's property takes the id
// check, and a plain variable with a bracketed key is a dynamic key. Any other
// target (a backtick or non-ASCII name, an expression) is an unplaced write: the
// scan cannot name the node it writes.
func setItemWrites(item string) []idWrite {
	if m := setPropertyID.FindStringSubmatch(item); m != nil {
		return []idWrite{{variable: m[1], kind: KindSetProperty}}
	}
	t := setTarget.FindStringSubmatch(item)
	if t == nil {
		return nil
	}
	target := strings.TrimSpace(t[1])
	switch {
	case idProperty.MatchString(target):
		return []idWrite{{kind: KindSetProperty}}
	case plainProperty.MatchString(target):
		return nil
	}
	if m := setDynamicKey.FindStringSubmatch(item); m != nil {
		return []idWrite{{variable: m[1], kind: KindDynamicMap, keyExpression: strings.TrimSpace(m[2])}}
	}
	m := setWholeMap.FindStringSubmatch(item)
	if m == nil {
		return []idWrite{{kind: KindDynamicMap}}
	}
	expression := strings.TrimSpace(m[3])
	if strings.HasPrefix(expression, "{") {
		if idKeyInMap.MatchString(expression) {
			return []idWrite{{variable: m[1], kind: KindDynamicMap, expression: expression}}
		}
		return nil
	}
	return []idWrite{{variable: m[1], kind: KindDynamicMap, expression: expression}}
}

// clause is a clause keyword and the span it governs.
type clause struct {
	name       string
	start, end int
	// depth is the bracket depth, over (), [], and {}, at the keyword.
	depth int
}

// clausePositions lists the clause keywords of text in order. A match
// preceded by ':' or '.' is a label or property that spells a keyword, not a
// clause.
func clausePositions(text string) []clause {
	var out []clause
	depths := bracketDepths(text)
	inBacktick := backtickMask(text)
	for _, loc := range clauseKeyword.FindAllStringIndex(text, -1) {
		if inBacktick[loc[0]] {
			continue
		}
		if prev := previousNonSpace(text, loc[0]); prev == ':' || prev == '.' || prev == '`' {
			continue
		}
		name := strings.ToUpper(strings.Join(strings.Fields(text[loc[0]:loc[1]]), " "))
		switch name {
		case "ON CREATE", "ON MATCH":
			name = "ON"
		case "OPTIONAL MATCH":
			name = "MATCH"
		}
		out = append(out, clause{name: name, start: loc[0], end: loc[1], depth: depths[loc[0]]})
	}
	return out
}

// clauseAt returns the name of the last clause keyword before position pos
// whose bracket depth does not exceed depth, the pattern's own depth. A keyword
// inside an earlier pattern's map or a list comprehension is deeper than a later
// top-level pattern and never becomes its context.
func clauseAt(clauses []clause, pos, depth int) string {
	name := ""
	for _, c := range clauses {
		if c.start >= pos {
			break
		}
		if c.depth <= depth {
			name = c.name
		}
	}
	return name
}

// labelExpressionUnknown stands in for a label expression the parser cannot
// reduce to a set of labels (A|B, !A, %). It is never an anchor label, so a
// node known only by such an expression is not proven covered.
const labelExpressionUnknown = "<label-expression>"

// parseLabels splits ":A:B" and ":A&B" into label names. A disjunction,
// negation, or wildcard reduces to [labelExpressionUnknown]: the node may carry
// only the uncovered alternative.
func parseLabels(raw string) []string {
	if strings.ContainsAny(raw, "|!%()") {
		return []string{labelExpressionUnknown}
	}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ':' || r == '&' }) {
		part = strings.Trim(strings.TrimSpace(part), "`")
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
