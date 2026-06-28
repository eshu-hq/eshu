// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseEmptyHCLKeepsDeterministicBuckets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	for _, key := range []string{
		"terraform_resources",
		"terraform_variables",
		"terraform_outputs",
		"terraform_modules",
		"terraform_checks",
		"terraform_lock_providers",
		"terragrunt_include_warnings",
	} {
		rows := bucketForTest(t, got, key)
		if len(rows) != 0 {
			t.Fatalf("%s = %#v, want empty bucket", key, rows)
		}
	}
}

func TestParseMalformedHCLReturnsErrorWithoutPartialPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(`resource "aws_s3_bucket" "logs" {
  bucket = "logs"
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, shared.Options{})
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed HCL error")
	}
	if got != nil {
		t.Fatalf("Parse() payload = %#v, want nil on malformed HCL", got)
	}
}
