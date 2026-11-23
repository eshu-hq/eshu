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
// token to the first piece of evidence that produced it. Unresolved lists the
// reads and the statement shapes the classifier could not attribute to a store
// (a fact_records read with no resolvable kind, an unparseable FROM item, a CTE
// that shadows a store, unbalanced parentheses, an unterminated literal); an
// audit treats each as a failure. The classifier fails closed on the shapes it
// knows it cannot read; a shape it misreads without knowing is what the seeded
// cases in classify_test.go and scrub_test.go pin one by one.
type Used struct {
	Stores     map[string]string
	Unresolved []string
}

// systemCatalogPrefixes name the relations that are schema introspection, not a
// store: PostgreSQL system catalogs and information_schema. A read of them
// answers no capability.
var systemCatalogPrefixes = []string{"pg_", "information_schema."}

func isSystemCatalog(name string) bool {
	for _, prefix := range systemCatalogPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
}

// relationAliases maps views and set-returning functions to the read table
// the capability matrix names.
var relationAliases = map[string]string{
	// The winners freshness watermark is written with the winners rows by one
	// resweep statement and read by winnersFreshness to mark the answer stale or
	// unavailable, so it is the winners store, not a separate one.
	"supply_chain_impact_winners_materialization": ReadTablePrefix + "supply_chain_impact_canonical_winners",

	// The supports readiness predicate reads the backfill marker and the old-writer
	// fence before every supports-served call; both gate the per-support table, so
	// they are that store and not separate ones.
	"supply_chain_impact_finding_supports_backfill": ReadTablePrefix + "supply_chain_impact_finding_supports",
	"supply_chain_impact_finding_supports_dirty":    ReadTablePrefix + "supply_chain_impact_finding_supports",

	"container_image_identity_current_supports":          ReadTablePrefix + "container_image_identity_supports",
	"container_image_identity_current_support_facts_for": ReadTablePrefix + "container_image_identity_supports",
}

// sqlWords are identifiers a FROM or JOIN clause can name that are not tables.
var sqlWords = map[string]struct{}{
	"select": {}, "lateral": {}, "values": {}, "only": {}, "unnest": {}, "generate_series": {},
}

var cteName = regexp.MustCompile(`(?i)(?:\bwith\s+(?:recursive\s+)?|,\s*)([a-z_][a-z0-9_]*)\s+as\s+(?:not\s+)?(?:materialized\s*)?\(`)

// kindNotIn and kindNotLike match the negated forms on fact_kind, which exclude
// kinds instead of naming the ones a statement reads.
var (
	kindNotIn   = regexp.MustCompile(`(?i)\bfact_kind\s+not\s+in\s*\(([^)]*)\)`)
	kindNotLike = regexp.MustCompile(`(?i)\bfact_kind\s+not\s+(?:like|ilike|similar\s+to)\b`)
)

// negatedKindsUnattributed reports a NOT IN list that names a kind the statement
// does not also read positively, or a NOT LIKE. Such an exclusion selects
// kinds by elimination, so a statement that names another kind literal would be
// attributed to that literal alone. A NOT IN whose every kind the statement also
// reads by name is a keep-predicate over kinds already attributed.
func negatedKindsUnattributed(masked, clean string, kinds []string) bool {
	if kindNotLike.MatchString(masked) {
		return true
	}
	known := map[string]struct{}{}
	for _, kind := range kinds {
		known[kind] = struct{}{}
	}
	for _, match := range kindNotIn.FindAllStringSubmatch(clean, -1) {
		for _, item := range quoted.FindAllStringSubmatch(match[1], -1) {
			if _, ok := known[item[1]]; !ok {
				return true
			}
		}
	}
	return false
}

// factKindWord finds fact_kind, bare or qualified, as a whole word.
var factKindWord = regexp.MustCompile(`(?i)\bfact_kind\b`)

// nonFunctionWords are the words that may precede a "(" that is not a function
// call: boolean and clause keywords, set operators, window and filter clauses,
// and the words that open a subquery, an IN list, or a CTE body.
var nonFunctionWords = map[string]struct{}{
	"and": {}, "or": {}, "not": {}, "where": {}, "on": {}, "when": {}, "then": {}, "else": {}, "having": {},
	"in": {}, "any": {}, "all": {}, "some": {}, "exists": {}, "select": {}, "from": {}, "join": {}, "lateral": {},
	"as": {}, "values": {}, "union": {}, "intersect": {}, "except": {}, "by": {}, "case": {}, "between": {},
	"using": {}, "returning": {}, "set": {}, "limit": {}, "materialized": {}, "distinct": {},
	"over": {}, "filter": {}, "within": {},
}

