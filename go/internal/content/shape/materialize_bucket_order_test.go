// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

// TestContentEntityBucketsFluxTypedEntitiesAppendedAtEnd guards the
// frozen-table invariant documented in AGENTS.md: a newly added entity bucket
// MUST be appended to the end of contentEntityBuckets, never inserted mid-table.
// Inserting before the SQL/data/impl_blocks/pagerduty/gitlab/helm/sql_migrations
// buckets would shift the persisted entity row order for every later label. The
// Flux typed entities (issue #5360 PR A, extended with FluxHelmRelease/
// FluxHelmRepository by issue #5483 C1) are the most recent addition, so they
// must be the trailing six entries, with sql_migrations (SqlMigration, #5346)
// immediately before them and the Helm template-value buckets before that.
func TestContentEntityBucketsFluxTypedEntitiesAppendedAtEnd(t *testing.T) {
	t.Parallel()

	n := len(contentEntityBuckets)
	if n < 9 {
		t.Fatalf("contentEntityBuckets has %d entries, want at least 9", n)
	}

	fluxTypedEntities := map[string]string{
		"flux_kustomizations":    "FluxKustomization",
		"flux_git_repositories":  "FluxGitRepository",
		"flux_oci_repositories":  "FluxOCIRepository",
		"flux_buckets":           "FluxBucket",
		"flux_helm_releases":     "FluxHelmRelease",
		"flux_helm_repositories": "FluxHelmRepository",
	}
	helmTemplateValues := map[string]string{
		"helm_value_definitions":     "HelmValueDefinition",
		"helm_template_value_usages": "HelmTemplateValueUsage",
	}

	// The six trailing entries must be the Flux typed entities, in this
	// fixed order.
	wantTrailing := []entityBucketMapping{
		{bucket: "flux_kustomizations", label: "FluxKustomization"},
		{bucket: "flux_git_repositories", label: "FluxGitRepository"},
		{bucket: "flux_oci_repositories", label: "FluxOCIRepository"},
		{bucket: "flux_buckets", label: "FluxBucket"},
		{bucket: "flux_helm_releases", label: "FluxHelmRelease"},
		{bucket: "flux_helm_repositories", label: "FluxHelmRepository"},
	}
	for i, want := range wantTrailing {
		got := contentEntityBuckets[n-len(wantTrailing)+i]
		if got != want {
			t.Fatalf("bucket at trailing index %d = %#v, want %#v (append at end)", i, got, want)
		}
	}

	// sql_migrations (#5346) must sit immediately before the Flux entries, never
	// shifted.
	sqlIdx := n - len(wantTrailing) - 1
	if got := contentEntityBuckets[sqlIdx]; got.bucket != "sql_migrations" || got.label != "SqlMigration" {
		t.Fatalf("bucket before flux entries = %q/%q, want sql_migrations/SqlMigration", got.bucket, got.label)
	}

	// The Helm template-value buckets (previous most-recent addition) must
	// remain immediately before sql_migrations, never shifted.
	if got := contentEntityBuckets[sqlIdx-1]; got.bucket != "helm_template_value_usages" || got.label != "HelmTemplateValueUsage" {
		t.Fatalf("bucket at sqlIdx-1 = %q/%q, want helm_template_value_usages/HelmTemplateValueUsage", got.bucket, got.label)
	}
	if got := contentEntityBuckets[sqlIdx-2]; got.bucket != "helm_value_definitions" || got.label != "HelmValueDefinition" {
		t.Fatalf("bucket at sqlIdx-2 = %q/%q, want helm_value_definitions/HelmValueDefinition", got.bucket, got.label)
	}

	// No Flux typed-entity bucket and no Helm template-value bucket may appear
	// anywhere earlier in the table.
	for i := 0; i < sqlIdx-2; i++ {
		if _, isFlux := fluxTypedEntities[contentEntityBuckets[i].bucket]; isFlux {
			t.Fatalf("Flux typed-entity bucket %q found mid-table at index %d; it must be appended at the end", contentEntityBuckets[i].bucket, i)
		}
		if _, isHelm := helmTemplateValues[contentEntityBuckets[i].bucket]; isHelm {
			t.Fatalf("Helm template-value bucket %q found mid-table at index %d; it must be appended at the end", contentEntityBuckets[i].bucket, i)
		}
	}

	// sql_migrations must appear only at sqlIdx, never elsewhere.
	for i := 0; i < n; i++ {
		if i != sqlIdx && contentEntityBuckets[i].bucket == "sql_migrations" {
			t.Fatalf("sql_migrations found at index %d; it must sit only just before the flux entries", i)
		}
	}
}
