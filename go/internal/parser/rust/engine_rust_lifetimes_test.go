// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathRustCapturesFunctionLifetimes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "borrow.rs")
	writeTestFile(
		t,
		filePath,
		`fn borrow<'a>(value: &'a str) -> &'a str {
    value
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	borrow := parsertest.AssertBucketItemByName(t, got, "functions", "borrow")
	assertStringSliceFieldValue(t, borrow, "lifetime_parameters", []string{"a"})
	assertStringSliceFieldValue(t, borrow, "signature_lifetimes", []string{"a"})
	assertStringFieldValue(t, borrow, "return_lifetime", "a")
}

func TestDefaultEngineParsePathRustCapturesImplLifetimes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "container.rs")
	writeTestFile(
		t,
		filePath,
		`struct Container<'a> {
    value: &'a str,
}

impl<'a> Container<'a> {
    fn value(&self) -> &'a str {
        self.value
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	implBlock := parsertest.AssertBucketItemByName(t, got, "impl_blocks", "Container")
	assertStringFieldValue(t, implBlock, "target", "Container<'a>")
	assertStringSliceFieldValue(t, implBlock, "lifetime_parameters", []string{"a"})
	assertStringSliceFieldValue(t, implBlock, "signature_lifetimes", []string{"a"})

	value := parsertest.AssertBucketItemByName(t, got, "functions", "value")
	assertStringFieldValue(t, value, "impl_context", "Container")
	assertStringSliceFieldValue(t, value, "signature_lifetimes", []string{"a"})
	assertStringFieldValue(t, value, "return_lifetime", "a")
}
