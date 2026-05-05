package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinPrefersPackageAwareSiblingFunctionReturnTypesAcrossSiblingDirectoriesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "usage", "Usage.kt")
	writeTestFile(
		t,
		apiPath,
		`package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeTestFile(
		t,
		otherPath,
		`package otherpkg

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`,
	)
	writeTestFile(
		t,
		usagePath,
		`package com.example

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			assertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}

func TestDefaultEngineParsePathKotlinPrefersPackageAwareSiblingFunctionReturnTypesAcrossDeeperPackageDirectoriesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "feature", "module", "deep", "Usage.kt")
	writeTestFile(
		t,
		apiPath,
		`package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`,
	)
	writeTestFile(
		t,
		otherPath,
		`package com.other

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`,
	)
	writeTestFile(
		t,
		usagePath,
		`package com.example

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, usagePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	want := map[string]string{
		"createFactory().createService": "Factory",
		"service.info":                  "Service",
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if wantType, ok := want[fullName]; ok {
			assertStringFieldValue(t, item, "inferred_obj_type", wantType)
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing inferred receiver calls: %#v in %#v", want, items)
	}
}
