package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDocsVerifyChecksLocalPathClaims(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs", "runbooks"), 0o700); err != nil {
		t.Fatalf("MkdirAll(docs) error = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "terraform", "prod"), 0o700); err != nil {
		t.Fatalf("MkdirAll(terraform) error = %v, want nil", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "terraform", "prod", "main.tf"), []byte("resource \"aws_s3_bucket\" \"example\" {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(terraform) error = %v, want nil", err)
	}
	docPath := filepath.Join(repo, "docs", "runbooks", "checkout.md")
	if err := os.WriteFile(
		docPath,
		[]byte("Terraform: [main](../../terraform/prod/main.tf).\nKubernetes: `deploy/kubernetes/missing.yaml`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(doc) error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want contradicted missing local path to fail")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	if got, want := envelope.Data.Summary.Valid, 1; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if !strings.Contains(out.String(), `"claim_type": "local_path"`) {
		t.Fatalf("output = %s, want local_path finding", out.String())
	}
	if got := envelope.Error; got == nil || !strings.Contains(got.Message, "contradicted") {
		t.Fatalf("Envelope.Error = %#v, want contradicted failure", got)
	}
}
