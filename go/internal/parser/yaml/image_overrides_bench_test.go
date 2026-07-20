// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRepresentativeHelmValues returns a Helm values.yaml body with the
// given number of nested "image:" blocks (a mix of sibling tag/digest keys
// and inline "repo:tag"/"repo@sha256:..." forms), plus surrounding non-image
// keys, so the walk collectHelmImageOverrides/collectHelmImageRepositories
// perform over this fixture matches a real chart's values shape rather than a
// single minimal case. Issue #5440 Performance Evidence: this same benchmark
// body runs unchanged on origin/main (no image_overrides bucket) and on this
// branch (image_overrides added alongside the existing image_repositories
// walk), so the two runs isolate the new bucket's cost.
func buildRepresentativeHelmValues(images int) string {
	var b strings.Builder
	b.WriteString("replicaCount: 2\n")
	b.WriteString("service:\n  type: ClusterIP\n  port: 8080\n")
	for i := range images {
		fmt.Fprintf(&b, "component%02d:\n", i)
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, "  image:\n    repository: ghcr.io/example/service-%02d\n    tag: \"1.%d.0\"\n", i, i)
		case 1:
			fmt.Fprintf(&b, "  image:\n    repository: ghcr.io/example/service-%02d:v1.%d.0\n", i, i)
		default:
			fmt.Fprintf(&b, "  image:\n    repository: ghcr.io/example/service-%02d@sha256:%064d\n", i, i)
		}
		fmt.Fprintf(&b, "  resources:\n    limits:\n      cpu: 500m\n      memory: 512Mi\n")
	}
	return b.String()
}

// buildRepresentativeKustomization returns a kustomization.yaml body with the
// given number of images[] entries (a mix of newName/newTag/digest
// combinations), plus resources/patches, matching the shape
// collectKustomizeImageOverrides/collectKustomizeObjectRefs both walk. See
// buildRepresentativeHelmValues for the Performance Evidence rationale.
func buildRepresentativeKustomization(images int) string {
	var b strings.Builder
	b.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\n")
	b.WriteString("kind: Kustomization\n")
	b.WriteString("resources:\n  - ../base\n")
	b.WriteString("images:\n")
	for i := range images {
		fmt.Fprintf(&b, "  - name: service-%02d\n", i)
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, "    newName: ghcr.io/example/service-%02d\n    newTag: \"1.%d.0\"\n", i, i)
		case 1:
			fmt.Fprintf(&b, "    newName: ghcr.io/example/service-%02d\n    digest: sha256:%064d\n", i, i)
		default:
			// name-only entry: no override declared.
		}
	}
	return b.String()
}

// BenchmarkParseHelmValuesRepresentativeImages measures Parse() over a
// values.yaml with 20 nested "image:" blocks -- issue #5440 Performance
// Evidence for the image_overrides Helm producer added alongside the
// existing image_repositories walk.
func BenchmarkParseHelmValuesRepresentativeImages(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "values.yaml")
	source := buildRepresentativeHelmValues(20)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write values.yaml: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if got := len(payload["helm_values"].([]map[string]any)); got != 1 {
			b.Fatalf("helm_values rows = %d, want 1", got)
		}
	}
}

// BenchmarkParseKustomizationRepresentativeImages measures Parse() over a
// kustomization.yaml with 20 images[] entries -- issue #5440 Performance
// Evidence for the image_overrides Kustomize producer added alongside the
// existing image_refs collectKustomizeObjectRefs walk.
func BenchmarkParseKustomizationRepresentativeImages(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "kustomization.yaml")
	source := buildRepresentativeKustomization(20)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write kustomization.yaml: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if got := len(payload["kustomize_overlays"].([]map[string]any)); got != 1 {
			b.Fatalf("kustomize_overlays rows = %d, want 1", got)
		}
	}
}

// BenchmarkParseHelmValuesLargeImages measures Parse() over a values.yaml
// with 200 nested "image:" blocks -- a worst-case-partition fixture (issue
// #5440 review) proving dedupeImageOverrideRows's O(n^2) linear-scan dedupe
// (image_overrides.go) does not blow up superlinearly at 10x the
// representative fixture's image count. Every image in this fixture is
// distinct (no real duplicates), so the scan always runs its full worst-case
// length per row.
func BenchmarkParseHelmValuesLargeImages(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "values.yaml")
	source := buildRepresentativeHelmValues(200)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write values.yaml: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if got := len(payload["helm_values"].([]map[string]any)); got != 1 {
			b.Fatalf("helm_values rows = %d, want 1", got)
		}
	}
}

// BenchmarkParseKustomizationLargeImages is BenchmarkParseKustomizationRepresentativeImages
// at 200 images[] entries -- the same worst-case-partition proof as
// BenchmarkParseHelmValuesLargeImages, for the Kustomize producer.
func BenchmarkParseKustomizationLargeImages(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "kustomization.yaml")
	source := buildRepresentativeKustomization(200)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write kustomization.yaml: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if got := len(payload["kustomize_overlays"].([]map[string]any)); got != 1 {
			b.Fatalf("kustomize_overlays rows = %d, want 1", got)
		}
	}
}
