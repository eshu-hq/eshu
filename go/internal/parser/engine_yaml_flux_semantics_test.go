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

// TestDefaultEngineParsePathYAMLFluxSourceReposGenerateNameStayDistinct is the
// #5360 PR A codex P2 identity proof: two generateName-only GitRepository docs
// in one multi-document file must produce TWO distinct flux_git_repositories
// rows, each with an empty name (never fabricated "<nil>") and its own
// generate_name, distinguished by the distinct document start line the `---`
// separator forces. This is the identity boundary the constructor doc comments
// rely on: (repo_id, path, label, name="", start_line) is unique because two
// same-label entities cannot share a start line in one file.
func TestDefaultEngineParsePathYAMLFluxSourceReposGenerateNameStayDistinct(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "sources.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  generateName: repo-a-
  namespace: flux-system
spec:
  url: https://github.com/acme/repo-a
  ref:
    branch: main
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  generateName: repo-b-
  namespace: flux-system
spec:
  url: https://github.com/acme/repo-b
  ref:
    branch: release
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

	repos, ok := got["flux_git_repositories"].([]map[string]any)
	if !ok {
		t.Fatalf("flux_git_repositories = %T, want []map[string]any", got["flux_git_repositories"])
	}
	if len(repos) != 2 {
		t.Fatalf("flux_git_repositories count = %d, want 2 distinct entities from two generateName docs; rows=%#v", len(repos), repos)
	}

	seenLines := make(map[int]struct{}, len(repos))
	seenGenerateNames := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if name, ok := repo["name"]; !ok || name != "" {
			t.Fatalf("name = %#v (present=%v), want empty string, never \"<nil>\"", repo["name"], ok)
		}
		line, _ := repo["line_number"].(int)
		if line <= 0 {
			t.Fatalf("line_number = %#v, want a positive document start line", repo["line_number"])
		}
		if _, dup := seenLines[line]; dup {
			t.Fatalf("two flux_git_repositories share line_number %d; the --- separator must force distinct document start lines", line)
		}
		seenLines[line] = struct{}{}
		generateName, _ := repo["generate_name"].(string)
		if generateName == "" {
			t.Fatalf("generate_name empty for a generateName doc; row=%#v", repo)
		}
		seenGenerateNames[generateName] = struct{}{}
	}
	for _, want := range []string{"repo-a-", "repo-b-"} {
		if _, ok := seenGenerateNames[want]; !ok {
			t.Fatalf("generate_name %q missing; got %#v", want, seenGenerateNames)
		}
	}
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

// TestDefaultEngineParsePathYAMLFluxHelmReleaseDoesNotMisrouteToK8sResource is
// the issue #5483 C1 sibling of the Kustomization misroute regression: a Flux
// HelmRelease (apiVersion helm.toolkit.fluxcd.io/*) must never fall through to
// the generic k8s_resources bucket (which drops every nested spec field except
// a handful of well-known ones); it must land as typed evidence in the
// dedicated flux_helm_releases bucket instead.
func TestDefaultEngineParsePathYAMLFluxHelmReleaseDoesNotMisrouteToK8sResource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "helmrelease.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: flux-system
spec:
  chart:
    spec:
      chart: podinfo
      version: 6.x
      sourceRef:
        kind: HelmRepository
        name: podinfo
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

	assertEmptyNamedBucket(t, got, "k8s_resources")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_version", "6.x")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "source_ref_kind", "HelmRepository")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "source_ref_name", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "target_namespace", "production")
}

// TestDefaultEngineParsePathYAMLFluxHelmRepositoryDoesNotMisrouteToK8sResource
// is the FluxHelmRepository sibling: apiVersion source.toolkit.fluxcd.io/*
// kind HelmRepository must land in flux_helm_repositories, never
// k8s_resources.
func TestDefaultEngineParsePathYAMLFluxHelmRepositoryDoesNotMisrouteToK8sResource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "helmrepository.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  url: https://stefanprodan.github.io/podinfo
  type: default
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

	assertEmptyNamedBucket(t, got, "k8s_resources")
	assertNamedBucketContains(t, got, "flux_helm_repositories", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_repositories", "url", "https://stefanprodan.github.io/podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_repositories", "repo_type", "default")
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
