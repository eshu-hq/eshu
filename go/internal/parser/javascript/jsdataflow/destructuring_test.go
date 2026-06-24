// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jsdataflow

import "testing"

// TestLowerObjectDestructuringDeclarationBindsFieldAccess proves an object
// destructuring declaration defines the bound local and reads the named source
// field, not only the whole object.
func TestLowerObjectDestructuringDeclarationBindsFieldAccess(t *testing.T) {
	t.Parallel()

	src := "function f(p, clean) {\n" +
		"\tpayload.id = p;\n" +
		"\tpayload.other = clean;\n" +
		"\tconst { id } = payload;\n" +
		"\tuse(id);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "payload.id:2->4") {
		t.Fatalf("destructuring did not read payload.id; got %v", got)
	}
	if !contains(got, "id:4->5") {
		t.Fatalf("destructuring did not define id; got %v", got)
	}
	if contains(got, "payload.other:3->4") {
		t.Fatalf("destructuring of id read unrelated payload.other field; got %v", got)
	}
}

// TestLowerObjectDestructuringAliasBindsValueOnly proves a renamed object
// pattern binds the target identifier and does not define the property key as a
// local variable.
func TestLowerObjectDestructuringAliasBindsValueOnly(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tpayload.id = p;\n" +
		"\tconst { id: userID } = payload;\n" +
		"\tuse(userID);\n" +
		"\tuse(id);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "payload.id:2->3") {
		t.Fatalf("renamed destructuring did not read payload.id; got %v", got)
	}
	if !contains(got, "userID:3->4") {
		t.Fatalf("renamed destructuring did not define userID; got %v", got)
	}
	if contains(got, "id:3->5") {
		t.Fatalf("renamed destructuring wrongly defined property key id; got %v", got)
	}
}

// TestLowerArrayDestructuringDeclarationBindsElement proves an array pattern
// defines the bound local and reads the source container element approximation.
func TestLowerArrayDestructuringDeclarationBindsElement(t *testing.T) {
	t.Parallel()

	src := "function f(p, k) {\n" +
		"\tvalues[k] = p;\n" +
		"\tconst [first] = values;\n" +
		"\tuse(first);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "values[*]:2->3") {
		t.Fatalf("array destructuring did not read values[*]; got %v", got)
	}
	if !contains(got, "first:3->4") {
		t.Fatalf("array destructuring did not define first; got %v", got)
	}
}

// TestLowerDestructuredParametersDefineBoundIdentifiers proves destructured
// parameters are entry definitions for the bound identifiers.
func TestLowerDestructuredParametersDefineBoundIdentifiers(t *testing.T) {
	t.Parallel()

	src := "function f({ id: userID }, [first]) {\n" +
		"\tuse(userID);\n" +
		"\tuse(first);\n" +
		"\tuse(id);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "userID:1->2") {
		t.Fatalf("destructured object param did not define userID; got %v", got)
	}
	if !contains(got, "first:1->3") {
		t.Fatalf("destructured array param did not define first; got %v", got)
	}
	if contains(got, "id:1->4") {
		t.Fatalf("destructured object param wrongly defined property key id; got %v", got)
	}
}

// TestLowerForOfRenamedDestructuringBindsValueOnly proves a for-of object
// pattern binds only the renamed target while preserving the loop-body edge.
func TestLowerForOfRenamedDestructuringBindsValueOnly(t *testing.T) {
	t.Parallel()

	src := "function f(rows) {\n" +
		"\tfor (const { id: userID } of rows) {\n" +
		"\t\tuse(userID);\n" +
		"\t\tuse(id);\n" +
		"\t}\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "userID:2->3") {
		t.Fatalf("for-of destructuring did not define userID; got %v", got)
	}
	if contains(got, "id:2->4") {
		t.Fatalf("for-of destructuring wrongly defined property key id; got %v", got)
	}
}

// TestLowerForOfDestructuringReadsIterableField proves a for-of object pattern
// reads the field of each iterable element, preserving prior container-element
// writes without falling back to the whole iterable only.
func TestLowerForOfDestructuringReadsIterableField(t *testing.T) {
	t.Parallel()

	src := "function f(p, k, rows) {\n" +
		"\trows[k].id = p;\n" +
		"\tfor (const { id } of rows) {\n" +
		"\t\tuse(id);\n" +
		"\t}\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "rows[*].id:2->3") {
		t.Fatalf("for-of destructuring did not read rows[*].id; got %v", got)
	}
	if !contains(got, "id:3->4") {
		t.Fatalf("for-of destructuring did not define id for the body; got %v", got)
	}
}

// TestLowerForInDestructuringDoesNotReadIterableField proves a for-in object
// pattern binds keys, not iterable elements, so it must not invent field flow
// from the iterated object values.
func TestLowerForInDestructuringDoesNotReadIterableField(t *testing.T) {
	t.Parallel()

	src := "function f(p, k, rows) {\n" +
		"\trows[k].id = p;\n" +
		"\tfor (const { id } in rows) {\n" +
		"\t\tuse(id);\n" +
		"\t}\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "rows[*].id:2->3") {
		t.Fatalf("for-in destructuring read iterable element field; got %v", got)
	}
	if !contains(got, "id:3->4") {
		t.Fatalf("for-in destructuring did not define id for the body; got %v", got)
	}
}

// TestLowerClosureDestructuredParamShadowsOuter proves a destructured callback
// parameter is local to the invoked closure and does not capture an outer
// same-named binding.
func TestLowerClosureDestructuredParamShadowsOuter(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet id = p;\n" +
		"\tdoThing(({ id }) => sink(id));\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "id:2->3") {
		t.Fatalf("destructured callback param wrongly captured outer id; got %v", got)
	}
}

// TestLowerClosureDestructuredLocalShadowsOuter proves a destructured local
// declaration inside an invoked closure shadows an outer same-named binding.
func TestLowerClosureDestructuredLocalShadowsOuter(t *testing.T) {
	t.Parallel()

	src := "function f(p, obj) {\n" +
		"\tlet id = p;\n" +
		"\tdoThing(() => { const { id } = obj; sink(id); });\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "id:2->3") {
		t.Fatalf("destructured closure local wrongly captured outer id; got %v", got)
	}
}
