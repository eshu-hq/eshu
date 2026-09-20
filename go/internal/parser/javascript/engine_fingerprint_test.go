// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestCommonJSExportsCarryFingerprints pins the CommonJS emission wiring:
// `module.exports = function ...` reaches the fingerprint helper as an
// assignment_expression whose function lives under `right`, not `body`.
// Passing the assignment through unwrapped records no_body and the exported
// function never receives a fingerprint. A function above the token floor
// must carry body_fp_exact and body_token_count.
func TestCommonJSExportsCarryFingerprints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "handler.js")
	parsertest.WriteFile(t, sourcePath, `exports.handler = async function handler(event, context) {
  const a = event + 1;
  const b = a + 2;
  const c = b + 3;
  const d = c + 4;
  const e = d + 5;
  const f = e + 6;
  const g = f + 7;
  const h = g + 8;
  const i = h + 9;
  const j = i + 10;
  const k = j + 11;
  const m = k + 12;
  context.done(a, b, c, d, e, f, g, h, i, j, k, m);
  return m + 13;
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	item := parsertest.AssertBucketItemByName(t, got, "functions", "handler")
	exact, ok := item["body_fp_exact"].(string)
	if !ok || exact == "" {
		t.Fatalf("handler body_fp_exact = %#v, want non-empty exact fingerprint", item["body_fp_exact"])
	}
	count, ok := item["body_token_count"].(int)
	if !ok || count < 50 {
		t.Fatalf("handler body_token_count = %#v, want >= 50 (above the emission floor)", item["body_token_count"])
	}
}
