package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsVerifyCommandIsRegistered(t *testing.T) {
	t.Parallel()

	cmd, _, err := rootCmd.Find([]string{"docs", "verify"})
	if err != nil {
		t.Fatalf("rootCmd.Find(docs verify) error = %v, want nil", err)
	}
	if cmd == nil {
		t.Fatal("docs verify command missing")
	}
}

func TestRunDocsVerifyJSONReportsContradictedClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu vaporize all`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newDocsVerifyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for contradicted finding")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	data := envelope.Data
	if got, want := int(data.Summary.Contradicted), 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got := envelope.Error; got == nil || !strings.Contains(got.Message, "contradicted") {
		t.Fatalf("Envelope.Error = %#v, want contradicted failure", got)
	}
}

func TestRunDocsVerifyTextReportsValidCommandAndEndpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "runbook.md")
	if err := os.WriteFile(
		docPath,
		[]byte("Run `eshu docs verify .` and call `GET /api/v0/documentation/findings`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newDocsVerifyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--limit", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "valid=2") {
		t.Fatalf("output = %q, want valid=2", out.String())
	}
}
