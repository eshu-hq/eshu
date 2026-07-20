// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// Kustomize-focused engine YAML tests split out of engine_yaml_semantics_test.go
// to keep that file under the repo's 500-line package-file cap (issue #5440,
// following the same split precedent as engine_yaml_semantics_crossplane_test.go,
// issue #5347).

func TestDefaultEngineParsePathYAMLKustomizeAndHelm(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	kustomizePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		kustomizePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: production
resources:
  - ../base
  - ../app
patches:
  - path: patches/replicas.yaml
`,
	)

	chartPath := filepath.Join(repoRoot, "Chart.yaml")
	writeTestFile(
		t,
		chartPath,
		`name: my-api-chart
version: 0.1.0
appVersion: 1.0.0
dependencies:
  - name: redis
    repository: https://charts.example.test/redis
`,
	)

	valuesPath := filepath.Join(repoRoot, "values.yaml")
	writeTestFile(
		t,
		valuesPath,
		`replicaCount: 2
image:
  repository: ghcr.io/example/app
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	kustomizePayload, err := engine.ParsePath(repoRoot, kustomizePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", kustomizePath, err)
	}
	assertNamedBucketContains(t, kustomizePayload, "kustomize_overlays", "kustomization")
	assertBucketContainsFieldValue(t, kustomizePayload, "kustomize_overlays", "namespace", "production")
	kustomizeOverlays := kustomizePayload["kustomize_overlays"].([]map[string]any)
	if len(kustomizeOverlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", kustomizeOverlays)
	}
	bases, ok := kustomizeOverlays[0]["bases"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].bases = %T, want []string", kustomizeOverlays[0]["bases"])
	}
	if len(bases) != 2 || bases[0] != "../app" || bases[1] != "../base" {
		t.Fatalf("kustomize_overlays[0].bases = %#v, want [../app ../base]", bases)
	}
	chartPayload, err := engine.ParsePath(repoRoot, chartPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", chartPath, err)
	}
	assertNamedBucketContains(t, chartPayload, "helm_charts", "my-api-chart")
	assertBucketContainsFieldValue(t, chartPayload, "helm_charts", "dependencies", "redis")
	assertBucketContainsFieldValue(t, chartPayload, "helm_charts", "dependency_repositories", "https://charts.example.test/redis")

	valuesPayload, err := engine.ParsePath(repoRoot, valuesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", valuesPath, err)
	}
	assertNamedBucketContains(t, valuesPayload, "helm_values", "values")
	assertBucketContainsFieldValue(t, valuesPayload, "helm_values", "image_repositories", "ghcr.io/example/app")
	assertBucketContainsFieldValue(t, valuesPayload, "helm_values", "top_level_keys", "image,replicaCount")
}

func TestDefaultEngineParsePathYAMLKustomizePatchTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
patches:
  - target:
      kind: Deployment
      name: comprehensive-app
    patch: |-
      - op: replace
        path: /spec/replicas
        value: 1
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	overlays := payload["kustomize_overlays"].([]map[string]any)
	if len(overlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
	}
	patchTargets, ok := overlays[0]["patch_targets"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].patch_targets = %T, want []string", overlays[0]["patch_targets"])
	}
	if len(patchTargets) != 1 || patchTargets[0] != "Deployment/comprehensive-app" {
		t.Fatalf("kustomize_overlays[0].patch_targets = %#v, want [Deployment/comprehensive-app]", patchTargets)
	}
}

func TestDefaultEngineParsePathYAMLKustomizeTypedDeployReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../base
  - https://github.com/myorg/shared-manifests.git//payments?ref=main
components:
  - shared/component
helmCharts:
  - name: nginx
    repo: https://charts.bitnami.com/bitnami
    releaseName: ingress-nginx
images:
  - name: nginx
    newName: ghcr.io/example/nginx
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	overlays := payload["kustomize_overlays"].([]map[string]any)
	if len(overlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
	}

	resourceRefs, ok := overlays[0]["resource_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].resource_refs = %T, want []string", overlays[0]["resource_refs"])
	}
	if len(resourceRefs) != 2 || resourceRefs[0] != "https://github.com/myorg/shared-manifests.git//payments?ref=main" || resourceRefs[1] != "shared/component" {
		t.Fatalf("kustomize_overlays[0].resource_refs = %#v, want [https://github.com/myorg/shared-manifests.git//payments?ref=main shared/component]", resourceRefs)
	}

	helmRefs, ok := overlays[0]["helm_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].helm_refs = %T, want []string", overlays[0]["helm_refs"])
	}
	if len(helmRefs) != 3 || helmRefs[0] != "https://charts.bitnami.com/bitnami" || helmRefs[1] != "ingress-nginx" || helmRefs[2] != "nginx" {
		t.Fatalf("kustomize_overlays[0].helm_refs = %#v, want [https://charts.bitnami.com/bitnami ingress-nginx nginx]", helmRefs)
	}

	imageRefs, ok := overlays[0]["image_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].image_refs = %T, want []string", overlays[0]["image_refs"])
	}
	if len(imageRefs) != 2 || imageRefs[0] != "ghcr.io/example/nginx" || imageRefs[1] != "nginx" {
		t.Fatalf("kustomize_overlays[0].image_refs = %#v, want [ghcr.io/example/nginx nginx]", imageRefs)
	}
}

