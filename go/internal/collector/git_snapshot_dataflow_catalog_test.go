package collector

import "testing"

func TestBuildDataflowCatalogVersionsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{
		{"dataflow_catalog_versions": []map[string]any{
			{"lang": "typescript", "catalog": "taint", "version": "v2"},
			{"lang": "python", "catalog": "taint", "version": "v1"},
		}},
		{"dataflow_catalog_versions": []map[string]any{
			{"lang": "typescript", "catalog": "taint", "version": "v2"},
			{"lang": "", "catalog": "taint", "version": "missing-language"},
		}},
	}
	got := buildDataflowCatalogVersions(parsed)
	if len(got) != 2 {
		t.Fatalf("len(buildDataflowCatalogVersions) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Language != "python" || got[0].Version != "v1" {
		t.Fatalf("versions not sorted by language/catalog/version: %+v", got)
	}
	if got[1].Language != "typescript" || got[1].Version != "v2" {
		t.Fatalf("versions not deduplicated correctly: %+v", got)
	}
}

func TestSnapshotFreshnessHintFoldsDataflowCatalogVersions(t *testing.T) {
	t.Parallel()

	base := RepositorySnapshot{FileCount: 1}
	baseline := snapshotFreshnessHint(base)

	withVersion := base
	withVersion.DataflowCatalogVersions = []DataflowCatalogVersionSnapshot{
		{Language: "python", Catalog: "taint", Version: "v1"},
	}
	withVersionHint := snapshotFreshnessHint(withVersion)
	if withVersionHint == baseline {
		t.Fatalf("catalog version did not change freshness hint: %s", baseline)
	}

	changedVersion := base
	changedVersion.DataflowCatalogVersions = []DataflowCatalogVersionSnapshot{
		{Language: "python", Catalog: "taint", Version: "v2"},
	}
	if got := snapshotFreshnessHint(changedVersion); got == withVersionHint {
		t.Fatalf("catalog version value is not load-bearing: %s", got)
	}
}
