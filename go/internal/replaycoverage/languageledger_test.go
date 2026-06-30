// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoSpecsDir resolves the committed specs directory from this test file's
// location (replaycoverage -> internal -> go -> repo root -> specs), so the
// real-spec guards read the same files the gate ships.
func repoSpecsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "specs"))
}

func writeLanguageLedger(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), LanguageLedgerFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write language ledger: %v", err)
	}
	return path
}

func TestLoadLanguageLedgerSortsAndDedupesIdentity(t *testing.T) {
	path := writeLanguageLedger(t, `version: 1
language_features:
  - language: rust
    parser_backing: tree-sitter-backed
  - language: go
    parser_backing: tree-sitter-backed
  - language: argocd
    parser_backing: structured-parser-backed-exception
`)
	ledger, err := LoadLanguageLedger(path)
	if err != nil {
		t.Fatalf("LoadLanguageLedger: %v", err)
	}
	if ledger.Version != 1 {
		t.Errorf("version = %d, want 1", ledger.Version)
	}
	want := []string{"argocd", "go", "rust"}
	if len(ledger.Languages) != len(want) {
		t.Fatalf("languages = %d, want %d", len(ledger.Languages), len(want))
	}
	for i, w := range want {
		if ledger.Languages[i].Language != w {
			t.Errorf("language[%d] = %q, want %q", i, ledger.Languages[i].Language, w)
		}
	}
}

func TestLoadLanguageLedgerRejectsBlankAndDuplicate(t *testing.T) {
	for name, body := range map[string]string{
		"blank":     "version: 1\nlanguage_features:\n  - language: \"\"\n",
		"duplicate": "version: 1\nlanguage_features:\n  - language: go\n  - language: go\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadLanguageLedger(writeLanguageLedger(t, body)); err == nil {
				t.Fatalf("%s language name must be a load error", name)
			}
		})
	}
}

func TestLoadLanguageLedgerMissingFileIsError(t *testing.T) {
	// A missing ledger is the scoreboard denominator going silently empty, which
	// would falsely claim every language is covered — so it must error.
	if _, err := LoadLanguageLedger(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("missing language ledger must be an error, not a silent empty ledger")
	}
}

func TestLoadLanguageLedgerMatchesRealSpecCount(t *testing.T) {
	// The committed ledger is the real denominator. Guard the count so a silent
	// drift (a language dropped from the ledger) is caught here.
	ledger, err := LoadLanguageLedger(filepath.Join(repoSpecsDir(t), LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger(real spec): %v", err)
	}
	if len(ledger.Languages) != 32 {
		t.Fatalf("real ledger language count = %d, want 32 (update this guard only with an intentional ledger change)", len(ledger.Languages))
	}
}