// factKindInFunction reports fact_kind passed as a bare argument of a function
// call (lower, upper, starts_with, left, substr, coalesce, and the like). Such a
// comparison selects kinds the classifier cannot name, so a statement that also
// names another kind literal would be attributed to that literal alone. A
// comparison written inside a call, as in bool_or(fact_kind = 'x'), names its
// kind and is read normally.
func factKindInFunction(masked string) bool {
	for _, loc := range factKindWord.FindAllStringIndex(masked, -1) {
		start := loc[0]
		if dot := lastNonSpace(masked, start); dot >= 0 && masked[dot] == '.' {
			start = dot
			for start > 0 && isWordByte(masked[start-1]) {
				start--
			}
		}
		prev := lastNonSpace(masked, start)
		next := skipSpaces(masked, loc[1])
		if prev < 0 || next >= len(masked) || (masked[prev] != '(' && masked[prev] != ',') ||
			(masked[next] != ',' && masked[next] != ')') {
			continue
		}
		depth := 0
		for i := prev; i >= 0; i-- {
			switch masked[i] {
			case ')':
				depth++
			case '(':
				if depth > 0 {
					depth--
					continue
				}
				j := lastNonSpace(masked, i)
				end := j + 1
				for j >= 0 && isWordByte(masked[j]) {
					j--
				}
				word := strings.ToLower(masked[j+1 : end])
				if _, keyword := nonFunctionWords[word]; word != "" && !keyword {
					return true
				}
				i = -1
			}
		}
	}
	return false
}

// lastNonSpace returns the index of the last non-space byte before at, or -1.
func lastNonSpace(text string, at int) int {
	i := at - 1
	for i >= 0 && isSpace(text[i]) {
		i--
	}
	return i
}

// kindOperator matches a fact_kind comparison that names no kind to attribute.
var kindOperator = regexp.MustCompile(`(?i)\bfact_kind\s*(?:like\b|ilike\b|similar\s+to\b|<>|!=|~)`)

var (
	kindEquals = regexp.MustCompile(`(?i)\bfact_kind\s*=\s*'([^']+)'`)
	kindIn     = regexp.MustCompile(`(?i)\bfact_kind\s+in\s*\(([^)]*)\)`)
	kindBind   = regexp.MustCompile(`(?i)\bfact_kind\s*=\s*(?:any\s*\(\s*)?\$(\d+)`)
	quoted     = regexp.MustCompile(`'([^']+)'`)
	bindItem   = regexp.MustCompile(`\$(\d+)`)
)

// Classify attributes every statement to the stores it reads. Literal kinds
// resolve from the text; bind kinds resolve from Args. A fact_records read with
// no resolvable kind, a bind kind of a type the classifier cannot read, and any
// shape it cannot parse land in Unresolved instead of being dropped.
func Classify(statements ...Statement) Used {
	used := Used{Stores: map[string]string{}}
	for _, statement := range statements {
		classifyOne(statement, censusScope{}, &used)
	}
	sort.Strings(used.Unresolved)
	return used
}

// censusScope carries what a literal fragment cannot see for itself. The zero
// value classifies a complete recorded statement, where nothing is assumed and
// every unreadable shape fails closed.
type censusScope struct {
	// census marks a source fragment: it may be partial SQL, so unbalanced
	// parentheses or an unterminated literal skip the dead-branch pass instead
	// of failing, and a bind it cannot resolve is left to fallbackKinds.
	census bool
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
	evidence := firstLine(statement.SQL)
	fail := func(reason string) { used.Unresolved = append(used.Unresolved, reason+": "+evidence) }
	clean, masked, err := scrub(statement.SQL)
	if err != nil {
		if !scope.census {
			fail(err.Error())
			return
		}
		clean, masked = statement.SQL, statement.SQL
	}
	if dead, err := deadSpans(masked); err != nil {
		if !scope.census {
			fail(err.Error())
			return
		}
	} else {
		clean, masked = blankSpans(clean, dead), blankSpans(masked, dead)
	}
	kinds, kindProblems := factKinds(clean, statement.Args, scope.census)
	for _, problem := range kindProblems {
		fail(problem)
	}
	if !scope.census && kindOperator.MatchString(masked) {
		fail("fact_kind is compared with LIKE, SIMILAR TO, <>, !=, or a regular-expression operator, which selects kinds the classifier cannot name")
	}
	if factKindInFunction(masked) {
		fail("fact_kind is an argument of a function call, which selects kinds the classifier cannot name")
	}
	if len(kinds) > 0 && negatedKindsUnattributed(masked, clean, kinds) {
		fail("fact_kind is compared with NOT IN or NOT LIKE for a kind the statement does not also read by name, so the excluded kinds are unattributed")
	}
	if len(kinds) == 0 {
		kinds = scope.fallbackKinds
	}
	ctes := map[string]struct{}{}
	for _, match := range cteName.FindAllStringSubmatchIndex(masked, -1) {
		name := strings.ToLower(masked[match[2]:match[3]])
		ctes[name] = struct{}{}
		if shadowsStore(name) {
			fail("a CTE named " + name + " shadows the relation of the same name, so reads of it cannot be attributed")
		}
		// A non-recursive CTE that reads its own name reads the real relation of
		// that name, whatever the later references mean.
		if !recursiveWith.MatchString(masked) && cteReadsItself(masked, match[1]-1, name) {
			delete(ctes, name)
		}
	}
	refs, problems := relationsOf(masked)
	for _, problem := range problems {
		if !scope.census {
			fail(problem)
		}
	}
	for _, ref := range refs {
		attribute(ref, ctes, scope, kinds, evidence, used)
	}
}

