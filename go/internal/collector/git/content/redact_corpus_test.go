// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestRedactCorpusForTheFindingsDifferential writes the stored body of every
// file in ESHU_8833_REDACT_CORPUS to ESHU_8833_REDACT_OUT under the default
// registry's policy for its path (skeleton for tfvars, marker for .yarnrc.yml),
// so the #8833 findings differential can run eshu_secret_line_findings over
// the raw and the stored bodies in Postgres. It is skipped unless both are set.
func TestRedactCorpusForTheFindingsDifferential(t *testing.T) {
	in, out := os.Getenv("ESHU_8833_REDACT_CORPUS"), os.Getenv("ESHU_8833_REDACT_OUT")
	if in == "" || out == "" {
		t.Skip("set ESHU_8833_REDACT_CORPUS and ESHU_8833_REDACT_OUT")
	}
	entries, err := os.ReadDir(in)
	if err != nil {
		t.Fatalf("read %s: %v", in, err)
	}
	registry := parser.DefaultRegistry()
	written, failed := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "manifest.json" {
			continue
		}
		// Corpus files are numbered (0001.yarnrc.yml); the policy is looked up
		// under the real basename.
		lookup := entry.Name()
		if strings.HasSuffix(lookup, ".yarnrc.yml") {
			lookup = "corpus/.yarnrc.yml"
		}
		store := registry.ContentStoreForPath(lookup)
		if store.Policy != parser.ContentStoreRedact {
			t.Fatalf("%s binds %q, want redact", entry.Name(), store.Policy)
		}
		body, err := os.ReadFile(filepath.Join(in, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		result := ApplyContentStore(store, string(body))
		if result.Outcome == StoreOutcomeRedactorFailed {
			failed++
			continue
		}
		if err := os.WriteFile(filepath.Join(out, entry.Name()), []byte(result.Body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		written++
	}
	t.Logf("written=%d redactor_failed=%d", written, failed)
}
