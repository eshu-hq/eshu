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
}

// parsedStatement is the part of a statement the id-write check reads.
type parsedStatement struct {
	writes           []idWrite
	varLabels        map[string]map[string]bool
	relationshipVars map[string]bool
	unwindAliases    map[string]string
}

// labelsOf returns the sorted labels of variable plus any extra pattern
// labels.
func (p parsedStatement) labelsOf(variable string, extra []string) []string {
	set := make(map[string]bool)
	for label := range p.varLabels[variable] {
		set[label] = true
	}
	for _, label := range extra {
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
	nodePattern = regexp.MustCompile(`\(\s*([A-Za-z_]\w*)?\s*((?::[^{)]*)?)(\{[^{}]*\})?\s*\)`)
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
		varLabels:        make(map[string]map[string]bool),
		relationshipVars: make(map[string]bool),
		unwindAliases:    make(map[string]string),
	}
	for _, m := range unwindPattern.FindAllStringSubmatch(text, -1) {
		parsed.unwindAliases[m[2]] = m[1]
	}
	for _, m := range relationshipPattern.FindAllStringSubmatch(text, -1) {
		parsed.relationshipVars[m[1]] = true
	}
	clauses := clausePositions(text)
	var nodeRanges [][2]int
	for _, loc := range nodePattern.FindAllStringSubmatchIndex(text, -1) {
		if !isPatternStart(text, loc[0]) {
			continue
		}
		nodeRanges = append(nodeRanges, [2]int{loc[0], loc[1]})
		variable := slice(text, loc[2], loc[3])
		labels := parseLabels(slice(text, loc[4], loc[5]))
		if variable != "" {
			if parsed.varLabels[variable] == nil {
				parsed.varLabels[variable] = make(map[string]bool)
			}
			for _, label := range labels {
				parsed.varLabels[variable][label] = true
			}
		}
		propertyMap := slice(text, loc[6], loc[7])
		context := clauseAt(clauses, loc[0])
		if propertyMap != "" && (context == "MERGE" || context == "CREATE") && idKeyInMap.MatchString(propertyMap) {
			parsed.writes = append(parsed.writes, idWrite{
				variable: variable, kind: KindMapKey, patternLabels: labels,
			})
		}
	}
	for i, clause := range clauses {
		end := clauseSpanEnd(text, clauses, i)
		switch clause.name {
		case "SET":
			for _, item := range splitTopLevel(text[clause.end:end]) {
				parsed.writes = append(parsed.writes, setItemWrites(strings.TrimSpace(item))...)
			}
		case "MERGE", "CREATE":
			// Fail closed: an id key left in a write clause after the node
			// patterns and relationship brackets are masked sits in a shape the
			// parser did not place, so no label can be proven for it.
			if idKeyInMap.MatchString(maskedSpan(text, clause.end, end, nodeRanges)) {
				parsed.writes = append(parsed.writes, idWrite{kind: KindMapKey})
			}
		}
	}
	return parsed
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

// maskedSpan returns text[from:to] with the recognized node patterns and the
// relationship brackets blanked out.
func maskedSpan(text string, from, to int, nodeRanges [][2]int) string {
	masked := []byte(text[from:to])
	blank := func(a, b int) {
		for k := max(a, from); k < min(b, to); k++ {
			masked[k-from] = ' '
		}
	}
	for _, r := range nodeRanges {
		blank(r[0], r[1])
	}
	for _, r := range relationshipBrackets(text) {
		blank(r[0], r[1])
	}
	return string(masked)
}

// relationshipBrackets returns the [start, end) ranges of every bracket
// group opened by an arrow dash: the -[r:TYPE {props}]- of a pattern.
func relationshipBrackets(text string) [][2]int {
	var out [][2]int
	for i := 0; i < len(text); i++ {
		if text[i] != '-' {
			continue
		}
		j := i + 1
		for j < len(text) && (text[j] == ' ' || text[j] == '\n' || text[j] == '\t') {
			j++
		}
		if j >= len(text) || text[j] != '[' {
			continue
		}
		depth := 0
		for k := j; k < len(text); k++ {
			switch text[k] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					out = append(out, [2]int{j, k + 1})
					i = k
					k = len(text)
				}
			}
		}
	}
	return out
}

// setItemWrites classifies one SET assignment item.
func setItemWrites(item string) []idWrite {
	if m := setPropertyID.FindStringSubmatch(item); m != nil {
		return []idWrite{{variable: m[1], kind: KindSetProperty}}
	}
	if m := setDynamicKey.FindStringSubmatch(item); m != nil {
		return []idWrite{{variable: m[1], kind: KindDynamicMap, keyExpression: strings.TrimSpace(m[2])}}
	}
	m := setWholeMap.FindStringSubmatch(item)
	if m == nil {
		return nil
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
	for _, loc := range clauseKeyword.FindAllStringIndex(text, -1) {
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

// bracketDepths returns the bracket depth, over (), [], and {}, at every byte.
func bracketDepths(text string) []int {
	depths := make([]int, len(text)+1)
	depth := 0
	for i := 0; i < len(text); i++ {
		depths[i] = depth
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	depths[len(text)] = depth
	return depths
}

// clauseAt returns the name of the last clause keyword before position pos.
func clauseAt(clauses []clause, pos int) string {
	name := ""
	for _, c := range clauses {
		if c.start >= pos {
			break
		}
		name = c.name
	}
	return name
}

// isPatternStart reports whether the '(' at pos opens a node pattern rather
// than a function call: a '(' glued to a preceding identifier is a call
// unless that identifier is a clause keyword.
func isPatternStart(text string, pos int) bool {
	if pos == 0 || !isWordByte(text[pos-1]) {
		return true
	}
	end := pos
	start := end
	for start > 0 && isWordByte(text[start-1]) {
		start--
	}
	switch strings.ToUpper(text[start:end]) {
	case "MERGE", "MATCH", "CREATE":
		return true
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func previousNonSpace(text string, pos int) byte {
	for i := pos - 1; i >= 0; i-- {
		if text[i] != ' ' && text[i] != '\n' && text[i] != '\t' {
			return text[i]
		}
	}
	return 0
}

func slice(text string, from, to int) string {
	if from < 0 || to < 0 {
		return ""
	}
	return text[from:to]
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

// splitTopLevel splits text on commas that sit outside (), [], and {}.
func splitTopLevel(text string) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, text[last:i])
				last = i + 1
			}
		}
	}
	return append(out, text[last:])
}

// blankLiteralsAndComments replaces string-literal contents and comments with
// nothing, so text inside them never reads as Cypher. Quote characters stay.
func blankLiteralsAndComments(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 4
		case c == '\'' || c == '"':
			b.WriteByte(c)
			i++
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == c {
					break
				}
				i++
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
