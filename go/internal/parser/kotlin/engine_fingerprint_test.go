// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestKotlinFunctionsCarryFingerprints pins the kotlin emission wiring:
// tree-sitter-kotlin names the body child `function_body` without a `body`
// field, so passing ChildByFieldName("body") silently records no_body for
// every function. A function above the token floor must carry body_fp_exact
// and body_token_count.
func TestKotlinFunctionsCarryFingerprints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Svc.kt")
	writeKotlinTestFile(t, sourcePath, `package demo

class Svc {
    constructor(seed: Int) {
        // secondary-constructor body walks the bare block child
        val a = seed + 1
        val b = a * 2
        val c = b - seed
        val d = c + a
        val e = d * b
        val f = e - c
        val g = f + d
        val h = g * e
        val i = h - f
        val total = i + g + h
        println(total)
    }

    fun compute(a: Int, b: Int): Int {
        // interior comment exercises the exact-only comment table
        val x = a + b
        val y = x * 2
        val z = y - a
        val w = z + b
        val v = w * x
        val u = v - y
        val s = u + z
        val r = s * w
        val q = r - v
        val total = q + u + s + r
        return total
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFingerprinted := func(name string) {
		t.Helper()
		item := parsertest.AssertBucketItemByName(t, got, "functions", name)
		exact, ok := item["body_fp_exact"].(string)
		if !ok || exact == "" {
			t.Fatalf("%s body_fp_exact = %#v, want non-empty exact fingerprint", name, item["body_fp_exact"])
		}
		count, ok := item["body_token_count"].(int)
		if !ok || count < 50 {
			t.Fatalf("%s body_token_count = %#v, want >= 50 (above the emission floor)", name, item["body_token_count"])
		}
	}
	assertFingerprinted("compute")
	assertFingerprinted("constructor")
}
