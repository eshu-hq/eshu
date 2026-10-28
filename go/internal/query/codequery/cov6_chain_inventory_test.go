// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

func TestCov6SkipCypherTrivia(t *testing.T) {
	t.Parallel()
	if got := skipCypherLineComment("// hello\nMATCH", 0); got != 8 {
		t.Fatalf("line comment end = %d, want 8", got)
	}
	if got := skipCypherLineComment("// noeol", 0); got != len("// noeol") {
		t.Fatalf("unterminated line comment end = %d, want %d", got, len("// noeol"))
	}
	if got := skipCypherBlockComment("/* hi */MATCH", 0); got != 8 {
		t.Fatalf("block comment end = %d, want 8", got)
	}
	if got := skipCypherBlockComment("/* unterminated", 0); got != len("/* unterminated") {
		t.Fatalf("unterminated block end = %d, want len", got)
	}
	// Quoted sections skip escaped quotes and doubled single-quotes.
	if got := skipCypherQuoted(`'it\'s' + x`, 0); got != 7 {
		t.Fatalf("escaped quote end = %d, want 7", got)
	}
	if got := skipCypherQuoted(`'it''s'`, 0); got != 7 {
		t.Fatalf("doubled quote end = %d, want 7", got)
	}
	if got := skipCypherQuoted("`tick` + y", 0); got != 6 {
		t.Fatalf("backtick end = %d, want 6", got)
	}
	if got := skipCypherQuoted(`'open`, 0); got != len(`'open`) {
		t.Fatalf("unterminated quote end = %d, want len", got)
	}
	if got := skipCypherTrivia("  // c\n/* d */MATCH", 0); got != len("  // c\n/* d */") {
		t.Fatalf("trivia skip = %d, want MATCH start", got)
	}
	if !startsCypherLineComment("//x", 0) || startsCypherLineComment("/x", 0) {
		t.Fatal("line comment start gate mismatch")
	}
	if !startsCypherBlockComment("/*x", 0) || startsCypherBlockComment("/x", 0) {
		t.Fatal("block comment start gate mismatch")
	}
}

func TestCov6NormalizeCallChainNodes(t *testing.T) {
	t.Parallel()
	maps := normalizeCallChainNodes([]map[string]any{{"id": "e1"}})
	if len(maps) != 1 {
		t.Fatalf("map slice = %d nodes, want 1", len(maps))
	}
	anys := normalizeCallChainNodes([]any{map[string]any{"id": "e2"}, "raw"})
	if len(anys) != 2 || anys[1] != "raw" {
		t.Fatalf("any slice = %v, want passthrough of non-maps", anys)
	}
	nodes := normalizeCallChainNodes([]dbtype.Node{{
		Labels: []string{"Function"},
		Props:  map[string]any{"id": "e3", "name": "run", "language": "go"},
	}})
	if len(nodes) != 1 {
		t.Fatalf("driver nodes = %d, want 1", len(nodes))
	}
	shape, ok := nodes[0].(map[string]any)
	if !ok || shape["id"] != "e3" || shape["name"] != "run" {
		t.Fatalf("driver node shape = %v, want id/name map", nodes[0])
	}
	if normalizeCallChainNodes("nope") != nil {
		t.Fatal("foreign input must normalize to nil")
	}
	if got := normalizeCallChainNode(map[string]any{"id": "e1"}); got.(map[string]any)["id"] != "e1" {
		t.Fatalf("map node = %v, want cloned map", got)
	}
	if got := graphNodeSemanticID(map[string]any{"id": " e1 "}); got != "e1" {
		t.Fatalf("id prop = %q, want trimmed e1", got)
	}
	if got := graphNodeSemanticID(map[string]any{"uid": "u9"}); got != "u9" {
		t.Fatalf("uid fallback = %q, want u9", got)
	}
	if got := graphNodeSemanticID(map[string]any{"name": "x"}); got != "" {
		t.Fatalf("nameless props = %q, want empty", got)
	}
	if got := graphNodeSemanticID(nil); got != "" {
		t.Fatalf("nil props = %q, want empty", got)
	}
}

