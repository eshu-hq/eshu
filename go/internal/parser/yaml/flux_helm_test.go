// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

func TestIsFluxHelmRelease(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		apiVersion string
		kind       string
		want       bool
	}{
		{"flux v2 helm release", "helm.toolkit.fluxcd.io/v2", "HelmRelease", true},
		{"flux v2beta2 helm release", "helm.toolkit.fluxcd.io/v2beta2", "HelmRelease", true},
		{"flux group wrong kind", "helm.toolkit.fluxcd.io/v2", "HelmRepository", false},
		{"generic group is not flux helm", "example.com/v1", "HelmRelease", false},
		{"empty apiVersion", "", "HelmRelease", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFluxHelmRelease(tc.apiVersion, tc.kind); got != tc.want {
				t.Fatalf("isFluxHelmRelease(%q, %q) = %v, want %v", tc.apiVersion, tc.kind, got, tc.want)
			}
		})
	}
}

// TestParseFluxHelmReleaseCapturesChartSourceRef proves the common case: a
// HelmRelease using spec.chart.spec.{chart,version,sourceRef} captures the
// chart name/version and the SAME three sourceRef keys FluxKustomization
// uses (source_ref_kind/name/namespace) -- never fabricated chart_ref_* keys
// when chartRef is absent.
func TestParseFluxHelmReleaseCapturesChartSourceRef(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"chart": map[string]any{
				"spec": map[string]any{
					"chart":   "podinfo",
					"version": "6.x",
					"sourceRef": map[string]any{
						"kind":      "HelmRepository",
						"name":      "podinfo",
						"namespace": "flux-system",
					},
				},
			},
			"targetNamespace": "production",
		},
	}
	metadata := map[string]any{
		"name":      "podinfo",
		"namespace": "flux-system",
	}

	row := parseFluxHelmRelease(document, metadata, "/repo/helmrelease.yaml", 3)

	if row["name"] != "podinfo" {
		t.Fatalf("name = %#v, want podinfo", row["name"])
	}
	if row["namespace"] != "flux-system" {
		t.Fatalf("namespace = %#v, want flux-system", row["namespace"])
	}
	if row["chart"] != "podinfo" {
		t.Fatalf("chart = %#v, want podinfo", row["chart"])
	}
	if row["chart_version"] != "6.x" {
		t.Fatalf("chart_version = %#v, want 6.x", row["chart_version"])
	}
	if row["source_ref_kind"] != "HelmRepository" {
		t.Fatalf("source_ref_kind = %#v, want HelmRepository", row["source_ref_kind"])
	}
	if row["source_ref_name"] != "podinfo" {
		t.Fatalf("source_ref_name = %#v, want podinfo", row["source_ref_name"])
	}
	if row["source_ref_namespace"] != "flux-system" {
		t.Fatalf("source_ref_namespace = %#v, want flux-system", row["source_ref_namespace"])
	}
	if row["target_namespace"] != "production" {
		t.Fatalf("target_namespace = %#v, want production", row["target_namespace"])
	}
	for _, key := range []string{"chart_ref_kind", "chart_ref_name", "chart_ref_namespace"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec.chartRef is absent (never fabricated)", key, row[key])
		}
	}
}

// TestParseFluxHelmReleaseCapturesChartRef proves the chartRef-based case:
// spec.chartRef.{kind,name,namespace} is captured under DISTINCT
// chart_ref_* keys, never folded into source_ref_* -- the two reference
// shapes are mutually exclusive per the Flux HelmRelease API and must never
// collide in the row.
func TestParseFluxHelmReleaseCapturesChartRef(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"chartRef": map[string]any{
				"kind":      "OCIRepository",
				"name":      "podinfo-oci",
				"namespace": "flux-system",
			},
		},
	}
	metadata := map[string]any{
		"name":      "podinfo",
		"namespace": "flux-system",
	}

	row := parseFluxHelmRelease(document, metadata, "/repo/helmrelease-chartref.yaml", 3)

	if row["chart_ref_kind"] != "OCIRepository" {
		t.Fatalf("chart_ref_kind = %#v, want OCIRepository", row["chart_ref_kind"])
	}
	if row["chart_ref_name"] != "podinfo-oci" {
		t.Fatalf("chart_ref_name = %#v, want podinfo-oci", row["chart_ref_name"])
	}
	if row["chart_ref_namespace"] != "flux-system" {
		t.Fatalf("chart_ref_namespace = %#v, want flux-system", row["chart_ref_namespace"])
	}
	for _, key := range []string{"chart", "chart_version", "source_ref_kind", "source_ref_name", "source_ref_namespace"} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec.chart is absent (never fabricated)", key, row[key])
		}
	}
}

