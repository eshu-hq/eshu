// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestParseBoundsOversizedDollarQuotedFunctionBody proves the fix for #4422: a
// single CREATE FUNCTION statement whose dollar-quoted plpgsql body exceeds
// maxSQLSegmentBytes must not be handed whole to tree-sitter. Before the fix,
// tree-sitter parses an opaque ~1MB plpgsql body superlinearly (measured
// >90s) and can hard-crash via a tree-sitter error-recovery assertion
// (SIGABRT in stack_node_retain). The bounded segment must parse in well
// under the old outlier time, must not crash, must still extract the
// function's signature entity, and must record the elision in
// payload["sql_parse_bounded"].
func TestParseBoundsOversizedDollarQuotedFunctionBody(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString("CREATE FUNCTION public.big_migration() RETURNS void AS $$\n")
	for body.Len() < 1024*1024 {
		body.WriteString("  UPDATE users SET x = x + 1 WHERE id = 5;\n")
	}
	body.WriteString("$$ LANGUAGE plpgsql;\n")

	path := writeSQLTestFile(t, "big_function.sql", body.String())

	done := make(chan map[string]any, 1)
	go func() {
		got, err := Parse(path, false, Options{}, newSQLTestParser(t))
		if err != nil {
			t.Errorf("Parse() error = %v, want nil", err)
			done <- nil
			return
		}
		done <- got
	}()

	start := time.Now()
	var got map[string]any
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Parse() did not return within 2s for a %d-byte dollar-quoted body (bound not enforced)", body.Len())
	}
	elapsed := time.Since(start)
	if elapsed >= 2*time.Second {
		t.Fatalf("Parse() took %v, want < 2s", elapsed)
	}
	if got == nil {
		t.Fatalf("Parse() returned nil payload")
	}

	assertSQLBucketContainsName(t, got, "sql_functions", "public.big_migration")

	bounded, _ := got["sql_parse_bounded"].([]map[string]any)
	if len(bounded) == 0 {
		t.Fatalf("sql_parse_bounded = empty, want at least one bounded-segment entry in %#v", got)
	}
	found := false
	for _, entry := range bounded {
		if action, _ := entry["action"].(string); action == "body_elided" {
			found = true
			if entry["path"] != path {
				t.Fatalf("sql_parse_bounded entry path = %v, want %q", entry["path"], path)
			}
			bytesVal, ok := entry["original_bytes"].(int)
			if !ok || bytesVal <= maxSQLSegmentBytes {
				t.Fatalf("sql_parse_bounded entry original_bytes = %v, want > %d", entry["original_bytes"], maxSQLSegmentBytes)
			}
		}
	}
	if !found {
		t.Fatalf("sql_parse_bounded missing an action=body_elided entry, got %#v", bounded)
	}
}

