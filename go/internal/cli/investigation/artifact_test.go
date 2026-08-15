// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
)

func TestWriteArtifactToStdout(t *testing.T) {
	t.Parallel()

	for _, out := range []string{"", "   "} {
		var stdout, stderr bytes.Buffer
		if err := investigation.WriteArtifact(&stdout, &stderr, out, []byte("packet-bytes")); err != nil {
			t.Fatalf("WriteArtifact(%q): %v", out, err)
		}
		if stdout.String() != "packet-bytes" {
			t.Fatalf("stdout = %q", stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want nothing on the stdout path", stderr.String())
		}
	}
}

// errWriter fails every write so the stdout path's error handling is covered.
type errWriter struct{ err error }

func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteArtifactStdoutErrorReachesTheCallerVerbatim(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("broken pipe")
	var stderr bytes.Buffer
	err := investigation.WriteArtifact(&errWriter{err: sentinel}, &stderr, "", []byte("x"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the writer's error", err)
	}
	// The stdout path returns the write error with no added prefix. Wrapping it
	// would change what an operator reads on a broken pipe.
	if err.Error() != "broken pipe" {
		t.Fatalf("err text = %q, want it unchanged", err.Error())
	}
}

func TestWriteArtifactToFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "packet.json")
	var stdout, stderr bytes.Buffer
	if err := investigation.WriteArtifact(&stdout, &stderr, path, []byte("packet-bytes")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing when --out is set", stdout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "packet-bytes" {
		t.Fatalf("file = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perms = %o, want 600", perm)
	}
	if got, want := stderr.String(), "wrote investigation packet to "+path+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// os.WriteFile applies its mode only when it creates the file, so regenerating
// an artifact over a world-readable file would otherwise leave it readable.
func TestWriteArtifactTightensAnExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "packet.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := investigation.WriteArtifact(&stdout, &stderr, path, []byte("fresh")); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perms = %o, want 600 after rewriting a 0644 file", perm)
	}
}

func TestWriteArtifactUnwritablePathErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "missing-dir", "packet.json")
	err := investigation.WriteArtifact(&stdout, &stderr, path, []byte("x"))
	if err == nil {
		t.Fatal("expected an error for an unwritable path")
	}
	if !strings.HasPrefix(err.Error(), "write investigation packet: ") {
		t.Fatalf("err = %q, want the write-investigation-packet prefix", err.Error())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want no success line after a failed write", stderr.String())
	}
}