// TestDefaultEngineParsePathYAMLKustomizeImageOverrides pins the
// image_overrides row shape a kustomization.yaml images[] list produces
// (issue #5440): the newTag/digest version truth that
// kustomize_overlays[].image_refs (collectKustomizeObjectRefs,
// kustomize_semantics.go) never reads. It also regression-guards image_refs
// itself, which must stay exactly what it was before image_overrides
// existed.
func TestDefaultEngineParsePathYAMLKustomizeImageOverrides(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: nginx
    newName: ghcr.io/example/nginx
    newTag: "1.25.3"
  - name: sidecar
    newName: ghcr.io/example/envoy
    digest: sha256:abc123def456
  - name: unpatched-app
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	// Regression guard: image_refs (flattened name+newName) must stay exactly
	// what it was before image_overrides existed.
	overlays := payload["kustomize_overlays"].([]map[string]any)
	if len(overlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
	}
	imageRefs, ok := overlays[0]["image_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].image_refs = %T, want []string", overlays[0]["image_refs"])
	}
	wantRefs := []string{"ghcr.io/example/envoy", "ghcr.io/example/nginx", "nginx", "sidecar", "unpatched-app"}
	if len(imageRefs) != len(wantRefs) {
		t.Fatalf("kustomize_overlays[0].image_refs = %#v, want %#v", imageRefs, wantRefs)
	}
	for i, want := range wantRefs {
		if imageRefs[i] != want {
			t.Fatalf("kustomize_overlays[0].image_refs = %#v, want %#v", imageRefs, wantRefs)
		}
	}

	overrides, ok := payload["image_overrides"].([]map[string]any)
	if !ok {
		t.Fatalf("image_overrides = %T, want []map[string]any", payload["image_overrides"])
	}
	// 2, not 3: "unpatched-app" declares no newName/newTag/digest -- a no-op
	// match-target entry with no version override -- and is skipped (issue
	// #5440 review P2). See TestDefaultEngineParsePathYAMLKustomizeImageOverridesNoOpEntry
	// below for the dedicated regression guard.
	if len(overrides) != 2 {
		t.Fatalf("len(image_overrides) = %d, want 2: %#v", len(overrides), overrides)
	}

	nginx := findNamedItem(t, payload, "image_overrides", "nginx")
	if nginx["repository"] != "ghcr.io/example/nginx" || nginx["tag"] != "1.25.3" || nginx["digest"] != "" {
		t.Fatalf("nginx override = %#v", nginx)
	}
	if nginx["source"] != "kustomize" || nginx["path"] != filePath || nginx["lang"] != "yaml" {
		t.Fatalf("nginx override provenance = %#v", nginx)
	}

	sidecar := findNamedItem(t, payload, "image_overrides", "sidecar")
	if sidecar["repository"] != "ghcr.io/example/envoy" || sidecar["digest"] != "sha256:abc123def456" || sidecar["tag"] != "" {
		t.Fatalf("sidecar override = %#v", sidecar)
	}

	for _, row := range overrides {
		if row["name"] == "unpatched-app" {
			t.Fatalf("image_overrides contains a row for the no-op match-target entry \"unpatched-app\": %#v", row)
		}
	}
}

// TestDefaultEngineParsePathYAMLKustomizeImageOverridesNoOpEntry is a
// dedicated regression guard for issue #5440 review P2: a Kustomize
// images[] entry with only `name` and none of `newName`/`newTag`/`digest`
// declares NO version override -- Kustomize itself performs no image
// substitution for it. Emitting a row for it would tell a consumer "yes, a
// version override is declared for this image" when the honest answer is
// "no version was declared." An entry with only `newTag` set (no newName,
// no digest) still IS a real override and must still emit a row.
func TestDefaultEngineParsePathYAMLKustomizeImageOverridesNoOpEntry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: name-only-no-op
  - name: tag-only-override
    newTag: "2.0.0"
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	overrides, ok := payload["image_overrides"].([]map[string]any)
	if !ok {
		t.Fatalf("image_overrides = %T, want []map[string]any", payload["image_overrides"])
	}
	if len(overrides) != 1 {
		t.Fatalf("len(image_overrides) = %d, want 1 (only the newTag entry declares an override): %#v", len(overrides), overrides)
	}

	for _, row := range overrides {
		if row["name"] == "name-only-no-op" {
			t.Fatalf("image_overrides contains a row for the name-only no-op entry: %#v", row)
		}
	}

	tagOnly := findNamedItem(t, payload, "image_overrides", "tag-only-override")
	if tagOnly["repository"] != "tag-only-override" || tagOnly["tag"] != "2.0.0" || tagOnly["digest"] != "" {
		t.Fatalf("tag-only-override = %#v, want repository=tag-only-override tag=2.0.0 digest=\"\"", tagOnly)
	}
}
