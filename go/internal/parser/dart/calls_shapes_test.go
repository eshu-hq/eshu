// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// assertBucketRows asserts that payload[bucket] has exactly want rows,
// returning the slice for further per-row assertions. calls_oracle_test.go
// proves the merged dartSyntaxIndex.collect traversal produces call sites
// matching a frozen full-tree oracle, but that oracle shares the same node-kind
// dispatch logic (dartCallChain.observe), so a semantic bug present in both
// would still pass. These tests instead pin the concrete
// name/full_name/line_number/row-count that dart.md and calls.go's comments
// claim dartCallChain.observe supports, so a future regression in the dispatch
// itself (not just the traversal mechanism) is caught.
func assertBucketRows(t *testing.T, payload map[string]any, bucket string, want int) []map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		if want == 0 {
			return nil
		}
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	if len(items) != want {
		t.Fatalf("payload[%q] row count = %d, want %d in %#v", bucket, len(items), want, items)
	}
	return items
}

// assertCallRow asserts that items contains exactly one row matching name,
// fullName, and line.
func assertCallRow(t *testing.T, items []map[string]any, name string, fullName string, line int) {
	t.Helper()

	for _, item := range items {
		if item["name"] != name || item["full_name"] != fullName {
			continue
		}
		if got, want := item["line_number"], line; got != want {
			t.Fatalf("call %s/%s line_number = %#v, want %#v", name, fullName, got, want)
		}
		return
	}
	t.Fatalf("missing call name=%q full_name=%q in %#v", name, fullName, items)
}

// TestParseNamedConstructorCall pins the named-constructor call shape
// (`Point.origin()`): the call site emits one row keyed off the constructor
// name, and the constructor's own declaration (`Point.origin() : x = 0;`,
// handled by a disjoint `constructor_signature` grammar node, never an
// `arguments`/`argument_part` node — see dartCallChain's doc comment)
// produces no row.
func TestParseNamedConstructorCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "named_ctor.dart", `class Point {
  final int x;
  Point.origin() : x = 0;
}
void useIt() {
  Point.origin();
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	items := assertBucketRows(t, payload, "function_calls", 1)
	assertCallRow(t, items, "origin", "Point.origin", 6)
}

// TestParseGenericArgsCall pins the generic-type-argument call shape
// (`identity<int>(5)`): the `type_arguments` selector between the callee and
// the `argument_part` selector must not break the chain or drop the call.
func TestParseGenericArgsCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "generic_args.dart", `T identity<T>(T value) => value;
void useIt() {
  identity<int>(5);
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	items := assertBucketRows(t, payload, "function_calls", 1)
	assertCallRow(t, items, "identity", "identity", 3)
}

// TestParseChainedCallEmitsEachLinkAndInnerClosureCall pins the chained-call
// shape (`fetch().then((v) => process(v))`): actual dartCallChain.observe
// behavior emits THREE separate rows, one per real invocation — `fetch()`
// itself (the qualifier call before the chain continues), `.then(...)` (the
// chained call; the receiver qualifier is lost at the `fetch()` call
// boundary per extendOrStart's doc comment, so `.then`'s full_name is just
// "then", not "fetch.then"), and `process(v)` (a nested call inside the
// closure body, attributed to the enclosing scope since closures do not
// start their own dartCallSite name). This is a characterization test of
// verified-correct current behavior, not a claim that all three rows were
// anticipated by the shape's caller.
func TestParseChainedCallEmitsEachLinkAndInnerClosureCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "chained_then.dart", `Future<int> fetch() async => 1;
int process(int v) => v;
void useIt() {
  fetch().then((v) => process(v));
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	items := assertBucketRows(t, payload, "function_calls", 3)
	assertCallRow(t, items, "fetch", "fetch", 4)
	assertCallRow(t, items, "then", "then", 4)
	assertCallRow(t, items, "process", "process", 4)
}

// TestParseSuperMethodCall pins the `super.method()` call shape: the
// `unconditional_assignable_selector` direct-sibling shape used by `super.m()`
// (as opposed to the `selector`-wrapped shape used by `o.m()`, see calls.go's
// switch comment) must still resolve to a `super.greet` full_name.
func TestParseSuperMethodCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "super_call.dart", `class Base {
  void greet() {}
}
class Sub extends Base {
  @override
  void greet() {
    super.greet();
  }
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	items := assertBucketRows(t, payload, "function_calls", 1)
	assertCallRow(t, items, "greet", "super.greet", 7)
}

// TestParseCascadeRepeatCallCollapsesToOneRow pins the documented cascade
// limitation (see appendUniqueDartCall's doc comment): repeat cascade calls
// to the same method (`b..write("a")..write("b")`) dedup by full_name into
// exactly ONE function_calls row, keyed to the first cascade call's line.
// This is the documented row-volume-collapse behavior, not a bug — this test
// pins it as a gate so a change that silently starts double-counting (or
// silently drops) cascade-repeat calls is caught.
func TestParseCascadeRepeatCallCollapsesToOneRow(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "cascade.dart", `class Buf {
  void write(String s) {}
}
void useIt(Buf b) {
  b..write("a")..write("b");
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	items := assertBucketRows(t, payload, "function_calls", 1)
	assertCallRow(t, items, "write", "write", 5)
}
