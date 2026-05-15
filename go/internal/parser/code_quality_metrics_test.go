package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoFunctionParameterCount(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "payments.go")
	writeTestFile(
		t,
		filePath,
		`package payments

func BuildPayment(ctx Context, id string, amount int, currency string, capture bool, metadata map[string]string) error {
	return nil
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

	function := assertFunctionByName(t, got, "BuildPayment")
	assertIntFieldValue(t, function, "parameter_count", 6)
}

func TestDefaultEngineParsePathTypeScriptFunctionParameterCount(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "payments.ts")
	writeTestFile(
		t,
		filePath,
		`export function buildPayment(id: string, amount: number, currency: string, capture: boolean, customerId: string, metadata: Record<string, string>) {
  return { id, amount, currency, capture, customerId, metadata };
}

export function compareDefaults(a = x < y, b = "left,right", c = "x,y") {
  return { a, b, c };
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

	function := assertFunctionByName(t, got, "buildPayment")
	assertIntFieldValue(t, function, "parameter_count", 6)
	defaultsFunction := assertFunctionByName(t, got, "compareDefaults")
	assertIntFieldValue(t, defaultsFunction, "parameter_count", 3)
}
