// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadEnvelopeFromFile proves a non-empty path reads the file and a
// missing file fails with the path preserved in the error.
func TestReadEnvelopeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.json")
	if err := os.WriteFile(path, []byte(`{"data":{}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	raw, err := ReadEnvelope(strings.NewReader("ignored"), path)
	if err != nil {
		t.Fatalf("ReadEnvelope error: %v", err)
	}
	if string(raw) != `{"data":{}}` {
		t.Fatalf("ReadEnvelope = %q, want file contents", raw)
	}

	missing := filepath.Join(dir, "absent.json")
	if _, err := ReadEnvelope(strings.NewReader(""), missing); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("ReadEnvelope(missing) error = %v, want error naming %q", err, missing)
	}
}

// TestReadEnvelopeFromStdin proves an empty path falls back to the reader.
func TestReadEnvelopeFromStdin(t *testing.T) {
	raw, err := ReadEnvelope(strings.NewReader(`{"data":{}}`), "  ")
	if err != nil {
		t.Fatalf("ReadEnvelope error: %v", err)
	}
	if string(raw) != `{"data":{}}` {
		t.Fatalf("ReadEnvelope = %q, want stdin contents", raw)
	}
}