// TestParseFluxHelmReleaseOmitsAbsentFields proves a bare HelmRelease with
// neither spec.chart nor spec.chartRef (an invalid CR, but the parser never
// validates) omits every chart/sourceRef/chartRef field rather than
// fabricating one.
func TestParseFluxHelmReleaseOmitsAbsentFields(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{"name": "bare"}

	row := parseFluxHelmRelease(document, metadata, "/repo/bare.yaml", 1)

	for _, key := range []string{
		"chart", "chart_version", "source_ref_kind", "source_ref_name", "source_ref_namespace",
		"chart_ref_kind", "chart_ref_name", "chart_ref_namespace", "target_namespace", "namespace",
	} {
		if _, present := row[key]; present {
			t.Fatalf("row[%q] = %#v, want absent when spec has no matching field", key, row[key])
		}
	}
}

// TestParseFluxHelmReleaseGenerateNameOnly mirrors the FluxKustomization/Flux
// source-CR generateName invariant: an empty name is never fabricated as
// "<nil>", and the literal metadata.generateName is captured as evidence.
func TestParseFluxHelmReleaseGenerateNameOnly(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{"generateName": "podinfo-"}

	row := parseFluxHelmRelease(document, metadata, "/repo/gen.yaml", 1)

	if name, ok := row["name"]; !ok || name != "" {
		t.Fatalf("name = %#v (present=%v), want empty string, never \"<nil>\"", row["name"], ok)
	}
	if row["generate_name"] != "podinfo-" {
		t.Fatalf("generate_name = %#v, want podinfo-", row["generate_name"])
	}
}

// TestParseFluxHelmReleaseWhollyNamelessOmitsGenerateName mirrors the
// sibling omit-when-absent invariant for generate_name.
func TestParseFluxHelmReleaseWhollyNamelessOmitsGenerateName(t *testing.T) {
	t.Parallel()

	document := map[string]any{"spec": map[string]any{}}
	metadata := map[string]any{}

	row := parseFluxHelmRelease(document, metadata, "/repo/nameless.yaml", 1)

	if name, ok := row["name"]; !ok || name != "" {
		t.Fatalf("name = %#v (present=%v), want empty string, never \"<nil>\"", row["name"], ok)
	}
	if _, present := row["generate_name"]; present {
		t.Fatalf("generate_name = %#v, want absent when metadata.generateName is absent", row["generate_name"])
	}
}

// TestParseFluxHelmReleaseBothChartAndChartRefSetCapturesBoth proves the
// parser is a pure, non-validating capture: when a manifest author sets BOTH
// spec.chart and spec.chartRef (an invalid CR per the Flux API, exactly one
// of the two must be set), the parser still captures both sets of fields
// verbatim. The exactly-one-of validation and honest non-link decision
// belongs entirely to the edge resolver
// (go/internal/storage/cypher/canonical_flux_helm_edges.go), never here.
func TestParseFluxHelmReleaseBothChartAndChartRefSetCapturesBoth(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"spec": map[string]any{
			"chart": map[string]any{
				"spec": map[string]any{
					"chart": "podinfo",
					"sourceRef": map[string]any{
						"kind": "HelmRepository",
						"name": "podinfo",
					},
				},
			},
			"chartRef": map[string]any{
				"kind": "OCIRepository",
				"name": "podinfo-oci",
			},
		},
	}
	metadata := map[string]any{"name": "podinfo-invalid"}

	row := parseFluxHelmRelease(document, metadata, "/repo/both.yaml", 1)

	if row["chart"] != "podinfo" {
		t.Fatalf("chart = %#v, want podinfo (verbatim capture)", row["chart"])
	}
	if row["chart_ref_kind"] != "OCIRepository" {
		t.Fatalf("chart_ref_kind = %#v, want OCIRepository (verbatim capture)", row["chart_ref_kind"])
	}
}