// TestParseSmallDollarQuotedFunctionIsUnaffected guards that a normal,
// under-cap dollar-quoted function body parses exactly as before the bound
// was introduced: the routine signature entity and its body table mention
// are still extracted, the indexed source of the function is byte-identical
// to the original (un-elided) dollar-quoted text, and no sql_parse_bounded
// entry is recorded.
func TestParseSmallDollarQuotedFunctionIsUnaffected(t *testing.T) {
	t.Parallel()

	const source = `CREATE OR REPLACE FUNCTION public.count_users() RETURNS integer
LANGUAGE sql
AS $$
  SELECT count(*) FROM public.users;
$$;
`
	path := writeSQLTestFile(t, "small_function.sql", source)

	got, err := Parse(path, false, Options{IndexSource: true}, newSQLTestParser(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertSQLBucketContainsName(t, got, "sql_functions", "public.count_users")
	assertSQLRelationshipExists(t, got, "READS_FROM", "public.count_users", "public.users")

	// Byte-identical body extraction: the indexed source for the function
	// entity must contain the untouched dollar-quoted body text verbatim,
	// proving the bound path did not elide, truncate, or otherwise alter an
	// under-cap segment before extraction.
	wantBody := "SELECT count(*) FROM public.users;"
	gotSource := sqlBucketSource(got, "sql_functions", "public.count_users")
	if !strings.Contains(gotSource, wantBody) {
		t.Fatalf("sql_functions[public.count_users].source = %q, want it to contain byte-identical body %q", gotSource, wantBody)
	}

	bounded, _ := got["sql_parse_bounded"].([]map[string]any)
	if len(bounded) != 0 {
		t.Fatalf("sql_parse_bounded = %#v, want empty for an under-cap segment", bounded)
	}
}

// TestSegmentBoundThresholdElidesOnlyOverCapBodies proves the exact
// maxSQLSegmentBytes boundary: a segment just over the cap triggers dollar-quote
// body elision, and a segment just under the cap does not.
func TestSegmentBoundThresholdElidesOnlyOverCapBodies(t *testing.T) {
	t.Parallel()

	buildSegment := func(bodyBytes int) string {
		var b strings.Builder
		b.WriteString("CREATE FUNCTION public.threshold_fn() RETURNS void AS $$\n")
		for b.Len() < bodyBytes {
			b.WriteString("x")
		}
		b.WriteString("\n$$ LANGUAGE plpgsql;\n")
		return b.String()
	}

	t.Run("just_under_cap", func(t *testing.T) {
		t.Parallel()

		text := buildSegment(maxSQLSegmentBytes - 200)
		if len(text) >= maxSQLSegmentBytes {
			t.Fatalf("test setup: segment length %d, want < %d", len(text), maxSQLSegmentBytes)
		}
		bounded, edited := elideOversizedDollarQuotedBodies(text)
		if edited {
			t.Fatalf("elideOversizedDollarQuotedBodies() edited = true for a %d-byte segment under the %d-byte cap", len(text), maxSQLSegmentBytes)
		}
		if bounded != text {
			t.Fatalf("elideOversizedDollarQuotedBodies() changed an under-cap segment")
		}
	})

	t.Run("just_over_cap", func(t *testing.T) {
		t.Parallel()

		text := buildSegment(maxSQLSegmentBytes + 200)
		if len(text) <= maxSQLSegmentBytes {
			t.Fatalf("test setup: segment length %d, want > %d", len(text), maxSQLSegmentBytes)
		}
		bounded, edited := elideOversizedDollarQuotedBodies(text)
		if !edited {
			t.Fatalf("elideOversizedDollarQuotedBodies() edited = false for a %d-byte segment over the %d-byte cap", len(text), maxSQLSegmentBytes)
		}
		if len(bounded) >= len(text) {
			t.Fatalf("elideOversizedDollarQuotedBodies() bounded length %d, want < original %d", len(bounded), len(text))
		}
		if !strings.Contains(bounded, "$$") {
			t.Fatalf("elideOversizedDollarQuotedBodies() must keep dollar-quote delimiters, got %q", firstN(bounded, 120))
		}
	})
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestParseDoesNotCrashOnPathologicalNonDollarQuotedSegment guards the
// defense-in-depth path: a single oversized segment with no dollar-quoted
// span (so elision cannot shrink it) still returns without panicking or
// hanging, by skipping the tree-sitter AST parse for that segment.
func TestParseDoesNotCrashOnPathologicalNonDollarQuotedSegment(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("CREATE TABLE public.wide (\n")
	i := 0
	for b.Len() < maxSQLSegmentBytes+1024 {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, "  col_%d VARCHAR(255) DEFAULT 'x'", i)
		i++
	}
	b.WriteString("\n);\n")

	path := writeSQLTestFile(t, "pathological_wide.sql", b.String())

	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{} }()
		if _, err := Parse(path, false, Options{}, newSQLTestParser(t)); err != nil {
			t.Errorf("Parse() error = %v, want nil", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Parse() did not return within 2s for an oversized non-dollar-quoted segment")
	}
}
