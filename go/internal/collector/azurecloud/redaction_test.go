// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"slices"
	"testing"
)

func TestRedactExtensionDropsForbiddenKeys(t *testing.T) {
	raw := map[string]any{
		"provisioningState": "Succeeded",
		"osType":            "Linux",
		"template":          map[string]any{"resources": []any{"secret-body"}},
		"connectionString":  "Server=db;Password=hunter2;",
		"primaryAccessKey":  "abc123",
		"adminPassword":     "hunter2",
		"publicIPAddress":   "52.1.2.3",
		"privateEndpoint":   "db.internal.example",
		"responseBody":      "raw provider body",
	}
	result := RedactExtension(raw)

	if _, ok := result.Extension["provisioningState"]; !ok {
		t.Fatal("safe key provisioningState was dropped")
	}
	if _, ok := result.Extension["osType"]; !ok {
		t.Fatal("safe key osType was dropped")
	}
	forbidden := []string{
		"template", "connectionString", "primaryAccessKey", "adminPassword",
		"publicIPAddress", "privateEndpoint", "responseBody",
	}
	for _, key := range forbidden {
		if _, ok := result.Extension[key]; ok {
			t.Fatalf("forbidden key %q survived redaction", key)
		}
		if !slices.Contains(result.RedactedKeys, key) {
			t.Fatalf("forbidden key %q not recorded in RedactedKeys: %v", key, result.RedactedKeys)
		}
	}
	if !result.Redacted {
		t.Fatal("Redacted flag should be true when keys were dropped")
	}
}

func TestRedactExtensionRecursesIntoNestedMaps(t *testing.T) {
	raw := map[string]any{
		"hardwareProfile": map[string]any{
			"vmSize":   "Standard_D2s_v3",
			"password": "leak",
		},
	}
	result := RedactExtension(raw)
	nested, ok := result.Extension["hardwareProfile"].(map[string]any)
	if !ok {
		t.Fatalf("nested map missing, got %T", result.Extension["hardwareProfile"])
	}
	if _, ok := nested["password"]; ok {
		t.Fatal("nested forbidden key password survived")
	}
	if nested["vmSize"] != "Standard_D2s_v3" {
		t.Fatalf("nested safe key vmSize = %v, want Standard_D2s_v3", nested["vmSize"])
	}
	if !result.Redacted {
		t.Fatal("Redacted should be true for nested drop")
	}
}

func TestRedactExtensionCleanPayload(t *testing.T) {
	raw := map[string]any{"provisioningState": "Succeeded", "tier": "Standard"}
	result := RedactExtension(raw)
	if result.Redacted {
		t.Fatal("Redacted should be false for a clean payload")
	}
	if len(result.RedactedKeys) != 0 {
		t.Fatalf("RedactedKeys should be empty, got %v", result.RedactedKeys)
	}
}

func TestRedactExtensionNilPayload(t *testing.T) {
	result := RedactExtension(nil)
	if result.Redacted {
		t.Fatal("nil payload should not be marked redacted")
	}
	if result.Extension != nil {
		t.Fatalf("nil payload should yield nil extension, got %v", result.Extension)
	}
}

func TestRedactionPolicyVersionStable(t *testing.T) {
	if RedactionPolicyVersion == "" {
		t.Fatal("RedactionPolicyVersion must not be empty")
	}
}
