// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"regexp"
	"testing"
)

// cypherParamRef matches a top-level $param reference in a statement.
var cypherParamRef = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// TestBuildCanonicalCodeCallUpsertSendsEveryReferencedParam is the #6782
// audit guard for the single-row canonical code-edge builder. Neo4j rejects a
// statement whose $param is missing, so every parameter a template reads must
// be present, nil when the edge has no value for it.
func TestBuildCanonicalCodeCallUpsertSendsEveryReferencedParam(t *testing.T) {
	t.Parallel()

	cases := map[string]CanonicalCodeCallParams{
		"calls_without_call_kind": {CallerEntityID: "entity:function:a", CalleeEntityID: "entity:function:b"},
		"calls_with_call_kind":    {CallerEntityID: "entity:function:a", CalleeEntityID: "entity:function:b", CallKind: "function_call"},
		"jsx_component":           {CallerEntityID: "entity:function:a", CalleeEntityID: "entity:function:b", CallKind: "jsx_component"},
		"metaclass":               {CallerEntityID: "entity:class:a", CalleeEntityID: "entity:class:b", RelationshipType: "USES_METACLASS"},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stmt := BuildCanonicalCodeCallUpsert(params, "parser/code-calls")
			for _, match := range cypherParamRef.FindAllStringSubmatch(stmt.Cypher, -1) {
				if _, ok := stmt.Parameters[match[1]]; !ok {
					t.Fatalf("statement reads $%s but Parameters has no such key:\n%s", match[1], stmt.Cypher)
				}
			}
		})
	}
}

// TestBuildCanonicalCodeCallUpsertAbsentCallKindIsNil keeps an absent call
// kind an explicit nil, which leaves rel.call_kind unset on both backends.
func TestBuildCanonicalCodeCallUpsertAbsentCallKindIsNil(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalCodeCallUpsert(CanonicalCodeCallParams{
		CallerEntityID: "entity:function:a",
		CalleeEntityID: "entity:function:b",
	}, "parser/code-calls")
	value, ok := stmt.Parameters["call_kind"]
	if !ok || value != nil {
		t.Fatalf("call_kind = %#v (present=%t), want explicit nil", value, ok)
	}
}
