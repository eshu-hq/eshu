// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func sortedConfigRepoIDs(refs []ArgoCDGeneratorConfigRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.ConfigRepoID)
	}
	sort.Strings(out)
	return out
}

func TestResolveArgoCDGeneratorConfigReposFromContent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo:control-plane",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "appset.yaml",
				"content": "kind: ApplicationSet\n" +
					"spec:\n" +
					"  generators:\n" +
					"    - git:\n" +
					"        repoURL: gitops-config\n" +
					"        files:\n" +
					"          - path: apps/*/config.yaml\n" +
					"  template:\n" +
					"    spec:\n" +
					"      source:\n" +
					"        repoURL: '{{.path.basenameNormalized}}'\n",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo:control-plane", Aliases: []string{"control-plane"}},
		{RepoID: "repo:gitops-config", Aliases: []string{"gitops-config"}},
	}

	refs := ResolveArgoCDGeneratorConfigRepos(envelopes, catalog)
	if got, want := sortedConfigRepoIDs(refs), []string{"repo:gitops-config"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("config repo IDs = %v, want %v", got, want)
	}
}

func TestResolveArgoCDGeneratorConfigReposFromStructured(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo:control-plane",
			Payload: map[string]any{
				"relative_path": "appset.yaml",
				"parsed_file_data": map[string]any{
					"argocd_applicationsets": []any{
						map[string]any{
							"name":                   "platform",
							"generator_source_repos": "gitops-config",
							"generator_source_paths": "apps/*/config.yaml",
							"template_source_repos":  "{{.service}}",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo:control-plane", Aliases: []string{"control-plane"}},
		{RepoID: "repo:gitops-config", Aliases: []string{"gitops-config"}},
	}

	refs := ResolveArgoCDGeneratorConfigRepos(envelopes, catalog)
	if got, want := sortedConfigRepoIDs(refs), []string{"repo:gitops-config"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("config repo IDs = %v, want %v", got, want)
	}
}

func TestResolveArgoCDGeneratorConfigReposExcludesControlRepo(t *testing.T) {
	t.Parallel()

	// A generator repoURL that resolves to the control repo itself must not be
	// returned: discoverArgoCDDocumentEvidence skips configRepo == controlRepo, so
	// the config file is the control repo's own file, already loaded.
	envelopes := []facts.Envelope{
		{
			ScopeID: "repo:control-plane",
			Payload: map[string]any{
				"repo_id":       "repo:control-plane",
				"artifact_type": "argocd",
				"relative_path": "appset.yaml",
				"content": "kind: ApplicationSet\n" +
					"spec:\n" +
					"  generators:\n" +
					"    - git:\n" +
					"        repoURL: control-plane\n" +
					"        files:\n" +
					"          - path: apps/*/config.yaml\n" +
					"  template:\n" +
					"    spec:\n" +
					"      source:\n" +
					"        repoURL: '{{.path.basenameNormalized}}'\n",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo:control-plane", Aliases: []string{"control-plane"}},
	}

	refs := ResolveArgoCDGeneratorConfigRepos(envelopes, catalog)
	if len(refs) != 0 {
		t.Fatalf("expected no config repos (control repo excluded), got %v", sortedConfigRepoIDs(refs))
	}
}

func TestResolveArgoCDGeneratorConfigReposIgnoresNonApplicationSets(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo:app",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "gitops-config"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo:gitops-config", Aliases: []string{"gitops-config"}},
	}

	if refs := ResolveArgoCDGeneratorConfigRepos(envelopes, catalog); len(refs) != 0 {
		t.Fatalf("expected no config repos for non-ApplicationSet fact, got %v", sortedConfigRepoIDs(refs))
	}
}

func TestResolveArgoCDGeneratorConfigReposEmptyInputs(t *testing.T) {
	t.Parallel()

	if refs := ResolveArgoCDGeneratorConfigRepos(nil, nil); refs != nil {
		t.Fatalf("expected nil for empty inputs, got %v", refs)
	}
}
