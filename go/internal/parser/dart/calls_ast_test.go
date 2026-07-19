// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// assertBucketEmpty asserts that a payload bucket has zero rows. AppendBucket
// only sets the map key when a row is appended, so an empty bucket may be a
// nil/absent key rather than an empty slice.
func assertBucketEmpty(t *testing.T, payload map[string]any, bucket string) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		return
	}
	if len(items) != 0 {
		t.Fatalf("payload[%q] = %#v, want empty", bucket, items)
	}
}

// TestParseDeclarationOnlyFileHasNoCallRows is the regression test for
// eshu-hq/eshu#5332: the legacy raw byte-scanner recorded every
// function/method/constructor declaration as a call to itself (heuristic
// "identifier immediately followed by `(`"), materializing a spurious
// self-loop CALLS edge for every declaration in the corpus. A file containing
// only declarations and zero call sites must produce zero function_calls
// rows.
func TestParseDeclarationOnlyFileHasNoCallRows(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "declarations_only.dart", `class Widget {
  Widget();
  Widget.named();
  int get value => 0;
  set value(int v) {}
  void run() {}
}
void topFn() {}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketEmpty(t, payload, "function_calls")
}

// TestParseKeepsArrowRecursionSelfCall proves the fix does not overcorrect
// into a self-loop guard: a genuine recursive call (caller == callee) on the
// same line as its own declaration must survive.
func TestParseKeepsArrowRecursionSelfCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "fib.dart", `int fib(int n) => n < 2 ? n : fib(n - 1) + fib(n - 2);
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "function_calls", "fib")
}

// TestParseKeepsBlockFormRecursionSelfCall covers the block-body recursion
// shape (call site on a different line than the declaration).
func TestParseKeepsBlockFormRecursionSelfCall(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "fact.dart", `int fact(int n) {
  if (n <= 1) {
    return 1;
  }
  return n * fact(n - 1);
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	call := assertBucketName(t, payload, "function_calls", "fact")
	if got, want := call["line_number"], 5; got != want {
		t.Fatalf("function_calls[fact][line_number] = %#v, want %#v", got, want)
	}
}

// TestParseCapturesMutualRecursionBothDirections proves both directed edges
// of a mutual recursion pair survive as distinct rows.
func TestParseCapturesMutualRecursionBothDirections(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "mutual.dart", `void a() {
  b();
}
void b() {
  a();
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "function_calls", "a")
	assertBucketName(t, payload, "function_calls", "b")
}

// TestParseDistinguishesCallSiteFromDeclarationLine is the direct regression
// case for the bug: a method is declared on one line and called from a
// different line elsewhere. Only the call site line must appear in
// function_calls; the declaration line must not.
func TestParseDistinguishesCallSiteFromDeclarationLine(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "widget.dart", `class Foo {
  void build() {}
}

void render(Foo f) {
  f.build();
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketNameCount(t, payload, "function_calls", "build", 1)
	call := assertBucketName(t, payload, "function_calls", "build")
	if got, want := call["line_number"], 6; got != want {
		t.Fatalf("function_calls[build][line_number] = %#v, want %#v (call site, not declaration line 2)", got, want)
	}
	if got, want := call["full_name"], "f.build"; got != want {
		t.Fatalf("function_calls[build][full_name] = %#v, want %#v", got, want)
	}
}
