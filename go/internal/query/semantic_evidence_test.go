// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSemanticEvidenceHandlerListsDocumentationObservationsWithTruthMetadata(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticEvidenceStore{
		readModel: semanticEvidenceListReadModel{
			Rows: []map[string]any{{
				"fact_id":             "fact:semantic-doc-1",
				"fact_kind":           facts.SemanticDocumentationObservationFactKind,
				"truth_basis":         "semantic_observation",
				"provider_profile_id": "semantic-docs-default",
				"prompt_version":      "docs-prompt-v1",
				"redaction_version":   "strict-redaction-v1",
				"policy_state":        facts.SemanticPolicyAllowed,
				"freshness_state":     facts.SemanticFreshnessFresh,
				"admission_state":     facts.SemanticAdmissionPartial,
			}},
			NextCursor: "1",
		},
	}
	handler := &SemanticEvidenceHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/semantic/documentation-observations?repo=repo:payments&provider_profile_id=semantic-docs-default&freshness_state=fresh&admission_state=partial&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.filter.FactKind, facts.SemanticDocumentationObservationFactKind; got != want {
		t.Fatalf("filter.FactKind = %q, want %q", got, want)
	}
	if got, want := store.filter.Repository, "repo:payments"; got != want {
		t.Fatalf("filter.Repository = %q, want %q", got, want)
	}
	if got, want := store.filter.ProviderProfileID, "semantic-docs-default"; got != want {
		t.Fatalf("filter.ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := store.filter.AdmissionState, facts.SemanticAdmissionPartial; got != want {
		t.Fatalf("filter.AdmissionState = %q, want %q", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want semantic evidence truth")
	}
	if got, want := envelope.Truth.Basis, TruthBasisSemanticFacts; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	rows := data["observations"].([]any)
	row := rows[0].(map[string]any)
	for _, key := range []string{
		"provider_profile_id",
		"prompt_version",
		"redaction_version",
		"policy_state",
		"freshness_state",
		"truth_basis",
	} {
		if _, ok := row[key]; !ok {
			t.Fatalf("semantic observation row missing %q: %#v", key, row)
		}
	}
}

func TestSemanticEvidenceHandlerScopedEmptyGrantReturnsEmptyWithoutRead(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticEvidenceStore{
		readModel: semanticEvidenceListReadModel{
			Rows: []map[string]any{{
				"fact_id":   "fact:semantic-doc-out-of-scope",
				"fact_kind": facts.SemanticDocumentationObservationFactKind,
			}},
		},
	}
	handler := &SemanticEvidenceHandler{Content: store, Profile: ProfileProduction}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/semantic/documentation-observations?provider_profile_id=semantic-docs-default&limit=25",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listDocumentationObservations(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("semantic evidence store calls = %d, want 0 for empty scoped grants", store.calls)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	rows := data["observations"].([]any)
	if got := len(rows); got != 0 {
		t.Fatalf("len(observations) = %d, want 0", got)
	}
	if got, want := data["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestSemanticEvidenceHandlerAllScopeScopedAdminKeepsUnboundedSemanticFilter(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticEvidenceStore{
		readModel: semanticEvidenceListReadModel{
			Rows: []map[string]any{{
				"fact_id":   "fact:semantic-doc-admin-visible",
				"fact_kind": facts.SemanticDocumentationObservationFactKind,
			}},
		},
	}
	handler := &SemanticEvidenceHandler{Content: store, Profile: ProfileProduction}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/semantic/documentation-observations?provider_profile_id=semantic-docs-default&limit=25",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:      AuthModeScoped,
		TenantID:  "tenant-admin",
		AllScopes: true,
	}))
	rec := httptest.NewRecorder()

	handler.listDocumentationObservations(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.calls, 1; got != want {
		t.Fatalf("semantic evidence store calls = %d, want %d", got, want)
	}
	if len(store.filter.AllowedRepositoryIDs) != 0 || len(store.filter.AllowedScopeIDs) != 0 {
		t.Fatalf("all-scope admin filter has scoped bounds: %#v", store.filter)
	}
}

