package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinInfersScopeFunctionPreservedAssignmentReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val service = createService().apply { }
    return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersAlsoScopeFunctionPreservedAssignmentReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val service = createService().also { }
    return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}

func TestDefaultEngineParsePathKotlinInfersScopeFunctionReceiverChainsForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    return createService().apply { }.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	for _, item := range items {
		name, _ := item["name"].(string)
		if name != "info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing name=%q in %#v", "info", items)
}