func TestCov6SplitRouteToCallerRelationships(t *testing.T) {
	t.Parallel()
	row := func(direction, id string) map[string]any {
		return map[string]any{
			"direction": direction, "entity_id": id, "name": "run",
			"file_path": "a.go", "repo_id": "r", "language": "go",
			"start_line": 1, "end_line": 5, "depth": 1,
		}
	}
	callers, callees, truncated := splitRouteToCallerRelationships([]map[string]any{
		row("incoming", "e1"), row("outgoing", "e2"),
	}, 10)
	if len(callers) != 1 || len(callees) != 1 || truncated {
		t.Fatalf("split = (%d,%d,%v), want (1,1,false)", len(callers), len(callees), truncated)
	}
	if callers[0]["entity_id"] != "e1" {
		t.Fatalf("caller = %v, want e1", callers[0])
	}
	_, _, truncated = splitRouteToCallerRelationships([]map[string]any{
		row("incoming", "e1"), row("incoming", "e2"),
	}, 1)
	if !truncated {
		t.Fatal("over-limit split must report truncation")
	}
}

func TestCov6NormalizeHardcodedSecretKinds(t *testing.T) {
	t.Parallel()
	if got := normalizeHardcodedSecretLimit(0); got != hardcodedSecretDefaultLimit {
		t.Fatalf("zero limit = %d, want default", got)
	}
	if got := normalizeHardcodedSecretLimit(1 << 30); got != hardcodedSecretMaxLimit {
		t.Fatalf("huge limit = %d, want max", got)
	}
	if got := normalizeHardcodedSecretLimit(7); got != 7 {
		t.Fatalf("plain limit = %d, want 7", got)
	}
	kinds, err := normalizeHardcodedSecretKinds([]string{" " + hardcodedSecretAllowedKinds[0] + " ", hardcodedSecretAllowedKinds[0]})
	if err != nil || len(kinds) != 1 {
		t.Fatalf("kinds = (%v,%v), want single deduped kind", kinds, err)
	}
	if _, err := normalizeHardcodedSecretKinds([]string{"bogus-kind"}); err == nil {
		t.Fatal("bogus kind must be rejected")
	}
	if kinds, err := normalizeHardcodedSecretKinds(nil); err != nil || len(kinds) != 0 {
		t.Fatalf("empty kinds = (%v,%v), want empty", kinds, err)
	}
}

func TestCov6StructuralInventoryRequest(t *testing.T) {
	t.Parallel()
	if got := (StructuralInventoryRequest{}).NormalizedLimit(); got != StructuralInventoryDefaultLimit {
		t.Fatalf("default limit = %d, want %d", got, StructuralInventoryDefaultLimit)
	}
	if got := (StructuralInventoryRequest{Limit: 1 << 30}).NormalizedLimit(); got != structuralInventoryMaxLimit {
		t.Fatalf("huge limit = %d, want max", got)
	}
	if got := (StructuralInventoryRequest{Limit: 5}).NormalizedLimit(); got != 5 {
		t.Fatalf("plain limit = %d, want 5", got)
	}
	if got := (StructuralInventoryRequest{}).Kind(); got != "entity" {
		t.Fatalf("default kind = %q, want entity", got)
	}
	if got := (StructuralInventoryRequest{InventoryKind: " DaTaClAsS "}).EntityType(); got != "Class" {
		t.Fatalf("dataclass entity type = %q, want Class", got)
	}
	if got := (StructuralInventoryRequest{InventoryKind: "function_count_by_file"}).EntityType(); got != "Function" {
		t.Fatalf("count kind entity type = %q, want Function", got)
	}
	if names := structuralInventoryKindNames(); len(names) != 9 {
		t.Fatalf("kind names = %v, want 9 families", names)
	}
	if got := nextStructuralInventoryOffset(10, 5, true); got != 15 {
		t.Fatalf("truncated offset = %v, want 15", got)
	}
	if got := nextStructuralInventoryOffset(10, 5, false); got != nil {
		t.Fatalf("complete offset = %v, want nil", got)
	}
}

