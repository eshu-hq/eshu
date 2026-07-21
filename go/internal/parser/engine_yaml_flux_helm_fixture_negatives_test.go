// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Fixture-intent coverage for the FluxHelmRelease/FluxHelmRepository
// scenarios the RECONCILES_FROM correlation edge extension (issue #5483 C1)
// must handle honestly. These prove the PARSER side of the contract: chart
// and chartRef fields are captured verbatim regardless of whether they
// resolve to anything (dangling), whether the chartRef names a kind the edge
// resolver deliberately never links (HelmChart), or whether the manifest sets
// both chart AND chartRef (invalid per the Flux API, but the parser never
// validates) -- the resolution/skip judgment belongs entirely to the Go-side
// edge builder (go/internal/storage/cypher/canonical_flux_helm_edges_test.go
// covers the T1-T4 tiers and every skip case against these same field
// shapes).
//
// Wires the new fixtures in tests/fixtures/ecosystems/flux_comprehensive/
// into an executable proof, mirroring
// engine_yaml_flux_fixture_negatives_test.go's fixtureDir/ParsePath pattern.
package parser

import "testing"

// TestFluxComprehensiveFixtureHelmReleaseChartSourceRefCapturedVerbatim
// proves the common case: chart.spec.{chart,version,sourceRef} is captured
// with the sourceRef fields under the same source_ref_* keys
// FluxKustomization uses.
func TestFluxComprehensiveFixtureHelmReleaseChartSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_version", "6.x")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "source_ref_kind", "HelmRepository")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "target_namespace", "production")
}

// TestFluxComprehensiveFixtureHelmReleaseChartRefCapturedVerbatim proves the
// chartRef case: spec.chartRef.{kind,name,namespace} is captured under the
// distinct chart_ref_* keys.
func TestFluxComprehensiveFixtureHelmReleaseChartRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease-chartref.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo-oci")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_ref_kind", "OCIRepository")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_ref_name", "podinfo-oci")
}

// TestFluxComprehensiveFixtureHelmReleaseDanglingSourceRefCapturedVerbatim
// proves a sourceRef naming a HelmRepository absent from the fixture repo
// still captures source_ref_kind/name/namespace honestly -- the parser makes
// no existence judgment.
func TestFluxComprehensiveFixtureHelmReleaseDanglingSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease-dangling-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "orphaned-release")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "source_ref_name", "does-not-exist")
}

// TestFluxComprehensiveFixtureHelmReleaseChartRefHelmChartKindCapturedVerbatim
// proves chartRef.kind: HelmChart is still captured as the literal string the
// manifest declares -- the parser never judges whether a chartRef kind
// resolves to anything; the edge resolver treats HelmChart as a deliberate
// honest non-link (fluxHelmChartRefKindToLabel).
func TestFluxComprehensiveFixtureHelmReleaseChartRefHelmChartKindCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease-chartref-helmchart.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo-helmchart-ref")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_ref_kind", "HelmChart")
}

// TestFluxComprehensiveFixtureHelmReleaseBothChartAndChartRefCapturedVerbatim
// proves a manifest setting BOTH spec.chart and spec.chartRef (invalid per
// the Flux API) still captures both sets of fields -- the parser is a pure,
// non-validating capture; the exactly-one-of rule and its honest non-link
// decision belong entirely to the edge resolver.
func TestFluxComprehensiveFixtureHelmReleaseBothChartAndChartRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease-both-chart-and-chartref.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo-invalid-both-refs")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "chart_ref_kind", "OCIRepository")
}

// TestFluxComprehensiveFixtureHelmReleaseCrossNamespaceSourceRefCapturedVerbatim
// proves a declared sourceRef.namespace that matches no candidate in the
// fixture repo is still captured verbatim (never validated by the parser).
func TestFluxComprehensiveFixtureHelmReleaseCrossNamespaceSourceRefCapturedVerbatim(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrelease-cross-namespace-sourceref.yaml")
	assertNamedBucketContains(t, got, "flux_helm_releases", "podinfo-cross-namespace")
	assertBucketContainsFieldValue(t, got, "flux_helm_releases", "source_ref_namespace", "other-namespace")
}

// TestFluxComprehensiveFixtureHelmRepositoryCapturesURLAndType proves the
// basic HelmRepository capture: spec.url and spec.type (as repo_type).
func TestFluxComprehensiveFixtureHelmRepositoryCapturesURLAndType(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrepository.yaml")
	assertNamedBucketContains(t, got, "flux_helm_repositories", "podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_repositories", "url", "https://stefanprodan.github.io/podinfo")
	assertBucketContainsFieldValue(t, got, "flux_helm_repositories", "repo_type", "default")
}

// TestFluxComprehensiveFixtureHelmRepositoryGenerateNameHasEmptyName mirrors
// the sibling Flux source-CR generateName invariant: a HelmRepository using
// metadata.generateName has an empty "name" (never fabricated "<nil>") and
// carries the literal generate_name evidence field.
func TestFluxComprehensiveFixtureHelmRepositoryGenerateNameHasEmptyName(t *testing.T) {
	t.Parallel()

	got := parseFluxFixtureFile(t, "helmrepository-generatename.yaml")
	repos, ok := got["flux_helm_repositories"].([]map[string]any)
	if !ok || len(repos) != 1 {
		t.Fatalf("flux_helm_repositories = %#v, want exactly one row", got["flux_helm_repositories"])
	}
	if name, present := repos[0]["name"]; !present || name != "" {
		t.Fatalf("name = %#v (present=%v), want empty string, never \"<nil>\"", repos[0]["name"], present)
	}
	if generateName, _ := repos[0]["generate_name"].(string); generateName != "ephemeral-podinfo-" {
		t.Fatalf("generate_name = %#v, want ephemeral-podinfo-", repos[0]["generate_name"])
	}
}

// TestFluxComprehensiveFixtureHelmRepositoryDuplicatesAndAmbiguousConsumerCapturedVerbatim
// proves the parser-side shape of the "ambiguous duplicate" scenario: two
// HelmRepository CRs sharing (name, namespace) in different files, and a
// HelmRelease whose sourceRef names them, all captured as distinct rows --
// the T2 tie-or-disambiguate judgment is proven at the resolver level
// (canonical_flux_helm_edges_test.go), not here.
func TestFluxComprehensiveFixtureHelmRepositoryDuplicatesAndAmbiguousConsumerCapturedVerbatim(t *testing.T) {
	t.Parallel()

	dupA := parseFluxFixtureFile(t, "helmrepository-duplicate-a.yaml")
	assertBucketContainsFieldValue(t, dupA, "flux_helm_repositories", "url", "https://a.example.com/charts")

	dupB := parseFluxFixtureFile(t, "helmrepository-duplicate-b.yaml")
	assertBucketContainsFieldValue(t, dupB, "flux_helm_repositories", "url", "https://b.example.com/charts")

	consumer := parseFluxFixtureFile(t, "helmrelease-ambiguous-duplicate.yaml")
	assertBucketContainsFieldValue(t, consumer, "flux_helm_releases", "source_ref_name", "shared-repo")
}
