package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinInfersLazyDelegatedPropertyReceiverTypesForDotCalls(t *testing.T) {
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
    val service by lazy { createService() }
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
		assertStringFieldValue(t, item, "call_kind", "kotlin_lazy_delegated_property_receiver")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}
