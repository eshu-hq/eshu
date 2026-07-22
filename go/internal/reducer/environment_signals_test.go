// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestHelmValuesFilenameEnvironmentRecognizesKnownSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"hyphen-prod", "deploy/values-prod.yaml", "prod"},
		{"dot-stage", "charts/app/values.staging.yaml", "stage"},
		{"alias-production", "values-production.yml", "prod"},
		{"uppercase-extension-and-suffix", "values-PROD.YAML", "prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := helmValuesFilenameEnvironment(tt.path); got != tt.want {
				t.Fatalf("helmValuesFilenameEnvironment(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestHelmValuesFilenameEnvironmentRejectsUnknownOrNonEnvFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{"base-values-yaml", "charts/app/values.yaml"},
		{"base-values-yml", "charts/app/values.yml"},
		{"schema-file", "charts/app/values.schema.yaml"},
		{"example-file", "charts/app/values.example.yaml"},
		{"template-file", "charts/app/values.template.yaml"},
		// Ambiguous: superficially environment-looking but not an exact known
		// token -- must not be admitted (the values.schema.yaml defect class).
		{"trailing-notes-not-exact-token", "values-production-notes.yaml"},
		{"non-yaml-extension", "values-prod.txt"},
		{"unrelated-file", "src/main.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := helmValuesFilenameEnvironment(tt.path); got != "" {
				t.Fatalf("helmValuesFilenameEnvironment(%q) = %q, want \"\"", tt.path, got)
			}
		})
	}
}

func TestNamespaceEnvironmentRecognizesExactAndCompoundTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{"exact-token", "prod", "prod"},
		{"exact-alias", "production", "prod"},
		{"compound-hyphen-suffix", "myapp-prod", "prod"},
		{"compound-underscore-suffix", "myapp_staging", "stage"},
		{"compound-prefix", "prod-myapp", "prod"},
		{"mixed-case", "MyApp-Production", "prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := namespaceEnvironment(tt.namespace); got != tt.want {
				t.Fatalf("namespaceEnvironment(%q) = %q, want %q", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestNamespaceEnvironmentRejectsNonEnvironmentNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
	}{
		{"empty", ""},
		{"kube-system", "kube-system"},
		{"default", "default"},
		{"app-only-name", "my-app"},
		// Ambiguous: "product" contains "prod" as a substring but is not the
		// "prod" token -- token-boundary matching must not invent an
		// environment from a substring match.
		{"substring-false-positive", "product-catalog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := namespaceEnvironment(tt.namespace); got != "" {
				t.Fatalf("namespaceEnvironment(%q) = %q, want \"\"", tt.namespace, got)
			}
		})
	}
}

func TestCollectNamespaceEnvironmentsFromFileDataArgoCDAndKustomize(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"argocd_applications": []any{
			map[string]any{"name": "svc", "dest_namespace": "production"},
		},
		"argocd_applicationsets": []any{
			map[string]any{"name": "svc-set", "dest_namespace": "staging"},
		},
		"kustomize_overlays": []any{
			map[string]any{"name": "kustomization", "namespace": "prod"},
		},
	}

	got := collectNamespaceEnvironmentsFromFileData(fileData)
	want := map[string]bool{"prod": true, "stage": true}
	if len(got) == 0 {
		t.Fatalf("collectNamespaceEnvironmentsFromFileData() = empty, want %v", want)
	}
	seen := make(map[string]bool, len(got))
	for _, env := range got {
		seen[env] = true
	}
	for env := range want {
		if !seen[env] {
			t.Errorf("collectNamespaceEnvironmentsFromFileData() = %v, missing %q", got, env)
		}
	}
}

func TestCollectNamespaceEnvironmentsFromFileDataIgnoresNonEnvironmentNamespace(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"argocd_applications": []any{
			map[string]any{"name": "svc", "dest_namespace": "default"},
		},
		"kustomize_overlays": []any{
			map[string]any{"name": "kustomization", "namespace": "kube-system"},
		},
	}

	got := collectNamespaceEnvironmentsFromFileData(fileData)
	if len(got) != 0 {
		t.Fatalf("collectNamespaceEnvironmentsFromFileData() = %v, want empty", got)
	}
}

func TestCollectNamespaceEnvironmentsFromFileDataNilSafe(t *testing.T) {
	t.Parallel()

	if got := collectNamespaceEnvironmentsFromFileData(nil); got != nil {
		t.Fatalf("collectNamespaceEnvironmentsFromFileData(nil) = %v, want nil", got)
	}
}
