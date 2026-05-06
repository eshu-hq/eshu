package reducer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesKotlinPackageAwareSiblingFunctionReturnTypesAcrossSiblingDirectoriesUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "usage", "Usage.kt")
	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v, want nil", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(apiPath, `package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`)
	writeFile(otherPath, `package otherpkg

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`)
	writeFile(usagePath, `package com.example

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	apiPayload, err := engine.ParsePath(repoRoot, apiPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", apiPath, err)
	}
	usagePayload, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}
	if functions, ok := apiPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "createFactory":
				function["uid"] = "content-entity:kotlin-create-factory"
			case name == "createService" && classContext == "Factory":
				function["uid"] = "content-entity:kotlin-factory-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if functions, ok := usagePayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			if name == "usage" {
				function["end_line"] = 6
				function["uid"] = "content-entity:kotlin-usage"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/usage/Usage.kt",
				"parsed_file_data": usagePayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/api/Api.kt",
				"parsed_file_data": apiPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v", len(rows), rows)
	}

	want := map[string]string{
		"content-entity:kotlin-factory-create-service": "createFactory().createService",
		"content-entity:kotlin-service-info":           "service.info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if got := row["full_name"]; got != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", got, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinPackageAwareSiblingFunctionReturnTypesAcrossDeeperPackageDirectoriesUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "feature", "module", "deep", "Usage.kt")
	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v, want nil", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(apiPath, `package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`)
	writeFile(otherPath, `package com.other

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`)
	writeFile(usagePath, `package com.example

fun usage(): String {
    val service = createFactory().createService()
    return service.info()
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	apiPayload, err := engine.ParsePath(repoRoot, apiPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", apiPath, err)
	}
	usagePayload, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}
	if functions, ok := apiPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "createFactory":
				function["uid"] = "content-entity:kotlin-create-factory"
			case name == "createService" && classContext == "Factory":
				function["uid"] = "content-entity:kotlin-factory-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if functions, ok := usagePayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			if name == "usage" {
				function["end_line"] = 6
				function["uid"] = "content-entity:kotlin-usage"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/feature/module/deep/Usage.kt",
				"parsed_file_data": usagePayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/api/Api.kt",
				"parsed_file_data": apiPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v", len(rows), rows)
	}

	want := map[string]string{
		"content-entity:kotlin-factory-create-service": "createFactory().createService",
		"content-entity:kotlin-service-info":           "service.info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if got := row["full_name"]; got != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", got, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinPackageAwareDirectFunctionReturnReceiverChainsAcrossSiblingDirectoriesUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "usage", "Usage.kt")
	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v, want nil", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(apiPath, `package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`)
	writeFile(otherPath, `package otherpkg

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`)
	writeFile(usagePath, `package com.example

fun usage(): String {
    return createFactory().createService().info()
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	apiPayload, err := engine.ParsePath(repoRoot, apiPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", apiPath, err)
	}
	usagePayload, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}
	if functions, ok := apiPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "createFactory":
				function["uid"] = "content-entity:kotlin-create-factory"
			case name == "createService" && classContext == "Factory":
				function["uid"] = "content-entity:kotlin-factory-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if functions, ok := usagePayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			if name, _ := function["name"].(string); name == "usage" {
				function["end_line"] = 5
				function["uid"] = "content-entity:kotlin-usage"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/usage/Usage.kt",
				"parsed_file_data": usagePayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/api/Api.kt",
				"parsed_file_data": apiPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, usagePayload["function_calls"])
	}

	want := map[string]string{
		"content-entity:kotlin-factory-create-service": "createFactory().createService",
		"content-entity:kotlin-service-info":           "createFactory().createService().info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if got := row["full_name"]; got != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", got, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesKotlinPackageAwareDirectFunctionReturnReceiverChainsAcrossDeeperPackageDirectoriesUsingInferredObjectType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "api", "Api.kt")
	otherPath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "other", "Other.kt")
	usagePath := filepath.Join(repoRoot, "src", "main", "kotlin", "com", "example", "feature", "module", "deep", "Usage.kt")
	writeFile := func(path string, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v, want nil", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
		}
	}

	writeFile(apiPath, `package com.example

class Service {
    fun info(): String = "ok"
}

class Factory {
    fun createService(): Service = Service()
}

fun createFactory(): Factory = Factory()
`)
	writeFile(otherPath, `package com.other

class OtherFactory {
    fun createService(): String = "wrong"
}

fun createFactory(): OtherFactory = OtherFactory()
`)
	writeFile(usagePath, `package com.example

fun usage(): String {
    return createFactory().createService().info()
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	apiPayload, err := engine.ParsePath(repoRoot, apiPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", apiPath, err)
	}
	usagePayload, err := engine.ParsePath(repoRoot, usagePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", usagePath, err)
	}
	if functions, ok := apiPayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			name, _ := function["name"].(string)
			classContext, _ := function["class_context"].(string)
			switch {
			case name == "createFactory":
				function["uid"] = "content-entity:kotlin-create-factory"
			case name == "createService" && classContext == "Factory":
				function["uid"] = "content-entity:kotlin-factory-create-service"
			case name == "info" && classContext == "Service":
				function["uid"] = "content-entity:kotlin-service-info"
			}
		}
	}
	if functions, ok := usagePayload["functions"].([]map[string]any); ok {
		for _, function := range functions {
			if name, _ := function["name"].(string); name == "usage" {
				function["end_line"] = 5
				function["uid"] = "content-entity:kotlin-usage"
			}
		}
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-kotlin",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/feature/module/deep/Usage.kt",
				"parsed_file_data": usagePayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-kotlin",
				"relative_path":    "src/main/kotlin/com/example/api/Api.kt",
				"parsed_file_data": apiPayload,
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v; function_calls=%#v", len(rows), rows, usagePayload["function_calls"])
	}

	want := map[string]string{
		"content-entity:kotlin-factory-create-service": "createFactory().createService",
		"content-entity:kotlin-service-info":           "createFactory().createService().info",
	}
	for _, row := range rows {
		calleeID, _ := row["callee_entity_id"].(string)
		if expectedFullName, ok := want[calleeID]; ok {
			if got := row["full_name"]; got != expectedFullName {
				t.Fatalf("full_name = %#v, want %#v for callee %#v", got, expectedFullName, calleeID)
			}
			delete(want, calleeID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected callee rows: %#v; rows=%#v", want, rows)
	}
}
