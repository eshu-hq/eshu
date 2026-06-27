// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"path/filepath"
	"testing"
)

// TestHelmTemplateChartB7FixtureProducesValueReference guards the B-7 corpus
// fixture tests/fixtures/ecosystems/helm-template-chart against drift. It parses
// the committed values.yaml and templates/deployment.yaml with the real parser
// and asserts at least one `.Values.<path>` usage resolves to a values.yaml leaf
// definition of the same dotted path — the contract the golden-corpus gate's
// rc-35 (HELM_TEMPLATE_VALUE_REFERENCE) asserts on the real graph backend. This
// is the fast, Docker-free proof that the fixture content produces the
// usage->definition edge the gate requires.
func TestHelmTemplateChartB7FixtureProducesValueReference(t *testing.T) {
	t.Parallel()

	chartDir := filepath.Join("..", "..", "..", "..", "tests", "fixtures",
		"ecosystems", "helm-template-chart")

	valuesPayload, err := Parse(filepath.Join(chartDir, "values.yaml"), false, Options{})
	if err != nil {
		t.Fatalf("parse fixture values.yaml: %v", err)
	}
	templatePayload, err := Parse(filepath.Join(chartDir, "templates", "deployment.yaml"), false, Options{})
	if err != nil {
		t.Fatalf("parse fixture template: %v", err)
	}

	definitions := helmRowNameSet(t, valuesPayload, "helm_value_definitions")
	usages := helmRowNameSet(t, templatePayload, "helm_template_value_usages")

	if len(definitions) == 0 {
		t.Fatalf("fixture values.yaml produced no helm_value_definitions")
	}
	if len(usages) == 0 {
		t.Fatalf("fixture template produced no helm_template_value_usages")
	}

	var resolved []string
	for name := range usages {
		if _, ok := definitions[name]; ok {
			resolved = append(resolved, name)
		}
	}
	if len(resolved) == 0 {
		t.Fatalf("no usage resolves to a definition; usages=%v definitions=%v", usages, definitions)
	}

	// Lock the headline references the rc-35 description names so a fixture edit
	// that drops them fails here, not silently in the slow gate.
	for _, want := range []string{"replicaCount", "image.repository", "image.tag", "service.port"} {
		if _, ok := usages[want]; !ok {
			t.Errorf("fixture template missing .Values usage %q", want)
		}
		if _, ok := definitions[want]; !ok {
			t.Errorf("fixture values.yaml missing leaf definition %q", want)
		}
	}
}

func helmRowNameSet(t *testing.T, payload map[string]any, bucket string) map[string]struct{} {
	t.Helper()
	rows, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] type = %T, want []map[string]any", bucket, payload[bucket])
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			set[name] = struct{}{}
		}
	}
	return set
}
