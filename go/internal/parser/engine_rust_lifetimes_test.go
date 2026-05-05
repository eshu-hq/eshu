package parser

import (
	"path/filepath"
	"testing"
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	borrow := assertBucketItemByName(t, got, "functions", "borrow")
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	implBlock := assertBucketItemByName(t, got, "impl_blocks", "Container")
	assertStringFieldValue(t, implBlock, "target", "Container<'a>")
	assertStringSliceFieldValue(t, implBlock, "lifetime_parameters", []string{"a"})
	assertStringSliceFieldValue(t, implBlock, "signature_lifetimes", []string{"a"})

	value := assertBucketItemByName(t, got, "functions", "value")
	assertStringFieldValue(t, value, "impl_context", "Container")
	assertStringSliceFieldValue(t, value, "signature_lifetimes", []string{"a"})
	assertStringFieldValue(t, value, "return_lifetime", "a")
}