var recursiveWith = regexp.MustCompile(`(?i)\bwith\s+recursive\b`)

// cteReadsItself reports whether the CTE body that opens at open names its own
// CTE name in a FROM or JOIN item.
func cteReadsItself(masked string, open int, name string) bool {
	if open < 0 || open >= len(masked) || masked[open] != '(' {
		return false
	}
	closeAt := closeOf(masked, open)
	if closeAt < 0 {
		return false
	}
	refs, _ := relationsOf(masked[open+1 : closeAt])
	for _, ref := range refs {
		if ref.name == name {
			return true
		}
	}
	return false
}

// shadowsStore reports a CTE name that a reader could take for a store.
func shadowsStore(name string) bool {
	if name == factRecordsTable || isSystemCatalog(name) || strings.HasPrefix(name, "content_") ||
		strings.HasPrefix(name, "eshu_search_") {
		return true
	}
	if _, ok := relationAliases[name]; ok {
		return true
	}
	_, ok := structuralTables[name]
	return ok
}

func attribute(ref relationRef, ctes map[string]struct{}, scope censusScope, kinds []string, evidence string, used *Used) {
	name := strings.TrimPrefix(ref.name, "public.")
	if token, ok := relationAliases[name]; ok {
		add(used, token, evidence)
		return
	}
	if _, ok := ctes[name]; ok {
		return
	}
	if _, ok := scope.ctes[name]; ok {
		return
	}
	if _, ok := sqlWords[name]; ok || ref.isFunction {
		return
	}
	if _, ok := structuralTables[name]; ok || isSystemCatalog(name) {
		return
	}
	if name == factRecordsTable {
		if len(kinds) == 0 {
			used.Unresolved = append(used.Unresolved, "fact_records read with no resolvable fact_kind: "+evidence)
		}
		for _, kind := range kinds {
			add(used, factRecordsToken(kind), evidence+" [fact_kind "+kind+"]")
		}
		return
	}
	if scope.known != nil {
		if _, ok := scope.known[name]; !ok {
			return
		}
	}
	if strings.HasPrefix(name, "content_") || strings.HasPrefix(name, "eshu_search_") {
		add(used, StoreContentSearch, evidence)
		return
	}
	add(used, ReadTablePrefix+name, evidence)
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
// IN lists (literals and binds), and bind placeholders resolved against args.
// A bind whose argument is missing or not a string, a string slice, or a
// StringArray is a problem, unless the text is a census fragment whose binds a
// fallback resolves.
func factKinds(text string, args []any, census bool) (kinds, problems []string) {
	seen := map[string]struct{}{}
	push := func(kind string) {
		if kind == "" {
			return
		}
		if _, ok := seen[kind]; !ok {
			seen[kind] = struct{}{}
			kinds = append(kinds, kind)
		}
	}
	resolve := func(raw string) {
		index, err := strconv.Atoi(raw)
		if err == nil && index >= 1 && index <= len(args) {
			if values, ok := stringsOf(args[index-1]); ok {
				for _, kind := range values {
					push(kind)
				}
				return
			}
		}
		if !census {
			problems = append(problems, "fact_kind bind $"+raw+" has no string argument the classifier can read")
		}
	}
	for _, match := range kindEquals.FindAllStringSubmatch(text, -1) {
		push(match[1])
	}
	for _, match := range kindIn.FindAllStringSubmatch(text, -1) {
		for _, item := range quoted.FindAllStringSubmatch(match[1], -1) {
			push(item[1])
		}
		for _, item := range bindItem.FindAllStringSubmatch(match[1], -1) {
			resolve(item[1])
		}
	}
	for _, match := range kindBind.FindAllStringSubmatch(text, -1) {
		resolve(match[1])
	}
	sort.Strings(kinds)
	return kinds, problems
}

func stringsOf(value any) ([]string, bool) {
	switch typed := value.(type) {
	case string:
		return []string{typed}, true
	case []string:
		return typed, true
	case array.StringArray:
		return []string(typed), true
	case *array.StringArray:
		if typed == nil {
			return nil, false
		}
		return []string(*typed), true
	default:
		return nil, false
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