func TestBuildSemanticEvidenceSQLFiltersCodeHintsByScopeAndProvider(t *testing.T) {
	t.Parallel()

	query, args := buildSemanticEvidenceSQL(semanticEvidenceFilter{
		FactKind:           facts.SemanticCodeHintFactKind,
		Repository:         "repo:payments",
		RelativePath:       "go/payments/handler.go",
		EntityID:           "entity:payments.Handle",
		ProviderProfileID:  "semantic-code-default",
		FreshnessState:     facts.SemanticFreshnessFresh,
		CorroborationState: facts.SemanticCorroborationUncorroborated,
		PromptVersion:      "code-prompt-v1",
		RedactionVersion:   "strict-redaction-v1",
		PolicyState:        facts.SemanticPolicyAllowed,
		Limit:              25,
		Offset:             50,
	})

	for _, fragment := range []string{
		"fact_records.fact_kind = '" + facts.SemanticCodeHintFactKind + "'",
		"fact_records.is_tombstone = FALSE",
		"fact_records.payload->'source'->>'repository_id' = $",
		"fact_records.payload->'source'->>'relative_path' = $",
		"fact_records.payload->'subject'->>'entity_id' = $",
		"fact_records.payload->'provider'->>'provider_profile_id' = $",
		"fact_records.payload->'chunk'->>'prompt_version' = $",
		"fact_records.payload->'chunk'->>'redaction_version' = $",
		"fact_records.payload->>'policy_state' = $",
		"fact_records.payload->>'freshness_state' = $",
		"fact_records.payload->>'corroboration_state' = $",
		"ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC",
		"LIMIT $",
		"OFFSET $",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("semantic code hint SQL missing fragment %q:\n%s", fragment, query)
		}
	}
	if strings.Contains(query, facts.SemanticDocumentationObservationFactKind) {
		t.Fatalf("semantic code hint SQL included documentation observation kind:\n%s", query)
	}
	if got, want := args[len(args)-2], 26; got != want {
		t.Fatalf("limit arg = %#v, want %#v", got, want)
	}
	if got, want := args[len(args)-1], 50; got != want {
		t.Fatalf("offset arg = %#v, want %#v", got, want)
	}
}

func TestBuildSemanticEvidenceSQLAppliesScopedRepositoryAuthorizationBeforePaging(t *testing.T) {
	t.Parallel()

	query, args := buildSemanticEvidenceSQL(semanticEvidenceFilter{
		FactKind:             facts.SemanticCodeHintFactKind,
		Repository:           "repo-team-a",
		ProviderProfileID:    "semantic-code-default",
		AllowedRepositoryIDs: []string{"repo-team-a", "repo-team-a"},
		AllowedScopeIDs:      []string{"repo-scope-a"},
		Limit:                10,
	})

	for _, fragment := range []string{
		"fact_records.payload->'source'->>'repository_id' = $",
		"fact_records.scope_id IN (",
		"fact_records.payload->'source'->>'repository_id' IN (",
		"fact_records.payload->'subject'->>'repository_id' IN (",
		"jsonb_array_elements(COALESCE(fact_records.payload->'object_refs'",
		"ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC",
		"LIMIT $",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("semantic evidence SQL missing fragment %q:\n%s", fragment, query)
		}
	}
	authIndex := strings.Index(query, "fact_records.scope_id IN (")
	orderIndex := strings.Index(query, "ORDER BY")
	if authIndex < 0 || orderIndex < 0 || authIndex > orderIndex {
		t.Fatalf("authorization predicate must appear before ORDER BY:\n%s", query)
	}
	if got, want := args[len(args)-4], "repo-team-a"; got != want {
		t.Fatalf("first auth arg = %#v, want %#v; args=%#v", got, want, args)
	}
	if got, want := args[len(args)-3], "repo-scope-a"; got != want {
		t.Fatalf("second auth arg = %#v, want %#v; args=%#v", got, want, args)
	}
	if got, want := args[len(args)-2], 11; got != want {
		t.Fatalf("limit arg = %#v, want %#v; args=%#v", got, want, args)
	}
}

func TestDocumentationFactKindListDoesNotIncludeCodeHints(t *testing.T) {
	t.Parallel()

	sqlList := documentationCollectedFactKindSQLList()
	if strings.Contains(sqlList, facts.SemanticCodeHintFactKind) {
		t.Fatalf("documentation fact list includes code hints, want opt-in semantic code hint route only: %s", sqlList)
	}
	if !strings.Contains(sqlList, facts.SemanticDocumentationObservationFactKind) {
		t.Fatalf("documentation fact list missing documentation semantic observations: %s", sqlList)
	}
}

