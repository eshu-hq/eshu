// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTerraformAddressTruthMarksInvalidHCLIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte(`resource "aws_s3_bucket" "logs" {`), 0o600); err != nil {
		t.Fatalf("WriteFile(main.tf) error = %v, want nil", err)
	}

	_, complete := terraformAddressTruth(root)
	if complete {
		t.Fatal("terraformAddressTruth complete = true, want false for invalid HCL")
	}
}

func BenchmarkTerraformAddressTruthLargeTree(b *testing.B) {
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
		addresses, complete := terraformAddressTruth(root)
		if !complete {
			b.Fatal("terraformAddressTruth complete = false, want true")
		}
		if got, want := len(addresses), 600; got != want {
			b.Fatalf("address count = %d, want %d", got, want)
		}
	}
}
