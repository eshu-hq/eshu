// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestCatalogPayloadAnchorsCanonicalRepoIDIsDiscriminating is the QA OOM
// regression. A canonical repo id tokenizes to [repository, r_<8-hex>], both 10
// characters, and every content and file payload stores it under "repo_id". The
// id must anchor on its r_ token, or the LIKE ANY predicate matches the fleet.
func TestCatalogPayloadAnchorsCanonicalRepoIDIsDiscriminating(t *testing.T) {
	t.Parallel()

	repoID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/payments", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID: %v", err)
	}
	if len(strings.TrimPrefix(repoID, "repository:")) != len("r_9a7e0001") {
		t.Fatalf("canonical repo id %q no longer has the 10-character form this regression pins", repoID)
	}

	got := CatalogPayloadAnchors([]CatalogEntry{{RepoID: repoID, Aliases: []string{repoID}}})
	want := []string{strings.TrimPrefix(repoID, "repository:")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

// TestCatalogPayloadAnchorsSlugAnchorsOnRepositoryToken is the owner-token
// regression. A slug's owner is shared by most of a fleet and sits in every clone
// path a file row stores, so a slug anchored on its owner selects most of the file
// rows however long the token is. The real catalog derivation is what feeds the
// load, so that is what this runs.
func TestCatalogPayloadAnchorsSlugAnchorsOnRepositoryToken(t *testing.T) {
	t.Parallel()

	entry, ok := RepositoryCatalogEntry(map[string]any{
		"repo_id":   "repository:r_9a7e0001",
		"name":      "svc-api",
		"repo_slug": "acme-corporation/svc-api",
	})
	if !ok {
		t.Fatal("RepositoryCatalogEntry returned ok=false")
	}
	got := CatalogPayloadAnchors([]CatalogEntry{entry})
	want := []string{"r_9a7e0001", "svc-api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

// TestCatalogPayloadAnchorsTokenSelection covers the boundary lengths around the
// 10-character "repository" token, ids with and without the r_ token, ties, and
// aliases with several tokens. Every alias must still yield an anchor: the
// fleet-wide token is only passed over when another token can stand in for it.
func TestCatalogPayloadAnchorsTokenSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alias string
		want  []string
	}{
		{name: "9 character id is shorter than the namespace", alias: "repository:r_1234567", want: []string{"r_1234567"}},
		{name: "10 character id ties the namespace", alias: "repository:r_9a7e0001", want: []string{"r_9a7e0001"}},
		{name: "11 character id beats the namespace", alias: "repository:r_9a7e00011", want: []string{"r_9a7e00011"}},
		{name: "id without the r_ token", alias: "repository:abc", want: []string{"abc"}},
		{name: "id without the r_ token at length 10", alias: "repository:abcdefghij", want: []string{"abcdefghij"}},
		{name: "namespace is not first", alias: "r_9a7e0001:repository", want: []string{"r_9a7e0001"}},
		{name: "uppercase id", alias: "Repository:R_9A7E0001", want: []string{"r_9a7e0001"}},
		{name: "several tokens with the namespace", alias: "repository:acme:payments", want: []string{"payments"}},
		{name: "longest wins over a later short token without a slash", alias: "team payments api", want: []string{"payments"}},
		{name: "tie goes to the later token without a slash", alias: "acme shop", want: []string{"shop"}},
		{name: "three way tie goes to the last token without a slash", alias: "aaa bbb ccc", want: []string{"ccc"}},
		{name: "slug takes the last token over a longer owner", alias: "acme-corporation/svc-api", want: []string{"svc-api"}},
		{name: "slug takes the last token on a length tie", alias: "acme/shop", want: []string{"shop"}},
		{name: "path takes its last token however short", alias: "github.com/acme-corporation/svc", want: []string{"svc"}},
		{name: "slug drops a git suffix before picking", alias: "acme-corporation/svc.git", want: []string{"svc"}},
		{name: "slug ignores a trailing slash", alias: "acme-corporation/svc/", want: []string{"svc"}},
		{name: "slug ending in the namespace keeps the owner", alias: "acme-corporation/repository", want: []string{"acme-corporation"}},
		{name: "slug of only the namespace keeps it", alias: "repository/repository", want: []string{"repository"}},
		{name: "alias that is only the namespace keeps it", alias: "repository", want: []string{"repository"}},
		{name: "alias of the namespace twice keeps it", alias: "repository:repository", want: []string{"repository"}},
		{name: "longer token that contains the namespace is kept", alias: "repository-service", want: []string{"repository-service"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := CatalogPayloadAnchors([]CatalogEntry{{RepoID: "repo:x", Aliases: []string{tt.alias}}})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("anchors = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCatalogPayloadAnchorsTokenIsFromTheAlias proves the superset rule directly
// through the real matcher: when an alias matches a candidate the chosen anchor
// is a substring of that candidate. A chosen token that is not one of the alias's
// own tokens could pass the selection test and still drop the fact.
func TestCatalogPayloadAnchorsTokenIsFromTheAlias(t *testing.T) {
	t.Parallel()

	aliases := []string{
		"repository:r_9a7e0001",
		"repository:r_1234567",
		"repository:r_9a7e00011",
		"repository:acme:payments",
		"acme/shop",
		"acme-corporation/svc-api",
		"acme-corporation/repository",
		"team payments api",
		"repository",
	}
	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			t.Parallel()

			entry := CatalogEntry{RepoID: "repo:target", Aliases: []string{alias}}
			candidate := "uses " + alias + " for deployment"
			if matchesEntry(candidate, entry) == "" {
				t.Fatalf("precondition: alias %q must match candidate %q", alias, candidate)
			}
			anchors := CatalogPayloadAnchors([]CatalogEntry{entry})
			if len(anchors) == 0 {
				t.Fatalf("alias %q produced no anchor", alias)
			}
			for _, anchor := range anchors {
				if !strings.Contains(strings.ToLower(candidate), anchor) {
					t.Fatalf("anchor %q is not a substring of matched candidate %q", anchor, candidate)
				}
				if !slices.Contains(catalogMatchTokens(alias), anchor) {
					t.Fatalf("anchor %q is not one of the alias tokens %v", anchor, catalogMatchTokens(alias))
				}
			}
		})
	}
}

// TestCatalogPayloadAnchorsDoNotDependOnCatalogOrder keeps the SQL parameter
// stable across catalog reorderings, so the query text and its plan do not churn.
func TestCatalogPayloadAnchorsDoNotDependOnCatalogOrder(t *testing.T) {
	t.Parallel()

	first := CatalogEntry{RepoID: "repository:r_9a7e0001", Aliases: []string{"repository:r_9a7e0001", "payments", "acme/payments"}}
	second := CatalogEntry{RepoID: "repository:r_2e4a0001", Aliases: []string{"repository:r_2e4a0001", "orders", "acme/orders"}}

	forward := CatalogPayloadAnchors([]CatalogEntry{first, second})
	backward := CatalogPayloadAnchors([]CatalogEntry{second, first})
	if !reflect.DeepEqual(forward, backward) {
		t.Fatalf("anchors depend on catalog order: %v vs %v", forward, backward)
	}
	want := []string{"orders", "payments", "r_2e4a0001", "r_9a7e0001"}
	if !reflect.DeepEqual(forward, want) {
		t.Fatalf("anchors = %v, want %v", forward, want)
	}
}

// TestCatalogPayloadAnchorsCoverFluxRemoteURL covers the one extractor that does
// not match through aliases. discoverStructuredFluxEvidence resolves spec.url by
// equality with RemoteURL, so a repository whose aliases share no token with its
// remote still needs an anchor that the Flux fact's own payload contains. The
// generic "repository" anchor hid this: it selected the Flux fact through the
// fact's own repo_id.
func TestCatalogPayloadAnchorsCoverFluxRemoteURL(t *testing.T) {
	t.Parallel()

	spellings := []string{
		"https://github.com/acme/renamed-config",
		"https://github.com/Acme/Renamed-Config.git",
		"ssh://git@github.com/acme/renamed-config.git",
		"git@github.com:acme/renamed-config.git",
	}
	for _, url := range spellings {
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			target := CatalogEntry{
				RepoID:    "repository:r_9a7e0001",
				Aliases:   []string{"repository:r_9a7e0001", "local-checkout-name"},
				RemoteURL: repositoryidentity.NormalizeRemoteURL("https://github.com/acme/renamed-config"),
			}
			envelope := facts.Envelope{
				ScopeID: "git:source",
				Payload: map[string]any{
					"repo_id":       "repository:r_2e4a0001",
					"relative_path": "clusters/prod/source.yaml",
					"parsed_file_data": map[string]any{
						"flux_git_repositories": []any{map[string]any{
							"name": "platform", "namespace": "flux-system", "url": url,
						}},
					},
				},
			}
			catalog := []CatalogEntry{target}
			if len(DiscoverEvidence([]facts.Envelope{envelope}, catalog)) != 1 {
				t.Fatalf("precondition: Flux url %q must resolve to the target", url)
			}
			if !payloadMatchesAnchorsSim(t, envelope, CatalogPayloadAnchors(catalog)) {
				t.Fatalf("Flux fact with url %q is not selected by anchors %v", url, CatalogPayloadAnchors(catalog))
			}
		})
	}
}

// TestCatalogNonRepoIDPayloadAnchorsLeaveOutRemoteURL holds the deferred terms to
// names and slugs. Only Flux resolves through RemoteURL and a marker selects those
// rows, so a remote token here would only add rows to every deferred pass.
func TestCatalogNonRepoIDPayloadAnchorsLeaveOutRemoteURL(t *testing.T) {
	t.Parallel()

	got := CatalogNonRepoIDPayloadAnchors([]CatalogEntry{{
		RepoID:    "repository:r_9a7e0001",
		Aliases:   []string{"repository:r_9a7e0001", "local-checkout-name"},
		RemoteURL: "https://github.com/acme/renamed-config",
	}})
	if want := []string{"local-checkout-name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}

// TestCatalogPayloadAnchorsOmitRepositoryForRealCatalogEntry runs the production
// catalog derivation, so the regression also holds for the alias set a real
// repository fact produces rather than a hand-built entry.
func TestCatalogPayloadAnchorsOmitRepositoryForRealCatalogEntry(t *testing.T) {
	t.Parallel()

	entry, ok := RepositoryCatalogEntry(map[string]any{
		"repo_id":    "repository:r_9a7e0001",
		"name":       "payments",
		"repo_slug":  "acme/payments",
		"remote_url": "https://github.com/acme/payments",
	})
	if !ok {
		t.Fatal("RepositoryCatalogEntry returned ok=false")
	}
	got := CatalogPayloadAnchors([]CatalogEntry{entry})
	want := []string{"payments", "r_9a7e0001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
}
