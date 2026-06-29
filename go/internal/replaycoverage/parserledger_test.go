// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParserLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "parser-backing-ledger.v1.yaml")
	body := `version: 1
parser_backing:
  - parser: hcl
    backing: HashiCorp hcl/v2 AST
  - parser: dockerfile
    backing: instruction scanner
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	ledger, err := LoadParserLedger(path)
	if err != nil {
		t.Fatalf("LoadParserLedger: %v", err)
	}
	if ledger.Version != 1 {
		t.Errorf("version = %d, want 1", ledger.Version)
	}
	if len(ledger.Parsers) != 2 {
		t.Fatalf("parsers = %d, want 2", len(ledger.Parsers))
	}
	// Entries are returned sorted by parser name for deterministic enumeration.
	if ledger.Parsers[0].Parser != "dockerfile" || ledger.Parsers[1].Parser != "hcl" {
		t.Errorf("parsers = %v, want [dockerfile hcl] sorted", []string{ledger.Parsers[0].Parser, ledger.Parsers[1].Parser})
	}
}

func TestLoadParserLedgerMissingFileIsError(t *testing.T) {
	if _, err := LoadParserLedger(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("expected error for missing ledger file, got nil")
	}
}

func TestLoadParserLedgerRejectsBlankParser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.yaml")
	body := `version: 1
parser_backing:
  - parser: ""
    backing: nothing
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	if _, err := LoadParserLedger(path); err == nil {
		t.Fatal("expected error for blank parser name, got nil")
	}
}
