// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Store tokens are the closed vocabulary of the capability matrix's
// answering_stores field (see capabilitycatalog.ValidAnsweringStore).
const (
	// StoreFactRecordsReducer is fact_records read through a reducer-owned
	// kind (a kind whose name starts with "reducer_").
	StoreFactRecordsReducer = "fact_records_reducer"
	// StoreFactRecordsCollector is fact_records read through any other kind.
	StoreFactRecordsCollector = "fact_records_collector"
	// StoreContentSearch is the content store and search index.
	StoreContentSearch = "content_search"
	// StoreGraph is the graph backend.
	StoreGraph = "graph"
	// ReadTablePrefix prefixes a purpose-built read table token.
	ReadTablePrefix = "read_table:"

	reducerKindPrefix = "reducer_"
	factRecordsTable  = "fact_records"
)

// Statement is one SQL text with its bind arguments, as a recorder captured it
// or as a census extracted it.
type Statement struct {
	SQL  string
	Args []any
}

// Used is the set of stores one or more statements read. Stores maps each
// token to the first piece of evidence that produced it. Unresolved lists
// reads the classifier could not attribute to a store; an audit treats each as
// a failure, so a read it cannot see never passes silently.
type Used struct {
	Stores     map[string]string
	Unresolved []string
}

// structuralTables are lifecycle and authority joins that scope a read to the
// active generation or to protected classifications. They never answer a
// capability, so they carry no store token. Each entry states why.
var structuralTables = map[string]string{
	"ingestion_scopes":  "active-generation lifecycle join",
	"scope_generations": "active-generation lifecycle join",

	"scanner_image_result_fact_classifications":  "protected finding classification join, not an answering store",
	"scanner_image_result_fact_associations":     "protected finding association join, not an answering store",
	"scanner_image_result_evidence_associations": "protected evidence association join, not an answering store",
	"scanner_image_result_advisory_dependencies": "protected advisory dependency join, not an answering store",

	"supply_chain_impact_winners_materialization": "freshness watermark of the winners read model, metadata of the answering table",
}

// relationAliases maps views and set-returning functions to the read table
// the capability matrix names.
var relationAliases = map[string]string{
	"container_image_identity_current_supports":          ReadTablePrefix + "container_image_identity_supports",
	"container_image_identity_current_support_facts_for": ReadTablePrefix + "container_image_identity_supports",
}

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	stringLit    = regexp.MustCompile(`'(?:[^']|'')*'`)
	kindEquals   = regexp.MustCompile(`(?i)\bfact_kind\s*=\s*'([^']+)'`)
	kindIn       = regexp.MustCompile(`(?i)\bfact_kind\s+in\s*\(([^)]*)\)`)
	kindBind     = regexp.MustCompile(`(?i)\bfact_kind\s*=\s*(?:any\s*\(\s*)?\$(\d+)`)
	quoted       = regexp.MustCompile(`'([^']+)'`)
	cteName      = regexp.MustCompile(`(?i)(?:\bwith\s+(?:recursive\s+)?|,\s*)([a-z_][a-z0-9_]*)\s+as\s+(?:not\s+)?(?:materialized\s*)?\(`)
	relation     = regexp.MustCompile(`(?i)\b(?:from|join)\s+(?:only\s+)?((?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*)(\s*\()?`)
	notRelation  = regexp.MustCompile(`(?i)\bis\s+(?:not\s+)?distinct\s+from\b|\bextract\s*\(\s*[a-z_]+\s+from\b|\b(?:trim|substring|overlay)\s*\([^)]*\bfrom\b`)
)

// sqlWords are identifiers a FROM or JOIN clause can name that are not tables.
var sqlWords = map[string]struct{}{
	"select": {}, "lateral": {}, "values": {}, "only": {}, "unnest": {}, "generate_series": {},
}

// Classify attributes every statement to the stores it reads. Literal kinds
// resolve from the text; bind kinds resolve from Args. A fact_records read with
// no resolvable kind, and a relation the classifier cannot name, land in
// Unresolved instead of being dropped.
func Classify(statements ...Statement) Used {
	used := Used{Stores: map[string]string{}}
	for _, statement := range statements {
		classifyOne(statement, censusScope{}, &used)
	}
	sort.Strings(used.Unresolved)
	return used
}

// censusScope carries what a literal fragment cannot see for itself. The zero
// value classifies a complete recorded statement, where nothing is assumed.
type censusScope struct {
	// fallbackKinds apply to a fact_records read whose own text and arguments
	// name no kind: a census passes the kinds its package names elsewhere,
	// since a fragment cannot see its binds.
	fallbackKinds []string
	// ctes are CTE names defined in sibling fragments of the same package.
	ctes map[string]struct{}
	// known, when set, restricts relations to tables and views the schema
	// creates, so a CTE alias or a word in prose never reads as a table.
	known map[string]struct{}
}

