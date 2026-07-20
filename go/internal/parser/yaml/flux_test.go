// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

func TestIsKustomizationGenericGroupAndFilenameOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		filename   string
		want       bool
	}{
		{"generic v1beta1 any filename", "kustomize.config.k8s.io/v1beta1", "overlay.yaml", true},
		{"generic v1 any filename", "kustomize.config.k8s.io/v1", "overlay.yaml", true},
		{"no apiVersion kustomization.yaml", "", "kustomization.yaml", true},
		{"no apiVersion kustomization.yml", "", "kustomization.yml", true},
		{"no apiVersion unrelated filename", "", "deployment.yaml", false},
		{
			name:       "flux apiVersion vetoes filename-only match",
			apiVersion: "kustomize.toolkit.fluxcd.io/v1",
			filename:   "kustomization.yaml",
			want:       false,
		},
		{
			name:       "unrelated foreign apiVersion vetoes filename-only match",
			apiVersion: "argoproj.io/v1alpha1",
			filename:   "kustomization.yaml",
			want:       false,
		},
		{
			// Pinned intended behavior: a real Kubernetes apiVersion always
			// carries a "/version" segment, so this shape never occurs from
			// a genuine manifest. The exact-prefix generic-group check (with
			// a trailing "/") does not match a version-less apiVersion, and
			// the veto below routes it to the generic k8s_resources
			// fallthrough rather than kustomize_overlays.
			name:       "version-less generic group apiVersion routes away from kustomize overlays",
			apiVersion: "kustomize.config.k8s.io",
			filename:   "kustomization.yaml",
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isKustomization(tc.apiVersion, tc.filename); got != tc.want {
				t.Fatalf("isKustomization(%q, %q) = %v, want %v", tc.apiVersion, tc.filename, got, tc.want)
			}
		})
	}
}

func TestIsFluxKustomization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{"flux v1 kustomization", "kustomize.toolkit.fluxcd.io/v1", "Kustomization", true},
		{"flux v1beta2 kustomization", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization", true},
		{"flux group wrong kind", "kustomize.toolkit.fluxcd.io/v1", "HelmRelease", false},
		{"generic kustomize group is not flux", "kustomize.config.k8s.io/v1beta1", "Kustomization", false},
		{"empty apiVersion", "", "Kustomization", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFluxKustomization(tc.apiVersion, tc.kind); got != tc.want {
				t.Fatalf("isFluxKustomization(%q, %q) = %v, want %v", tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}

func TestParseFluxKustomizationCapturesSourceRefAndOmitsAbsentFields(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"sourceRef": map[string]any{
				"kind": "GitRepository",
				"name": "flux-system",
				// namespace intentionally absent.
			},
			"path": "clusters/production",
			// targetNamespace intentionally absent.
		},
	}
	metadata := map[string]any{
		"name":      "apps",
		"namespace": "flux-system",
	}

	row := parseFluxKustomization(document, metadata, "/repo/apps-kustomization.yaml", 3)

	if row["name"] != "apps" {
		t.Fatalf("name = %#v, want apps", row["name"])
	}
	if row["namespace"] != "flux-system" {
		t.Fatalf("namespace = %#v, want flux-system", row["namespace"])
	}
	if row["path"] != "/repo/apps-kustomization.yaml" {
		t.Fatalf("path = %#v, want the file path", row["path"])
	}
	if row["source_ref_kind"] != "GitRepository" {
		t.Fatalf("source_ref_kind = %#v, want GitRepository", row["source_ref_kind"])
	}
	if row["source_ref_name"] != "flux-system" {
		t.Fatalf("source_ref_name = %#v, want flux-system", row["source_ref_name"])
	}
	if _, present := row["source_ref_namespace"]; present {
		t.Fatalf("source_ref_namespace = %#v, want absent (not fabricated)", row["source_ref_namespace"])
	}
	if row["source_path"] != "clusters/production" {
		t.Fatalf("source_path = %#v, want clusters/production", row["source_path"])
	}
	if _, present := row["target_namespace"]; present {
		t.Fatalf("target_namespace = %#v, want absent (not fabricated)", row["target_namespace"])
	}
}

func TestParseFluxKustomizationOmitsSourceRefWhenAbsent(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{},
	}
	metadata := map[string]any{"name": "bare"}

	row := parseFluxKustomization(document, metadata, "/repo/bare.yaml", 1)

	for _, key := range []string{"source_ref_kind", "source_ref_name", "source_ref_namespace", "source_path", "target_namespace"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec has no matching field", key, row[key])
		}
	}
	// metadata.namespace is absent here too: it must be OMITTED, never
	// fabricated as "<nil>" (fmt.Sprint(nil)) or an empty string.
	if _, present := row["namespace"]; present {
		t.Fatalf("namespace = %#v, want absent when metadata has no namespace (never fabricated)", row["namespace"])
	}
}
