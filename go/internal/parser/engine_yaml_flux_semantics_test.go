// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathYAMLFluxKustomizationDoesNotMisrouteToOverlay is
// the issue #5342 misroute regression: a Flux CD Kustomization
// (apiVersion kustomize.toolkit.fluxcd.io/*) used to be captured by the
// generic Kustomize matcher's bare "kustomize" apiVersion prefix and routed
// to parseKustomization, which only reads top-level keys -- a Flux
// Kustomization nests everything under spec, so it produced a near-empty
// overlay and silently dropped spec.sourceRef/spec.path/spec.targetNamespace.
// This test proves the misroute no longer happens and the fields land as
// typed evidence in the dedicated flux_kustomizations bucket instead.
func TestDefaultEngineParsePathYAMLFluxKustomizationDoesNotMisrouteToOverlay(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "apps-kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: apps
  namespace: flux-system
spec:
  interval: 10m
  path: ./clusters/production/apps
  prune: true
  sourceRef:
    kind: GitRepository
    name: flux-system
    namespace: flux-system
  targetNamespace: production
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

	// Misroute regression: the Flux Kustomization group must never populate
	// the generic overlay bucket.
	assertEmptyNamedBucket(t, got, "kustomize_overlays")

	// Capture: the typed sourceRef/path/targetNamespace evidence lands in
	// its own bucket instead.
	assertNamedBucketContains(t, got, "flux_kustomizations", "apps")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_kind", "GitRepository")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_name", "flux-system")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_namespace", "flux-system")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_path", "./clusters/production/apps")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "target_namespace", "production")
}

// TestDefaultEngineParsePathYAMLFluxKustomizationFilenameVeto covers the
// critical edge case from issue #5342: a Flux Kustomization CR saved under
// the exact file name "kustomization.yaml" must still route to the Flux
// evidence path, not the generic overlay parser, even though the filename
// alone would normally trigger isKustomization's filename-only branch. An
// explicit foreign apiVersion vetoes that branch.
func TestDefaultEngineParsePathYAMLFluxKustomizationFilenameVeto(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: infra
  namespace: flux-system
spec:
  path: ./infra
  sourceRef:
    kind: GitRepository
    name: infra-repo
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

	assertEmptyNamedBucket(t, got, "kustomize_overlays")
	assertNamedBucketContains(t, got, "flux_kustomizations", "infra")
	assertBucketContainsFieldValue(t, got, "flux_kustomizations", "source_ref_name", "infra-repo")
}

// TestDefaultEngineParsePathYAMLKustomizeGenericGroupNonRegression is the
// exact-equivalence proof for issue #5342's dispatcher fix: every existing
// generic-Kustomize routing path (any kustomize.config.k8s.io version, and
// the no-apiVersion filename-only match) must still produce the identical
// kustomize_overlays output it did before the fix, and must never populate
// the new flux_kustomizations bucket.
func TestDefaultEngineParsePathYAMLKustomizeGenericGroupNonRegression(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
	}{
		{"generic v1beta1", "kustomize.config.k8s.io/v1beta1"},
		{"generic v1", "kustomize.config.k8s.io/v1"},
		{"no apiVersion (filename only)", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			filePath := filepath.Join(repoRoot, "kustomization.yaml")
			apiVersionLine := ""
			if tc.apiVersion != "" {
				apiVersionLine = "apiVersion: " + tc.apiVersion + "\n"
			}
			writeTestFile(
				t,
				filePath,
				apiVersionLine+`kind: Kustomization
namespace: production
resources:
  - ../base
  - ../app
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

			assertNamedBucketContains(t, got, "kustomize_overlays", "kustomization")
			assertBucketContainsFieldValue(t, got, "kustomize_overlays", "namespace", "production")
			overlays := got["kustomize_overlays"].([]map[string]any)
			if len(overlays) != 1 {
				t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
			}
			bases, ok := overlays[0]["bases"].([]string)
			if !ok || len(bases) != 2 || bases[0] != "../app" || bases[1] != "../base" {
				t.Fatalf("kustomize_overlays[0].bases = %#v, want [../app ../base]", overlays[0]["bases"])
			}
			assertEmptyNamedBucket(t, got, "flux_kustomizations")
		})
	}
}