func TestSemanticEvidencePublicRowDropsProviderInternals(t *testing.T) {
	t.Parallel()

	row := semanticEvidencePublicRow(map[string]any{
		"fact_id":   "fact:semantic-doc-1",
		"fact_kind": facts.SemanticDocumentationObservationFactKind,
		"payload": map[string]any{
			"chunk": map[string]any{
				"chunk_id":           "chunk:doc",
				"prompt_version":     "semantic-docs.v1",
				"prompt_payload":     "raw prompt body",
				"raw_provider_input": "private input",
			},
			"provider": map[string]any{
				"provider_profile_id":   "semantic-docs-default",
				"provider_kind":         "deepseek",
				"credential_value":      "credential-placeholder",
				"raw_provider_response": "private output",
			},
			"subject": map[string]any{
				"entity_id":   "entity:payments.Handle",
				"secret_note": "private subject annotation",
			},
			"evidence_refs": []any{map[string]any{
				"kind":        "documentation_section",
				"id":          "section:deploy",
				"secret_note": "private evidence annotation",
			}},
			"object_refs": []any{map[string]any{
				"entity_id":         "entity:payments.Index",
				"raw_provider_hint": "private hint",
			}},
		},
	})

	for _, nestedKey := range []string{"chunk", "provider", "subject"} {
		nested, ok := row[nestedKey].(map[string]any)
		if !ok {
			t.Fatalf("row[%q] = %#v, want nested map", nestedKey, row[nestedKey])
		}
		for _, forbidden := range []string{
			"prompt_payload",
			"raw_provider_input",
			"credential_value",
			"raw_provider_response",
			"secret_note",
		} {
			if _, ok := nested[forbidden]; ok {
				t.Fatalf("row[%q] leaked %q: %#v", nestedKey, forbidden, nested)
			}
		}
	}
	for _, listKey := range []string{"evidence_refs", "object_refs"} {
		refs, ok := row[listKey].([]any)
		if !ok || len(refs) != 1 {
			t.Fatalf("row[%q] = %#v, want one ref", listKey, row[listKey])
		}
		ref, ok := refs[0].(map[string]any)
		if !ok {
			t.Fatalf("row[%q][0] = %#v, want map", listKey, refs[0])
		}
		for _, forbidden := range []string{"secret_note", "raw_provider_hint"} {
			if _, ok := ref[forbidden]; ok {
				t.Fatalf("row[%q] leaked %q: %#v", listKey, forbidden, ref)
			}
		}
	}
}

// TestSemanticEvidencePublicRowSurfacesBoundedSourceACLState proves the bounded
// source_acl_state observed on the payload's acl_summary is surfaced verbatim as
// a distinct access-posture axis on the public semantic-evidence row, alongside
// (never folded into) freshness_state/policy_state/admission_state. ACL and
// freshness are independent axes: a fresh row can be denied and a stale row can
// be allowed, so the test asserts both axes survive independently.
func TestSemanticEvidencePublicRowSurfacesBoundedSourceACLState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		freshness string
		acl       string
	}{
		{facts.SemanticFreshnessFresh, facts.SourceACLStateDenied},
		{facts.SemanticFreshnessStale, facts.SourceACLStateAllowed},
		{facts.SemanticFreshnessFresh, facts.SourceACLStatePartial},
		{facts.SemanticFreshnessFresh, facts.SourceACLStateMissing},
		{facts.SemanticFreshnessStale, facts.SourceACLStateStale},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.freshness+"_"+tc.acl, func(t *testing.T) {
			t.Parallel()
			row := semanticEvidencePublicRow(map[string]any{
				"fact_id":   "fact:semantic-doc-1",
				"fact_kind": facts.SemanticDocumentationObservationFactKind,
				"payload": map[string]any{
					"freshness_state": tc.freshness,
					"policy_state":    facts.SemanticPolicyAllowed,
					"acl_summary":     map[string]any{"source_acl_state": tc.acl},
				},
			})
			if got := row["source_acl_state"]; got != tc.acl {
				t.Fatalf("row[source_acl_state] = %#v, want %q (verbatim)", got, tc.acl)
			}
			if got := row["freshness_state"]; got != tc.freshness {
				t.Fatalf("row[freshness_state] = %#v, want %q (independent axis)", got, tc.freshness)
			}
			if got := row["policy_state"]; got != facts.SemanticPolicyAllowed {
				t.Fatalf("row[policy_state] = %#v, want %q (independent axis)", got, facts.SemanticPolicyAllowed)
			}
		})
	}
}

// TestSemanticEvidencePublicRowOmitsAbsentSourceACLState proves a row whose
// payload carries no bounded ACL claim surfaces no source_acl_state field
// (absence means "no ACL claim"), and that a non-bounded value is dropped rather
// than surfaced (fail closed).
func TestSemanticEvidencePublicRowOmitsAbsentSourceACLState(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]map[string]any{
		"no acl_summary": {"freshness_state": facts.SemanticFreshnessFresh},
		"empty state":    {"acl_summary": map[string]any{"source_acl_state": ""}},
		"non-bounded":    {"acl_summary": map[string]any{"source_acl_state": "unknown"}},
	} {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			row := semanticEvidencePublicRow(map[string]any{
				"fact_id":   "fact:semantic-doc-1",
				"fact_kind": facts.SemanticDocumentationObservationFactKind,
				"payload":   payload,
			})
			if _, present := row["source_acl_state"]; present {
				t.Fatalf("source_acl_state surfaced for payload %q with no bounded claim: %#v", name, row)
			}
		})
	}
}

type fakeSemanticEvidenceStore struct {
	filter    semanticEvidenceFilter
	readModel semanticEvidenceListReadModel
	err       error
	calls     int
}

func (s *fakeSemanticEvidenceStore) semanticEvidence(
	_ context.Context,
	filter semanticEvidenceFilter,
) (semanticEvidenceListReadModel, error) {
	s.calls++
	s.filter = filter
	return s.readModel, s.err
}
