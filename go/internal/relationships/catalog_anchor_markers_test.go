// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

// TestUnconditionalPayloadMarkersSelectContentNotKeyNames is the marker contract.
// Every parsed file carries the argocd_applications and argocd_applicationsets keys,
// mostly null, so a marker that names the key selects every file fact in the fleet.
// A row is selected for what the key holds.
func TestUnconditionalPayloadMarkersSelectContentNotKeyNames(t *testing.T) {
	t.Parallel()

	application := []any{map[string]any{"name": "svc", "source_repo": "https://github.com/acme/svc"}}
	applicationSet := []any{map[string]any{"name": "svc", "generator_source_repos": "https://github.com/acme/cfg"}}
	bucket := func(key string, value any) map[string]any {
		return map[string]any{"repo_id": "repository:r_1", "relative_path": "a.yaml", "parsed_file_data": map[string]any{key: value}}
	}
	content := func(body string) map[string]any {
		return map[string]any{"repo_id": "repository:r_1", "relative_path": "a.yaml", "content": body}
	}

	cases := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{"applications holds a row", bucket("argocd_applications", application), true},
		{"applicationsets holds a row", bucket("argocd_applicationsets", applicationSet), true},
		{"flux bucket holds a row", bucket("flux_git_repositories", []any{map[string]any{"name": "s", "url": "https://github.com/acme/my%2Dapp"}}), true},
		{"raw Application kind", content("kind: Application\nmetadata:\n  name: svc\n"), true},
		{"raw ApplicationSet kind", content("kind: ApplicationSet\nmetadata:\n  name: svc\n"), true},
		{"argocd artifact type", map[string]any{"relative_path": "a.yaml", "artifact_type": "argocd", "content": "x"}, true},

		{"applications null", bucket("argocd_applications", nil), false},
		{"applicationsets null", bucket("argocd_applicationsets", nil), false},
		{"applications empty array", bucket("argocd_applications", []any{}), false},
		{"applicationsets empty array", bucket("argocd_applicationsets", []any{}), false},
		{"applications empty object", bucket("argocd_applications", map[string]any{}), false},
		{"applicationsets empty object", bucket("argocd_applicationsets", map[string]any{}), false},
		{"applications empty string", bucket("argocd_applications", ""), false},
		{"applicationsets whitespace string", bucket("argocd_applicationsets", "   "), false},
		{"flux bucket null", bucket("flux_git_repositories", nil), false},
		{"flux bucket empty array", bucket("flux_git_repositories", []any{}), false},
		{"flux bucket empty object", bucket("flux_git_repositories", map[string]any{}), false},
		{"flux key name in prose", content("see flux_git_repositories"), false},
		{"key name in prose", content("see argocd_applications and argocd_applicationsets"), false},
		{"key name in a value", bucket("source", `"argocd_applications": [{"name": "x"}]`), false},
		{"key name in a path", map[string]any{"relative_path": "argocd_applications.yaml", "content": "x"}, false},
		{"other artifact type", map[string]any{"relative_path": "a.tf", "artifact_type": "terraform", "content": "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := facts.Envelope{ScopeID: "repo:one", Payload: tc.payload}
			if got := payloadMatchesAnchorsSim(t, envelope, UnconditionalPayloadMarkers()); got != tc.want {
				t.Fatalf("selected = %t, want %t for %#v", got, tc.want, tc.payload)
			}
			if tc.want {
				return
			}
			parsed, _ := tc.payload["parsed_file_data"].(map[string]any)
			applications, _ := factschema.DecodeParsedFileDataArgoCDApplications(parsed)
			appSets, _ := factschema.DecodeParsedFileDataArgoCDApplicationSets(parsed)
			gitRepositories, _ := factschema.DecodeParsedFileDataFluxGitRepositories(parsed)
			if len(applications)+len(appSets)+len(gitRepositories) != 0 {
				t.Fatalf("dropped a row the reader can use: %d applications, %d applicationsets, %d git repositories",
					len(applications), len(appSets), len(gitRepositories))
			}
		})
	}
}

// TestUnconditionalPayloadMarkersReturnsACopy holds the claim callers rely on when
// they append to the result: nothing they do to it reaches the shared list.
func TestUnconditionalPayloadMarkersReturnsACopy(t *testing.T) {
	t.Parallel()

	first := UnconditionalPayloadMarkers()
	want := first[0]
	first[0] = "overwritten"
	if got := UnconditionalPayloadMarkers()[0]; got != want {
		t.Fatalf("marker = %q, want %q: a caller's write reached the shared list", got, want)
	}
}

// TestUnconditionalPayloadMarkersFollowParserOutput holds the bucket markers to what the
// parser writes, so the "[{" spelling cannot drift from the real output. A manifest
// fills a bucket and is selected; any other YAML leaves the buckets empty and is not.
func TestUnconditionalPayloadMarkersFollowParserOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{"application", "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: svc\n" +
			"spec:\n  source:\n    repoURL: https://github.com/acme/svc.git\n    path: deploy\n", true},
		{"applicationset", "apiVersion: argoproj.io/v1alpha1\nkind: ApplicationSet\nmetadata:\n  name: svc\n" +
			"spec:\n  generators:\n    - git:\n        repoURL: https://github.com/acme/cfg.git\n", true},
		{"flux gitrepository", "apiVersion: source.toolkit.fluxcd.io/v1\nkind: GitRepository\nmetadata:\n  name: svc\n" +
			"  namespace: flux-system\nspec:\n  url: https://github.com/acme/my%2Dapp\n  ref:\n    branch: main\n", true},
		{"other yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: svc\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "manifest.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			engine, err := parser.DefaultEngine()
			if err != nil {
				t.Fatalf("DefaultEngine: %v", err)
			}
			parsed, err := engine.ParsePath(root, path, false, parser.Options{})
			if err != nil {
				t.Fatalf("ParsePath: %v", err)
			}
			// Raw source text can hold "kind: Application" too. This test is about
			// the buckets, so only they may decide.
			delete(parsed, "source")
			envelope := facts.Envelope{ScopeID: "repo:one", Payload: map[string]any{"parsed_file_data": parsed}}
			if got := payloadMatchesAnchorsSim(t, envelope, UnconditionalPayloadMarkers()); got != tc.want {
				t.Fatalf("selected = %t, want %t for parser output %#v", got, tc.want, parsed)
			}
		})
	}
}
