// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestBuildCodeCallRowMapDerivesTieredConfidence(t *testing.T) {
	cases := []struct {
		name           string
		method         codeprovenance.Method
		wantConfidence float64
	}{
		{"scip", codeprovenance.MethodSCIP, 0.99},
		{"same_file", codeprovenance.MethodSameFile, 0.95},
		{"import_binding", codeprovenance.MethodImportBinding, 0.90},
		{"type_inferred", codeprovenance.MethodTypeInferred, 0.80},
		{"scope_unique_name", codeprovenance.MethodScopeUniqueName, 0.70},
		{"repo_unique_name", codeprovenance.MethodRepoUniqueName, 0.50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"caller_entity_id":  "uid:a",
				"callee_entity_id":  "uid:b",
				"resolution_method": tc.method,
			}
			_, rowMap, ok := buildCodeCallRowMap(payload, "parser/code-calls")
			if !ok {
				t.Fatal("buildCodeCallRowMap ok = false, want true")
			}
			if got := rowMap["confidence"]; got != tc.wantConfidence {
				t.Errorf("confidence = %v, want %v", got, tc.wantConfidence)
			}
			if got := rowMap["resolution_method"]; got != tc.method {
				t.Errorf("resolution_method = %v, want %v", got, tc.method)
			}
			if reason, _ := rowMap["reason"].(string); reason == "" {
				t.Error("reason is empty, want a mechanism reason")
			}
		})
	}
}

func TestBuildCodeCallRowMapDefaultsToLegacyConfidence(t *testing.T) {
	payload := map[string]any{
		"caller_entity_id": "uid:a",
		"callee_entity_id": "uid:b",
	}
	_, rowMap, ok := buildCodeCallRowMap(payload, "parser/code-calls")
	if !ok {
		t.Fatal("buildCodeCallRowMap ok = false, want true")
	}
	if got := rowMap["resolution_method"]; got != codeprovenance.MethodUnspecified {
		t.Errorf("resolution_method = %v, want %q", got, codeprovenance.MethodUnspecified)
	}
	if got := rowMap["confidence"]; got != codeprovenance.LegacyConfidence {
		t.Errorf("confidence = %v, want %v", got, codeprovenance.LegacyConfidence)
	}
}

func TestBuildCodeCallRowMapMetaclassCarriesDeclaredConfidence(t *testing.T) {
	payload := map[string]any{
		"relationship_type": "USES_METACLASS",
		"source_entity_id":  "uid:a",
		"target_entity_id":  "uid:b",
		"resolution_method": codeprovenance.MethodDeclared,
	}
	cypher, rowMap, ok := buildCodeCallRowMap(payload, "parser/python-metaclass")
	if !ok {
		t.Fatal("buildCodeCallRowMap ok = false, want true")
	}
	if got := rowMap["confidence"]; got != 0.95 {
		t.Errorf("metaclass confidence = %v, want 0.95", got)
	}
	if got := rowMap["resolution_method"]; got != codeprovenance.MethodDeclared {
		t.Errorf("metaclass resolution_method = %v, want %q", got, codeprovenance.MethodDeclared)
	}
	if !strings.Contains(cypher, "rel.confidence = row.confidence") {
		t.Error("metaclass cypher is not confidence-parameterized")
	}
}

// TestCodeCallCypherTemplatesAreParameterized guards against regressing to a
// hard-coded confidence literal on any code-edge template.
func TestCodeCallCypherTemplatesAreParameterized(t *testing.T) {
	templates := map[string]string{
		"call":            batchCanonicalCodeCallUpsertCypher,
		"reference":       batchCanonicalCodeReferenceUpsertCypher,
		"metaclass":       batchCanonicalMetaclassUpsertCypher,
		"label-call":      buildLabelScopedCodeCallCypher("Function", "Function"),
		"label-reference": buildLabelScopedCodeReferenceCypher("Function", "Function"),
	}
	for name, cypher := range templates {
		if !strings.Contains(cypher, "rel.confidence = row.confidence") {
			t.Errorf("%s template is not confidence-parameterized", name)
		}
		if !strings.Contains(cypher, "rel.resolution_method = row.resolution_method") {
			t.Errorf("%s template does not persist resolution_method", name)
		}
		if strings.Contains(cypher, "confidence = 0.95") {
			t.Errorf("%s template still hard-codes confidence = 0.95", name)
		}
	}
}
