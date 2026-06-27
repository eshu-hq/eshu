// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"
)

// writeHelmChartFixture stages a minimal chart directory with a Chart.yaml,
// values.yaml, and one template under templates/ so isHelmTemplateManifest
// recognizes the template (it requires a sibling ../Chart.yaml). It returns the
// chart root directory.
func writeHelmChartFixture(t *testing.T, valuesBody, templateBody string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("name: webapp\nversion: 1.0.0\n"), 0o600); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(valuesBody), 0o600); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}
	templatesDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templatesDir, 0o750); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "deployment.yaml"), []byte(templateBody), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	return dir
}

// TestParseHelmValuesEmitsValueDefinitions proves values.yaml is flattened into
// one helm_value_definitions row per leaf key, carrying the dotted path and the
// source line, which the HelmValueDefinition node is built from.
func TestParseHelmValuesEmitsValueDefinitions(t *testing.T) {
	t.Parallel()

	valuesBody := `replicaCount: 2
image:
  repository: nginx
  tag: "1.27"
service:
  port: 8080
`
	dir := writeHelmChartFixture(t, valuesBody, "")
	payload, err := Parse(filepath.Join(dir, "values.yaml"), false, Options{})
	if err != nil {
		t.Fatalf("Parse(values.yaml) error = %v, want nil", err)
	}

	rows, ok := payload["helm_value_definitions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[helm_value_definitions] type = %T, want []map[string]any", payload["helm_value_definitions"])
	}

	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}

	for _, want := range []string{"replicaCount", "image.repository", "image.tag", "service.port"} {
		row, ok := byName[want]
		if !ok {
			t.Fatalf("missing helm_value_definitions row for %q; got names %v", want, namesOfHelmRows(rows))
		}
		if line := row["line_number"].(int); line <= 0 {
			t.Errorf("helm_value_definitions[%q].line_number = %d, want > 0", want, line)
		}
	}
	// Intermediate map keys (image, service) must NOT be emitted as definitions —
	// only leaves are referenceable values.
	if _, ok := byName["image"]; ok {
		t.Errorf("intermediate key %q should not be a value definition", "image")
	}
}

// TestParseHelmTemplateEmitsValueUsages proves a templates/*.yaml manifest under
// a chart is scanned for {{ .Values.<dotted.path> }} references, each becoming
// one helm_template_value_usages row carrying the dotted path and line.
func TestParseHelmTemplateEmitsValueUsages(t *testing.T) {
	t.Parallel()

	templateBody := `apiVersion: apps/v1
kind: Deployment
spec:
  replicas: {{ .Values.replicaCount }}
  template:
    spec:
      containers:
        - image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          ports:
            - containerPort: {{ .Values.service.port }}
`
	dir := writeHelmChartFixture(t, "replicaCount: 2\n", templateBody)
	payload, err := Parse(filepath.Join(dir, "templates", "deployment.yaml"), false, Options{})
	if err != nil {
		t.Fatalf("Parse(template) error = %v, want nil", err)
	}

	rows, ok := payload["helm_template_value_usages"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[helm_template_value_usages] type = %T, want []map[string]any", payload["helm_template_value_usages"])
	}

	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}

	for _, want := range []string{"replicaCount", "image.repository", "image.tag", "service.port"} {
		row, ok := byName[want]
		if !ok {
			t.Fatalf("missing helm_template_value_usages row for %q; got names %v", want, namesOfHelmRows(rows))
		}
		if line := row["line_number"].(int); line <= 0 {
			t.Errorf("helm_template_value_usages[%q].line_number = %d, want > 0", want, line)
		}
	}
}

// TestParseHelmTemplateValueUsagesDedup proves the same .Values path used twice
// is emitted once (first occurrence wins on line).
func TestParseHelmTemplateValueUsagesDedup(t *testing.T) {
	t.Parallel()

	templateBody := `env: {{ .Values.image.tag }}
label: {{ .Values.image.tag }}
`
	dir := writeHelmChartFixture(t, "image:\n  tag: x\n", templateBody)
	payload, err := Parse(filepath.Join(dir, "templates", "deployment.yaml"), false, Options{})
	if err != nil {
		t.Fatalf("Parse(template) error = %v, want nil", err)
	}
	rows := payload["helm_template_value_usages"].([]map[string]any)
	count := 0
	for _, row := range rows {
		if row["name"].(string) == "image.tag" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("image.tag usage count = %d, want 1 (deduped)", count)
	}
}

func namesOfHelmRows(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			out = append(out, name)
		}
	}
	return out
}
