// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeReadCapFile writes size bytes of source-shaped text to a temp file and
// returns its path.
func writeReadCapFile(t *testing.T, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.py")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestSourceReadCapDefault pins the documented default (#8986): 64 MiB, far
// above the largest lock file measured in the repository and golden corpus
// (274,035 bytes), and the test setter restores it.
func TestSourceReadCapDefault(t *testing.T) {
	if got := SourceReadCap(); got != MaxSourceBytes {
		t.Fatalf("SourceReadCap() = %d, want MaxSourceBytes %d", got, MaxSourceBytes)
	}
	if MaxSourceBytes != 64<<20 {
		t.Fatalf("MaxSourceBytes = %d, want 64 MiB", MaxSourceBytes)
	}
	restore := SetSourceReadCapForTest(10)
	if got := SourceReadCap(); got != 10 {
		t.Fatalf("SourceReadCap() after set = %d, want 10", got)
	}
	restore()
	if got := SourceReadCap(); got != MaxSourceBytes {
		t.Fatalf("SourceReadCap() after restore = %d, want %d", got, MaxSourceBytes)
	}
}

// TestReadFileCappedEnforcesTheCap (#8986) covers the boundary: a file at the
// cap reads whole, a file one byte over returns ErrSourceTooLarge and no body.
// Not parallel: it lowers the process-wide cap.
func TestReadFileCappedEnforcesTheCap(t *testing.T) {
	defer SetSourceReadCapForTest(16)()

	atCap := writeReadCapFile(t, 16)
	body, err := ReadFileCapped(atCap)
	if err != nil {
		t.Fatalf("ReadFileCapped(at cap) error = %v, want nil", err)
	}
	if len(body) != 16 {
		t.Fatalf("ReadFileCapped(at cap) len = %d, want 16", len(body))
	}

	overCap := writeReadCapFile(t, 17)
	body, err = ReadFileCapped(overCap)
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("ReadFileCapped(over cap) error = %v, want ErrSourceTooLarge", err)
	}
	if body != nil {
		t.Fatalf("ReadFileCapped(over cap) body len = %d, want nil", len(body))
	}

	if _, err := ReadFileCapped(filepath.Join(t.TempDir(), "missing.py")); err == nil || errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("ReadFileCapped(missing) error = %v, want a not-exist error", err)
	}
}

// TestReadSourceRejectsAFileOverTheCap (#8986) is the shared read path every
// language parser uses: a file over the cap never reaches a parser, whether
// ReadSource reads it from disk or a caller primed its bytes. Not parallel: it
// lowers the process-wide cap.
func TestReadSourceRejectsAFileOverTheCap(t *testing.T) {
	defer SetSourceReadCapForTest(16)()

	overCap := writeReadCapFile(t, 17)
	if _, err := ReadSource(overCap); !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("ReadSource(disk, over cap) error = %v, want ErrSourceTooLarge", err)
	}

	primed := filepath.Join(t.TempDir(), "primed.py")
	PrimeSource(primed, bytes.Repeat([]byte("y"), 17))
	defer ClearSource(primed)
	if _, err := ReadSource(primed); !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("ReadSource(primed, over cap) error = %v, want ErrSourceTooLarge", err)
	}

	underCap := writeReadCapFile(t, 16)
	got, err := ReadSource(underCap)
	if err != nil {
		t.Fatalf("ReadSource(under cap) error = %v, want nil", err)
	}
	if len(got) != 16 {
		t.Fatalf("ReadSource(under cap) len = %d, want 16", len(got))
	}
}
