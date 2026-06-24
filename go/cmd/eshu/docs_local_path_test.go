// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestRunDocsVerifyUsesGitFileAsTruthRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: ../.git/worktrees/repo\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(.git) error = %v, want nil", err)
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
	if err := os.WriteFile(docPath, []byte("Terraform: [main](../../terraform/prod/main.tf).\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(doc) error = %v, want nil", err)
	}

	envelope, err := runDocsVerifyJSONForTest(t, docPath)
	if err != nil {
		t.Fatalf("docs verify error = %v, want nil", err)
	}
	if got, want := envelope.Data.Summary.Valid, 1; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
}

func TestRunDocsVerifyMarksEscapedLocalPathAsMissingEvidence(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o700); err != nil {
		t.Fatalf("MkdirAll(docs) error = %v, want nil", err)
	}
	docPath := filepath.Join(repo, "docs", "runbook.md")
	if err := os.WriteFile(docPath, []byte("Escaped path: `../../outside.yaml`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(doc) error = %v, want nil", err)
	}

	envelope, err := runDocsVerifyJSONForTest(t, docPath)
	if err != nil {
		t.Fatalf("docs verify error = %v, want nil", err)
	}
	if got, want := envelope.Data.Summary.MissingEvidence, 1; got != want {
		t.Fatalf("Summary.MissingEvidence = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Summary.Contradicted, 0; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
}

func runDocsVerifyJSONForTest(t *testing.T, docPath string, args ...string) (docsVerifyEnvelope, error) {
	t.Helper()

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{docPath, "--json"}, args...))
	err := cmd.Execute()
	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	return envelope, err
}