func classifyOne(statement Statement, scope censusScope, used *Used) {
	text := dropDeadBranches(blockComment.ReplaceAllString(lineComment.ReplaceAllString(statement.SQL, ""), ""))
	kinds := factKinds(text, statement.Args)
	if len(kinds) == 0 {
		kinds = scope.fallbackKinds
	}
	bare := notRelation.ReplaceAllString(stringLit.ReplaceAllString(text, "''"), " ")
	ctes := map[string]struct{}{}
	for _, match := range cteName.FindAllStringSubmatch(bare, -1) {
		ctes[strings.ToLower(match[1])] = struct{}{}
	}
	evidence := firstLine(statement.SQL)
	for _, match := range relation.FindAllStringSubmatch(bare, -1) {
		name := strings.TrimPrefix(strings.ToLower(match[1]), "public.")
		isFunction := strings.TrimSpace(match[2]) != ""
		if _, ok := ctes[name]; ok {
			continue
		}
		if _, ok := scope.ctes[name]; ok {
			continue
		}
		if token, ok := relationAliases[name]; ok {
			add(used, token, evidence)
			continue
		}
		if _, ok := sqlWords[name]; ok || isFunction {
			continue
		}
		if _, ok := structuralTables[name]; ok {
			continue
		}
		if name == factRecordsTable {
			if len(kinds) == 0 {
				used.Unresolved = append(used.Unresolved, "fact_records read with no resolvable fact_kind: "+evidence)
			}
			for _, kind := range kinds {
				add(used, factRecordsToken(kind), evidence+" [fact_kind "+kind+"]")
			}
			continue
		}
		if scope.known != nil {
			if _, ok := scope.known[name]; !ok {
				continue
			}
		}
		if strings.HasPrefix(name, "content_") || strings.HasPrefix(name, "eshu_search_") {
			add(used, StoreContentSearch, evidence)
			continue
		}
		add(used, ReadTablePrefix+name, evidence)
	}
}

func add(used *Used, token, evidence string) {
	if _, ok := used.Stores[token]; !ok {
		used.Stores[token] = evidence
	}
}

func factRecordsToken(kind string) string {
	if strings.HasPrefix(kind, reducerKindPrefix) {
		return StoreFactRecordsReducer
	}
	return StoreFactRecordsCollector
}

// factKinds returns the fact kinds a statement filters on: literal equality,
// IN lists, and bind placeholders resolved against args.
func factKinds(text string, args []any) []string {
	seen := map[string]struct{}{}
	var out []string
	push := func(kind string) {
		if kind == "" {
			return
		}
		if _, ok := seen[kind]; !ok {
			seen[kind] = struct{}{}
			out = append(out, kind)
		}
	}
	for _, match := range kindEquals.FindAllStringSubmatch(text, -1) {
		push(match[1])
	}
	for _, match := range kindIn.FindAllStringSubmatch(text, -1) {
		for _, item := range quoted.FindAllStringSubmatch(match[1], -1) {
			push(item[1])
		}
	}
	for _, match := range kindBind.FindAllStringSubmatch(text, -1) {
		index, err := strconv.Atoi(match[1])
		if err != nil || index < 1 || index > len(args) {
			continue
		}
		for _, kind := range stringsOf(args[index-1]) {
			push(kind)
		}
	}
	sort.Strings(out)
	return out
}

func stringsOf(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []string:
		return typed
	case array.StringArray:
		return []string(typed)
	case *array.StringArray:
		if typed == nil {
			return nil
		}
		return []string(*typed)
	default:
		return nil
	}
}

func firstLine(sql string) string {
	for _, line := range strings.Split(sql, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if len(trimmed) > 100 {
				trimmed = trimmed[:100]
			}
			return trimmed
		}
	}
	return ""
}

var (
	deadConjunct = regexp.MustCompile(`(?i)\band\s+false\s*\)`)
	deadOrArm    = regexp.MustCompile(`(?i)\(\s*not\s+false\s*\)\s+or\s*(\()`)
)

// dropDeadBranches removes the parts of a statement that a literal makes
// unreachable, so the tables and kinds they name are not counted as reads:
//
//   - a parenthesized group that ends in "AND FALSE" is false whatever precedes it;
//   - the group after "(NOT FALSE) OR" is never needed, because the left arm is true.
//
// The impact reads close their protected derived branch this way until the
// finding-authority gate opens, and the closed text still names the scanner
// authority tables. String literals are masked first so a parenthesis inside one
// cannot unbalance the walk.
func dropDeadBranches(text string) string {
	masked := stringLit.ReplaceAllStringFunc(text, func(lit string) string {
		return strings.Repeat("x", len(lit))
	})
	type span struct{ start, end int }
	var dead []span
	for _, loc := range deadConjunct.FindAllStringIndex(masked, -1) {
		if open, ok := matchingOpen(masked, loc[1]-1); ok {
			dead = append(dead, span{open, loc[1]})
		}
	}
	for _, loc := range deadOrArm.FindAllStringSubmatchIndex(masked, -1) {
		if closeAt, ok := matchingClose(masked, loc[2]); ok {
			dead = append(dead, span{loc[2], closeAt + 1})
		}
	}
	if len(dead) == 0 {
		return text
	}
	sort.Slice(dead, func(i, j int) bool { return dead[i].start < dead[j].start })
	var out strings.Builder
	cursor := 0
	for _, d := range dead {
		if d.start < cursor {
			continue
		}
		out.WriteString(text[cursor:d.start])
		cursor = d.end
	}
	out.WriteString(text[cursor:])
	return out.String()
}

// matchingOpen returns the index of the parenthesis that opens the group
// closed at closeAt.
func matchingOpen(text string, closeAt int) (int, bool) {
	depth := 0
	for i := closeAt; i >= 0; i-- {
		switch text[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// matchingClose returns the index of the parenthesis that closes the group
// opened at openAt.
func matchingClose(text string, openAt int) (int, bool) {
	depth := 0
	for i := openAt; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
