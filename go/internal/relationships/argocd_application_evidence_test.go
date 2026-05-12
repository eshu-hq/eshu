package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverStructuredArgoCDEvidencePreservesApplicationSourceTupleAlignment(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/multi-source.yaml",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":             "multi-source",
							"source_repos":     "https://github.com/myorg/helm-charts.git,https://github.com/myorg/config-repo.git",
							"source_paths":     ",envs/prod",
							"source_revisions": "main,release",
							"source_roots":     ",envs/",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-charts", Aliases: []string{"helm-charts"}},
		{RepoID: "repo-config", Aliases: []string{"config-repo"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len(evidence) = %d, want 2: %#v", len(evidence), evidence)
	}

	for _, item := range evidence {
		if item.TargetRepoID == "repo-charts" {
			if _, ok := item.Details["first_party_ref_path"]; ok {
				t.Fatalf("repo-charts must not inherit first_party_ref_path: %#v", item.Details)
			}
			if _, ok := item.Details["first_party_ref_root"]; ok {
				t.Fatalf("repo-charts must not inherit first_party_ref_root: %#v", item.Details)
			}
			if got, want := item.Details["source_revision"], "main"; got != want {
				t.Fatalf("repo-charts source_revision = %#v, want %#v", got, want)
			}
		}
		if item.TargetRepoID == "repo-config" {
			if got, want := item.Details["first_party_ref_path"], "envs/prod"; got != want {
				t.Fatalf("repo-config first_party_ref_path = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_root"], "envs/"; got != want {
				t.Fatalf("repo-config first_party_ref_root = %#v, want %#v", got, want)
			}
			if got, want := item.Details["source_revision"], "release"; got != want {
				t.Fatalf("repo-config source_revision = %#v, want %#v", got, want)
			}
		}
	}
}
