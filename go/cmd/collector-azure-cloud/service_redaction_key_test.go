// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadRedactionKeyEmptyPathYieldsZeroKey proves an unset key path disables
// tag observation emission (zero key) rather than failing, so the collector
// stays backward compatible when no key is configured.
func TestLoadRedactionKeyEmptyPathYieldsZeroKey(t *testing.T) {
	key, err := loadRedactionKey("")
	if err != nil {
		t.Fatalf("empty path: unexpected error %v", err)
	}
	if !key.IsZero() {
		t.Fatal("empty key path must yield a zero key (tag observation disabled)")
	}
}

// TestLoadRedactionKeyValidFileYieldsKey proves a populated key file produces a
// usable non-zero redaction key.
func TestLoadRedactionKeyValidFileYieldsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("sufficient-azure-redaction-key-material"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	key, err := loadRedactionKey(path)
	if err != nil {
		t.Fatalf("valid key file: %v", err)
	}
	if key.IsZero() {
		t.Fatal("valid key file must yield a non-zero key")
	}
}

// TestLoadRedactionKeyBlankFileRejected proves a blank/whitespace key file fails
// closed so facts are never emitted with an unkeyed marker.
func TestLoadRedactionKeyBlankFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write blank key file: %v", err)
	}
	if _, err := loadRedactionKey(path); err == nil {
		t.Fatal("blank key file must be rejected")
	}
}

// TestLoadRedactionKeyMissingFileRejected proves a configured-but-unreadable key
// path is a hard error, never a silent keyless run.
func TestLoadRedactionKeyMissingFileRejected(t *testing.T) {
	if _, err := loadRedactionKey(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("missing key file must error")
	}
}