func TestCov6SymbolSearchRequest(t *testing.T) {
	t.Parallel()
	req := SymbolSearchRequest{Query: " run "}
	if got := req.ResolvedSymbol(); got != "run" {
		t.Fatalf("resolved symbol = %q, want trimmed query", got)
	}
	req.Symbol = " exact "
	if got := req.ResolvedSymbol(); got != "exact" {
		t.Fatalf("resolved symbol = %q, want trimmed symbol field", got)
	}
	if got := (SymbolSearchRequest{}).normalizedLimit(); got != symbolSearchDefaultLimit {
		t.Fatalf("default symbol limit = %d, want %d", got, symbolSearchDefaultLimit)
	}
	if mode, err := (SymbolSearchRequest{MatchMode: "bogus"}).normalizedMatchMode(); err == nil || mode != "" {
		t.Fatalf("bogus match mode = (%q,%v), want rejection", mode, err)
	}
	if got := (SymbolSearchRequest{MatchMode: "bogus"}).MustMatchMode(); got != "exact" {
		t.Fatalf("bogus must-match = %q, want exact fallback", got)
	}
	types := SymbolSearchRequest{EntityType: "function", EntityTypes: []string{"Function", "class"}}.NormalizedEntityTypes()
	if len(types) != 2 {
		t.Fatalf("entity types = %v, want 2 deduplicated", types)
	}
	if got := firstEntityType(SymbolSearchRequest{}); got != "" {
		t.Fatalf("empty first type = %q, want empty", got)
	}
	rows := normalizeSymbolGraphResults([]map[string]any{
		{"entity_name": "run", "repo_id": "r", "file_path": "a.go", "start_line": 1, "end_line": 5},
	}, "exact")
	if len(rows) != 1 || rows[0]["name"] != "run" || rows[0]["entity_name"] != "run" {
		t.Fatalf("graph rows = %v, want mirrored names", rows)
	}
	if rows[0]["classification"] != "definition" || rows[0]["match_kind"] != "exact" || rows[0]["rank"] != 1 {
		t.Fatalf("graph row = %v, want definition/exact/rank 1", rows[0])
	}
}

func TestCov6TopicTrigramAndTerms(t *testing.T) {
	t.Parallel()
	if !CodeTopicTermHasTrigram("parse") {
		t.Fatal("parse must carry a trigram run")
	}
	if CodeTopicTermHasTrigram("a-b") {
		t.Fatal("short runs must not qualify")
	}
	if CodeTopicTermHasTrigram("ab 12") {
		t.Fatal("split runs must not qualify")
	}
	if got := firstCodeTopicTerm(nil); got != "" {
		t.Fatalf("empty terms = %q, want empty", got)
	}
	if got := firstCodeTopicTerm([]string{"plan", "probe"}); got != "plan" {
		t.Fatalf("first term = %q, want plan", got)
	}
}

func TestCov6NornicDBAnchorLookupCypher(t *testing.T) {
	t.Parallel()
	scoped := NornicDBRelationshipStoryAnchorLookupCypher("Function", "id", true)
	unscoped := NornicDBRelationshipStoryAnchorLookupCypher("Function", "id", false)
	if scoped == "" || unscoped == "" {
		t.Fatal("anchor lookup cypher must not be empty")
	}
	if !strings.Contains(scoped, "Function") || !strings.Contains(unscoped, "Function") {
		t.Fatal("anchor lookup must name the entity label")
	}
	if scoped == unscoped {
		t.Fatal("scoped and unscoped anchor lookups must differ")
	}
}

func TestCov6NormalizeCodeFlowLanguage(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"C#": "csharp", "js": "javascript", "ts": "typescript", "Go": "go",
	} {
		if got := normalizeCodeFlowLanguage(raw); got != want {
			t.Fatalf("normalizeCodeFlowLanguage(%q) = %q, want %q", raw, got, want)
		}
	}
}
