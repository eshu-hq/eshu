// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunIsAdvisoryWhenEveryCheckFails pins the contract doc.go, README.md and
// AGENTS.md all state: every check is advisory and Run returns nil even when
// nothing on the machine is right. Without this the claim rests on the other
// tests happening not to assert an error, which is not the same as pinning it.
//
// The behaviour matters: an operator running doctor already knows something is
// wrong, and the combination of findings is what identifies the cause. Bailing
// on the first failure would hide the rest.
func TestRunIsAdvisoryWhenEveryCheckFails(t *testing.T) {
	var buf bytes.Buffer
	err := Run(&buf, Deps{
		ConfigDir:   "/nonexistent",
		EnvFilePath: "/nonexistent/.env",
		APIBaseURL:  "http://127.0.0.1:1",
		Stat:        func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		LookPath:    func(string) (string, error) { return "", os.ErrNotExist },
		HTTPClient:  &http.Client{Timeout: time.Second, Transport: errorTransport{}},
	})
	if err != nil {
		t.Fatalf("Run() with every check failing = %v, want nil (checks are advisory)", err)
	}

	// Every failing check still has to be reported, or "advisory" would just
	// mean "silent".
	for _, want := range []string{
		"Config directory missing",
		"Config file missing",
		"not found in PATH",
		"API not reachable",
		"Neo4j URI not configured",
		"Postgres DSN not configured",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("report missing %q\n%s", want, buf.String())
		}
	}
}

// TestNilDepsFallBackToTheRealMachine pins README.md's statement that Deps
// fields left nil fall back to os.Stat and exec.LookPath. That sentence is the
// contract for every caller that fills in only part of Deps, and nothing else
// tested it -- the other tests all supply both seams.
func TestNilDepsFallBackToTheRealMachine(t *testing.T) {
	// A directory that really exists, resolved by the real os.Stat.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("x=1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	var buf bytes.Buffer
	err := Run(&buf, Deps{
		ConfigDir:   dir,
		EnvFilePath: envFile,
		APIBaseURL:  "http://127.0.0.1:1",
		// Stat and LookPath deliberately nil: this is the assertion.
		HTTPClient: &http.Client{Timeout: time.Second, Transport: errorTransport{}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Config directory exists: "+dir) {
		t.Errorf("nil Stat did not fall back to os.Stat; the real directory was reported missing\n%s", out)
	}
	if !strings.Contains(out, "Config file exists: "+envFile) {
		t.Errorf("nil Stat did not fall back to os.Stat for the settings file\n%s", out)
	}
	// The real exec.LookPath ran for each service binary. Whether they are
	// installed depends on the machine, so assert the line count rather than
	// the verdict -- a nil LookPath that panicked or was skipped would emit
	// none of them.
	reported := 0
	for _, bin := range serviceBinaries {
		if strings.Contains(out, bin) {
			reported++
		}
	}
	if reported != len(serviceBinaries) {
		t.Errorf("nil LookPath reported %d of %d service binaries; the fallback did not run",
			reported, len(serviceBinaries))
	}
}

// TestHealthTimeoutMatchesTheDocumentedBound pins the "3-second" figure stated
// in README.md and doc.go. A doc that names a specific number and a constant
// that drifts from it is how a doc becomes untrustworthy one edit at a time.
func TestHealthTimeoutMatchesTheDocumentedBound(t *testing.T) {
	if healthTimeout != 3*time.Second {
		t.Fatalf("healthTimeout = %v, but the package docs promise 3 seconds", healthTimeout)
	}
}
