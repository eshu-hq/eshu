package shape

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestMaterializeBuildsAnalyticsEntities(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID:       "repository:r_analytics",
		SourceSystem: "git",
		Files: []File{
			{
				Path:         "target/manifest.json",
				Body:         `{"metadata":{"project_name":"jaffle_shop"}}`,
				Digest:       "digest-analytics",
				Language:     "json",
				ArtifactType: "dbt_manifest",
				EntityBuckets: map[string][]Entity{
					"analytics_models": {
						{
							Name:       "order_metrics",
							LineNumber: 1,
							Metadata: map[string]any{
								"asset_name":      "analytics.public.order_metrics",
								"materialization": "view",
								"parse_state":     "complete",
							},
						},
					},
					"data_assets": {
						{
							Name:       "analytics.public.order_metrics",
							LineNumber: 1,
							Metadata: map[string]any{
								"kind":     "model",
								"database": "analytics",
								"schema":   "public",
							},
						},
					},
					"data_columns": {
						{
							Name:       "analytics.public.order_metrics.customer_id",
							LineNumber: 1,
							Metadata: map[string]any{
								"asset_name": "analytics.public.order_metrics",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 3 {
		t.Fatalf("len(Materialize().Entities) = %d, want 3", len(got.Entities))
	}

	model := got.Entities[0]
	if got, want := model.EntityType, "AnalyticsModel"; got != want {
		t.Fatalf("entities[0].EntityType = %q, want %q", got, want)
	}
	if got, want := model.EntityID, content.CanonicalEntityID("repository:r_analytics", "target/manifest.json", "AnalyticsModel", "order_metrics", 1); got != want {
		t.Fatalf("entities[0].EntityID = %q, want %q", got, want)
	}
	if got, want := model.Metadata["asset_name"], "analytics.public.order_metrics"; got != want {
		t.Fatalf("entities[0].Metadata[asset_name] = %#v, want %#v", got, want)
	}

	asset := got.Entities[1]
	if got, want := asset.EntityType, "DataAsset"; got != want {
		t.Fatalf("entities[1].EntityType = %q, want %q", got, want)
	}
	if got, want := asset.Metadata["kind"], "model"; got != want {
		t.Fatalf("entities[1].Metadata[kind] = %#v, want %#v", got, want)
	}

	column := got.Entities[2]
	if got, want := column.EntityType, "DataColumn"; got != want {
		t.Fatalf("entities[2].EntityType = %q, want %q", got, want)
	}
	if got, want := column.Metadata["asset_name"], "analytics.public.order_metrics"; got != want {
		t.Fatalf("entities[2].Metadata[asset_name] = %#v, want %#v", got, want)
	}
}
