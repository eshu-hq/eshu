// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func writeDoc(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, parser.AdmissionDocPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunRegeneratesIdempotentlyAndCheckDetectsDrift(t *testing.T) {
	root := writeDoc(t, "# Page\n\n"+parser.AdmissionTableBegin+"\nstale\n"+parser.AdmissionTableEnd+"\n\ntail\n")
	var out, errOut bytes.Buffer

	if err := run([]string{"-repo-root", root, "-check"}, &out, &errOut); err == nil {
		t.Fatal("-check must fail on a stale page")
	}
	if err := run([]string{"-repo-root", root}, &out, &errOut); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(root, parser.AdmissionDocPath))
	if !strings.Contains(string(first), "| `gomod` | go |") || !strings.HasSuffix(string(first), "\ntail\n") {
		t.Fatalf("unexpected regenerated page:\n%s", first)
	}
	if err := run([]string{"-repo-root", root}, &out, &errOut); err != nil {
		t.Fatalf("second regenerate: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(root, parser.AdmissionDocPath))
	if !bytes.Equal(first, second) {
		t.Fatal("regeneration is not byte-idempotent")
	}
	if err := run([]string{"-repo-root", root, "-check"}, &out, &errOut); err != nil {
		t.Fatalf("-check on a fresh page: %v", err)
	}
}

func TestRunRejectsMissingMarkersAndRoot(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"-repo-root", writeDoc(t, "no markers\n")}, &out, &errOut); err == nil {
		t.Fatal("missing markers must fail")
	}
	if err := run(nil, &out, &errOut); err == nil {
		t.Fatal("missing -repo-root must fail")
	}
}

func TestRunPreservesDocFileMode(t *testing.T) {
	root := writeDoc(t, "# Page\n\n"+parser.AdmissionTableBegin+"\nstale\n"+parser.AdmissionTableEnd+"\n")
	path := filepath.Join(root, parser.AdmissionDocPath)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := run([]string{"-repo-root", root}, &out, &errOut); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("regenerated page mode = %o, want 640", got)
	}
}

func TestRunRejectsMissingDoc(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"-repo-root", t.TempDir()}, &out, &errOut); err == nil {
		t.Fatal("a repo root without the admission page must fail")
	}
}
