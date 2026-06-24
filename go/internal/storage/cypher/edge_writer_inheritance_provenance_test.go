// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestBuildInheritanceRowMapDerivesTieredConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		relationshipType string
		method           codeprovenance.Method
		wantConfidence   float64
	}{
		{"inherits", "INHERITS", codeprovenance.MethodDeclared, 0.95},
		{"implements", "IMPLEMENTS", codeprovenance.MethodTypeInferred, 0.80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{
				"child_entity_id":   "uid:child",
				"parent_entity_id":  "uid:parent",
				"relationship_type": tc.relationshipType,
				"resolution_method": tc.method,
			}
			_, rowMap, ok := buildInheritanceRowMap(payload, "reducer/inheritance")
			if !ok {
				t.Fatal("buildInheritanceRowMap ok = false, want true")
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

func TestBuildInheritanceRowMapDefaultsToLegacyConfidence(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"child_entity_id":  "uid:child",
		"parent_entity_id": "uid:parent",
	}
	_, rowMap, ok := buildInheritanceRowMap(payload, "reducer/inheritance")
	if !ok {
		t.Fatal("buildInheritanceRowMap ok = false, want true")
	}
	if got := rowMap["resolution_method"]; got != codeprovenance.MethodUnspecified {
		t.Errorf("resolution_method = %v, want %q", got, codeprovenance.MethodUnspecified)
	}
	if got := rowMap["confidence"]; got != codeprovenance.LegacyConfidence {
		t.Errorf("confidence = %v, want %v", got, codeprovenance.LegacyConfidence)
	}
}

func TestInheritanceCypherTemplatesAreParameterized(t *testing.T) {
	t.Parallel()

	templates := map[string]string{
		"inherits":         batchCanonicalInheritanceEdgeUpsertCypher,
		"overrides":        batchCanonicalInheritanceOverrideUpsertCypher,
		"aliases":          batchCanonicalInheritanceAliasUpsertCypher,
		"implements":       batchCanonicalImplementsEdgeUpsertCypher,
		"label-inherits":   buildLabelScopedInheritanceCypher("Class", "Class", "INHERITS"),
		"label-implements": buildLabelScopedInheritanceCypher("Class", "Interface", "IMPLEMENTS"),
	}
	for name, cypher := range templates {
		if !strings.Contains(cypher, "rel.confidence = row.confidence") {
			t.Errorf("%s template is not confidence-parameterized", name)
		}
		if !strings.Contains(cypher, "rel.reason = row.reason") {
			t.Errorf("%s template is not reason-parameterized", name)
		}
		if !strings.Contains(cypher, "rel.resolution_method = row.resolution_method") {
			t.Errorf("%s template does not persist resolution_method", name)
		}
		if strings.Contains(cypher, "confidence = 0.95") {
			t.Errorf("%s template still hard-codes confidence = 0.95", name)
		}
	}
}
