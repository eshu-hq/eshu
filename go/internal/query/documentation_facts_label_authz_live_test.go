// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestDocumentationFactsLabelsHonorScopedGrantsLive proves the generation
// labels of a documentation facts page never disclose a scope the caller is
// not granted (#7128 review F1). A token granted only scope E asks about every
// other seeded scope and generation; each answer must be the one it gets for a
// scope or generation that does not exist, and must carry no ungranted scope
// id, generation id, or lifecycle state. A granted token, the shared key, and
// an unauthenticated (admin) caller keep the full labels.
func TestDocumentationFactsLabelsHonorScopedGrantsLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationFactsGenerationRowSet(t, ctx, db)
	handler := &DocumentationHandler{Content: NewContentReader(db), Profile: ProfileProduction}

	scoped := func(scopeIDs, repoIDs []string) *AuthContext {
		return &AuthContext{
			Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
			AllowedScopeIDs: scopeIDs, AllowedRepositoryIDs: repoIDs,
		}
	}
	grantedE := scoped([]string{docFactsScopeE}, nil)
	grantedERepo := scoped(nil, []string{docFactsScopeE}) // seed payload repo = scope id
	ungrantedIDs := []string{
		docFactsScopeA, docFactsScopeB, docFactsScopeC, docFactsScopeD,
		docFactsGenOld, docFactsGenNew, docFactsGenB, docFactsGenC, docFactsGenD,
	}
	leakedWords := []string{"dead_lettered_domain", "pending_repo_generation", "no_active_generation", "superseded"}

	// label is the part of a response that describes the page's generation.
	label := func(target, requested string, token *AuthContext) (string, string) {
		t.Helper()
		data, resp, body := documentationFactsScopedGET(t, handler, target, token)
		if n := len(data["facts"].([]any)); n != 0 {
			t.Fatalf("%s returned %d facts to a token that is not granted them", target, n)
		}
		encoded, err := json.Marshal(map[string]any{
			"states":             data["states"],
			"generation_binding": data["generation_binding"],
			"freshness":          resp.Truth.Freshness,
		})
		if err != nil {
			t.Fatalf("encode label: %v", err)
		}
		// The caller's own requested id may be echoed; nothing else may differ.
		return strings.ReplaceAll(string(encoded), requested, "<requested>"), body
	}

	for _, token := range []*AuthContext{grantedE, grantedERepo} {
		missingScope, _ := label("/api/v0/documentation/facts?scope_id=scope:docfacts-missing", "scope:docfacts-missing", token)
		for _, scope := range []string{docFactsScopeA, docFactsScopeB, docFactsScopeC, docFactsScopeD} {
			t.Run("empty page for ungranted "+scope, func(t *testing.T) {
				got, body := label("/api/v0/documentation/facts?scope_id="+scope, scope, token)
				if got != missingScope {
					t.Fatalf("ungranted %s label\n got  %s\n want %s (the nonexistent-scope label)", scope, got, missingScope)
				}
				assertNoDocumentationLabelLeak(t, strings.ReplaceAll(body, scope, "<requested>"), ungrantedIDs, leakedWords)
			})
		}

		missingGen, _ := label("/api/v0/documentation/facts?generation_id=generation:docfacts-missing&repo=x",
			"generation:docfacts-missing", token)
		for _, generation := range []string{docFactsGenOld, docFactsGenNew, docFactsGenB, docFactsGenC, docFactsGenD} {
			t.Run("explicit ungranted "+generation, func(t *testing.T) {
				got, body := label("/api/v0/documentation/facts?generation_id="+generation+"&repo=x", generation, token)
				if got != missingGen {
					t.Fatalf("ungranted %s label\n got  %s\n want %s (the nonexistent-generation label)", generation, got, missingGen)
				}
				assertNoDocumentationLabelLeak(t, strings.ReplaceAll(body, generation, "<requested>"), ungrantedIDs, leakedWords)
			})
		}
	}

	t.Run("granted and unscoped callers keep the full labels", func(t *testing.T) {
		shared := &AuthContext{Mode: AuthModeShared}
		for _, tc := range []struct {
			name   string
			target string
			token  *AuthContext
			want   []string
		}{
			{"granted empty active scope", "/api/v0/documentation/facts?scope_id=" + docFactsScopeE, grantedE, []string{docFactsGenE}},
			{"granted by repo", "/api/v0/documentation/facts?scope_id=" + docFactsScopeE, grantedERepo, []string{docFactsGenE}},
			{
				"granted dead-lettered scope", "/api/v0/documentation/facts?scope_id=" + docFactsScopeB,
				scoped([]string{docFactsScopeB}, nil),
				[]string{"no_active_generation", "dead_lettered_domain"},
			},
			{
				"granted explicit superseded", "/api/v0/documentation/facts?generation_id=" + docFactsGenOld + "&repo=x",
				scoped([]string{docFactsScopeA}, nil),
				[]string{"superseded", docFactsScopeA},
			},
			{
				"shared key dead-lettered scope", "/api/v0/documentation/facts?scope_id=" + docFactsScopeB, shared,
				[]string{"no_active_generation", "dead_lettered_domain"},
			},
			{
				"shared key explicit superseded", "/api/v0/documentation/facts?generation_id=" + docFactsGenOld + "&repo=x", shared,
				[]string{"superseded", docFactsScopeA},
			},
			{"admin active generation", "/api/v0/documentation/facts?scope_id=" + docFactsScopeE, nil, []string{docFactsGenE}},
			{
				"admin pending scope", "/api/v0/documentation/facts?scope_id=" + docFactsScopeC, nil,
				[]string{"no_active_generation", "pending_repo_generation"},
			},
		} {
			_, _, body := documentationFactsScopedGET(t, handler, tc.target, tc.token)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s: response lacks %q:\n%s", tc.name, want, body)
				}
			}
		}
	})

	t.Run("granted token reading rows keeps the row generation label", func(t *testing.T) {
		data, _, _ := documentationFactsScopedGET(t, handler,
			"/api/v0/documentation/facts?scope_id="+docFactsScopeA, scoped([]string{docFactsScopeA}, nil))
		want := map[string]any{"mode": "active", "generation_id": docFactsGenNew, "is_active": true}
		if got := data["generation_binding"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("generation_binding = %#v, want %#v", got, want)
		}
	})

	// Runs last: it adds a fact to scope A's superseded generation that a
	// repository grant reaches through the fact payload, not the scope.
	t.Run("explicit rows reached through a fact grant keep their label", func(t *testing.T) {
		const refRepo = "repository:docfacts-ref-granted"
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ('fact:docfacts-ref', $1, $2, 'documentation_document', 'stable:docfacts-ref',
  'proof', 'proof', 'key:docfacts-ref', clock_timestamp(), clock_timestamp(),
  jsonb_build_object('repository_id', $3::text, 'source_id', 'doc-source:docfacts-ref'))`,
			docFactsScopeA, docFactsGenOld, refRepo); err != nil {
			t.Fatalf("seed fact-granted row: %v", err)
		}
		token := scoped(nil, []string{refRepo})
		data, resp, _ := documentationFactsScopedGET(t, handler,
			"/api/v0/documentation/facts?generation_id="+docFactsGenOld+"&source_id=doc-source:docfacts-ref", token)
		if n := len(data["facts"].([]any)); n != 1 {
			t.Fatalf("fact-granted read returned %d rows, want 1", n)
		}
		if resp.Truth.Freshness.State != "stale" || !strings.Contains(resp.Truth.Freshness.Detail, docFactsScopeA) {
			t.Fatalf("returned rows labelled %#v, want stale for %s", resp.Truth.Freshness, docFactsScopeA)
		}
		// The same token and generation with no visible row learns nothing.
		got, body := label("/api/v0/documentation/facts?generation_id="+docFactsGenOld+"&source_id=doc-source:none",
			docFactsGenOld, token)
		want, _ := label("/api/v0/documentation/facts?generation_id=generation:docfacts-missing&source_id=doc-source:none",
			"generation:docfacts-missing", token)
		if got != want {
			t.Fatalf("empty fact-granted read label\n got  %s\n want %s", got, want)
		}
		assertNoDocumentationLabelLeak(t, strings.ReplaceAll(body, docFactsGenOld, "<requested>"), ungrantedIDs, leakedWords)
	})
}

func assertNoDocumentationLabelLeak(t *testing.T, body string, ids, words []string) {
	t.Helper()
	for _, s := range append(append([]string{}, ids...), words...) {
		if strings.Contains(body, s) {
			t.Fatalf("response discloses %q to a token not granted it:\n%s", s, body)
		}
	}
}
