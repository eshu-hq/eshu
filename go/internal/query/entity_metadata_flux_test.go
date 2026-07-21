// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

// TestEnrichEntityResultsWithContentMetadataFluxGitRepository is the #5360 PR A
// codex P1 regression: a graph-projected FluxGitRepository node returned via
// get_entity_context carries none of its typed fields (url, ref_branch, ...)
// because the fixed graph metadata projection does not select them and the
// content-enrichment fallback only fires for labels that
// graphLabelToContentEntityType maps. Before the fix that map omitted the four
// Flux labels, so resultContentEntityType returned "" and the url was silently
// dropped -- the same read-surface-bridge gap #5346 fixed for SqlMigration.
func TestEnrichEntityResultsWithContentMetadataFluxGitRepository(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-flux-git", "repo-1", "clusters/production/flux-system.yaml", "FluxGitRepository", "flux-system",
					int64(1), int64(9), "yaml", "", []byte(`{"url":"https://github.com/acme/flux-system","ref_branch":"main"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "content-flux-git",
			"name":       "flux-system",
			"labels":     []string{"FluxGitRepository"},
			"file_path":  "clusters/production/flux-system.yaml",
			"repo_id":    "repo-1",
			"language":   "yaml",
			"start_line": 1,
			"end_line":   9,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "flux-system", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any (Flux typed fields must be surfaced via content enrichment)", got[0]["metadata"])
	}
	if gotValue, want := metadata["url"], "https://github.com/acme/flux-system"; gotValue != want {
		t.Fatalf("metadata[url] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["ref_branch"], "main"; gotValue != want {
		t.Fatalf("metadata[ref_branch] = %#v, want %#v", gotValue, want)
	}
}

// TestEnrichEntityResultsWithContentMetadataFluxBucket is the sibling P1
// regression for FluxBucket's bucket_name field.
func TestEnrichEntityResultsWithContentMetadataFluxBucket(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-flux-bucket", "repo-1", "clusters/production/bucket.yaml", "FluxBucket", "flux-artifacts",
					int64(1), int64(9), "yaml", "", []byte(`{"bucket_name":"flux-artifacts","endpoint":"minio.acme.internal","provider":"generic"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "content-flux-bucket",
			"name":       "flux-artifacts",
			"labels":     []string{"FluxBucket"},
			"file_path":  "clusters/production/bucket.yaml",
			"repo_id":    "repo-1",
			"language":   "yaml",
			"start_line": 1,
			"end_line":   9,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "flux-artifacts", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any (Flux Bucket typed fields must be surfaced via content enrichment)", got[0]["metadata"])
	}
	if gotValue, want := metadata["bucket_name"], "flux-artifacts"; gotValue != want {
		t.Fatalf("metadata[bucket_name] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["provider"], "generic"; gotValue != want {
		t.Fatalf("metadata[provider] = %#v, want %#v", gotValue, want)
	}
}

// TestEnrichEntityResultsWithContentMetadataFluxHelmRelease is the issue
// #5483 C1 sibling of the #5360 PR A read-surface-bridge regression: a
// graph-projected FluxHelmRelease node must surface its typed chart/sourceRef
// fields through get_entity_context via the content-enrichment bridge, since
// the fixed graph metadata projection does not select them.
func TestEnrichEntityResultsWithContentMetadataFluxHelmRelease(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-flux-helmrelease", "repo-1", "clusters/production/helmrelease.yaml", "FluxHelmRelease", "podinfo",
					int64(1), int64(9), "yaml", "", []byte(`{"chart":"podinfo","chart_version":"6.x","source_ref_kind":"HelmRepository"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "content-flux-helmrelease",
			"name":       "podinfo",
			"labels":     []string{"FluxHelmRelease"},
			"file_path":  "clusters/production/helmrelease.yaml",
			"repo_id":    "repo-1",
			"language":   "yaml",
			"start_line": 1,
			"end_line":   9,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "podinfo", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any (FluxHelmRelease typed fields must be surfaced via content enrichment)", got[0]["metadata"])
	}
	if gotValue, want := metadata["chart"], "podinfo"; gotValue != want {
		t.Fatalf("metadata[chart] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["source_ref_kind"], "HelmRepository"; gotValue != want {
		t.Fatalf("metadata[source_ref_kind] = %#v, want %#v", gotValue, want)
	}
}

// TestEnrichEntityResultsWithContentMetadataFluxHelmRepository is the
// FluxHelmRepository sibling, proving its url/repo_type fields surface the
// same way.
func TestEnrichEntityResultsWithContentMetadataFluxHelmRepository(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-flux-helmrepo", "repo-1", "clusters/production/helmrepository.yaml", "FluxHelmRepository", "podinfo",
					int64(1), int64(9), "yaml", "", []byte(`{"url":"https://stefanprodan.github.io/podinfo","repo_type":"default"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "content-flux-helmrepo",
			"name":       "podinfo",
			"labels":     []string{"FluxHelmRepository"},
			"file_path":  "clusters/production/helmrepository.yaml",
			"repo_id":    "repo-1",
			"language":   "yaml",
			"start_line": 1,
			"end_line":   9,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "podinfo", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any (FluxHelmRepository typed fields must be surfaced via content enrichment)", got[0]["metadata"])
	}
	if gotValue, want := metadata["url"], "https://stefanprodan.github.io/podinfo"; gotValue != want {
		t.Fatalf("metadata[url] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["repo_type"], "default"; gotValue != want {
		t.Fatalf("metadata[repo_type] = %#v, want %#v", gotValue, want)
	}
}

// TestFluxEntityTypesAreNotLanguageQueryable locks the entity_context-only
// design decision (#5360 PR A, ledger read_surfaces: [entity_context]): the
// four Flux typed entities are content-enriched for get_entity_context but must
// NOT be advertised in the language-query entity_type enum. They carry language
// "yaml" (not a supportedLanguages value), and adding them to
// graphFirstContentBackedEntityTypes -- as the P1 bridge fix could tempt --
// would leak them into allSupportedEntityTypes() and falsely claim
// language-query support, the exact #5369 failure mode.
func TestFluxEntityTypesAreNotLanguageQueryable(t *testing.T) {
	t.Parallel()

	typeSet := make(map[string]bool)
	for _, typ := range SupportedEntityTypes() {
		typeSet[typ] = true
	}
	for _, absent := range []string{
		"flux_kustomization", "flux_git_repository", "flux_oci_repository", "flux_bucket",
		"flux_helm_release", "flux_helm_repository",
	} {
		if typeSet[absent] {
			t.Errorf("entity type %q must not be language-queryable (Flux is entity_context-only, no yaml language)", absent)
		}
	}
}
