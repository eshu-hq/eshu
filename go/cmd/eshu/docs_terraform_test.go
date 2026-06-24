// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDocsVerifyChecksTerraformAddressClaims(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	stack := filepath.Join(root, "terraform", "prod")
	if err := os.MkdirAll(stack, 0o700); err != nil {
		t.Fatalf("MkdirAll(terraform/prod) error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(stack, "main.tf"),
		[]byte(`resource "aws_s3_bucket" "logs" {}
data "aws_iam_policy_document" "reader" {}
module "network" {
  source = "../modules/network"
}
`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(main.tf) error = %v, want nil", err)
	}
	docPath := filepath.Join(root, "README.md")
	if err := os.WriteFile(
		docPath,
		[]byte(""+
			"Bucket: `terraform/aws_s3_bucket.logs`.\n"+
			"Policy: `terraform/data.aws_iam_policy_document.reader`.\n"+
			"Module: `terraform/module.network`.\n"+
			"Queue: `terraform/aws_sqs_queue.missing`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for contradicted Terraform address")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	if got, want := envelope.Data.Summary.Valid, 3; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	assertDocsVerifyFinding(t, envelope.Data.Findings, "terraform_address", "aws_s3_bucket.logs", "valid")
	assertDocsVerifyFinding(t, envelope.Data.Findings, "terraform_address", "data.aws_iam_policy_document.reader", "valid")
	assertDocsVerifyFinding(t, envelope.Data.Findings, "terraform_address", "module.network", "valid")
	assertDocsVerifyFinding(t, envelope.Data.Findings, "terraform_address", "aws_sqs_queue.missing", "contradicted")
}

func TestDocsVerifyTerraformTruthMarksInvalidHCLIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte(`resource "aws_s3_bucket" "logs" {`), 0o600); err != nil {
		t.Fatalf("WriteFile(main.tf) error = %v, want nil", err)
	}

	_, complete := docsVerifyTerraformAddressTruth(root)
	if complete {
		t.Fatal("docsVerifyTerraformAddressTruth complete = true, want false for invalid HCL")
	}
}

func TestRunDocsVerifyReportsTerraformMissingEvidenceWhenTruthIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte(`resource "aws_s3_bucket" "logs" {`), 0o600); err != nil {
		t.Fatalf("WriteFile(main.tf) error = %v, want nil", err)
	}
	docPath := filepath.Join(root, "README.md")
	if err := os.WriteFile(docPath, []byte("Bucket: `terraform/aws_s3_bucket.logs`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil for missing evidence with --fail-on contradicted; output=%s", err, out.String())
	}

	var envelope docsVerifyEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if got, want := envelope.Data.Summary.MissingEvidence, 1; got != want {
		t.Fatalf("Summary.MissingEvidence = %d, want %d", got, want)
	}
	if got := envelope.Data.Summary.Contradicted; got != 0 {
		t.Fatalf("Summary.Contradicted = %d, want 0", got)
	}
	assertDocsVerifyFinding(t, envelope.Data.Findings, "terraform_address", "aws_s3_bucket.logs", "missing_evidence")
}

func BenchmarkDocsVerifyTerraformAddressTruthLargeTree(b *testing.B) {
	root := b.TempDir()
	stack := filepath.Join(root, "terraform")
	if err := os.MkdirAll(stack, 0o700); err != nil {
		b.Fatalf("MkdirAll(terraform) error = %v, want nil", err)
	}
	for i := range 200 {
		content := fmt.Sprintf(`resource "aws_s3_bucket" "logs_%d" {}
data "aws_iam_policy_document" "reader_%d" {}
module "network_%d" {
  source = "../modules/network"
}
`, i, i, i)
		if err := os.WriteFile(filepath.Join(stack, fmt.Sprintf("main_%03d.tf", i)), []byte(content), 0o600); err != nil {
			b.Fatalf("WriteFile(main_%03d.tf) error = %v, want nil", i, err)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		addresses, complete := docsVerifyTerraformAddressTruth(root)
		if !complete {
			b.Fatal("docsVerifyTerraformAddressTruth complete = false, want true")
		}
		if got, want := len(addresses), 600; got != want {
			b.Fatalf("address count = %d, want %d", got, want)
		}
	}
}
