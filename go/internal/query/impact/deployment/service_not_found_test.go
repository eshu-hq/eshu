// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// notFoundStubStore answers SearchEntitiesByExactNameAnyRepo from a fixed
// fixture regardless of the requested type, so the tests prove the helper's
// own type and name re-verification rather than the double's. All other
// ContentStore methods stay absent: the helper must never call them.
type notFoundStubStore struct {
	querycontract.ContentStore
	rows     []querycontract.EntityContent
	errTypes map[string]bool
	limits   map[string]int
	calls    int
}

func (s *notFoundStubStore) SearchEntitiesByExactNameAnyRepo(
	_ context.Context, entityType, name string, limit int,
) ([]querycontract.EntityContent, error) {
	s.calls++
	if s.limits == nil {
		s.limits = map[string]int{}
	}
	s.limits[entityType] = limit
	if s.errTypes[entityType] {
		return nil, fmt.Errorf("probe failed")
	}
	return s.rows, nil
}

// notFoundEnvelopeRequest builds an envelope-negotiated request carrying
// wantRepos as the scoped grant (nil: an unscoped caller; empty: an empty
// grant).
func notFoundEnvelopeRequest(wantRepos []string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if wantRepos != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "tenant-a",
			AllowedRepositoryIDs: wantRepos,
		}))
	}
	return req
}

func notFoundFixtureRows() []querycontract.EntityContent {
	return []querycontract.EntityContent{
		{RepoID: "repo-b", RelativePath: "gitops/app.yaml", EntityType: "ArgoCDApplication", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "charts/payments/Chart.yaml", EntityType: "HelmChart", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "apps/payments.yaml", EntityType: "ArgoCDApplicationSet", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "deploy/svc.yaml", EntityType: "Deployment", EntityName: "payments"},
		{RepoID: "repo-a", RelativePath: "gitops/other.yaml", EntityType: "ArgoCDApplication", EntityName: "other"},
	}
}

type notFoundTestEnvelope struct {
	Code       string
	Details    map[string]any
	References []any
	Truncated  bool
}

func decodeNotFoundTestEnvelope(t *testing.T, body []byte) notFoundTestEnvelope {
	t.Helper()
	var raw struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("body is not an error envelope: %v; body = %s", err, body)
	}
	out := notFoundTestEnvelope{Code: raw.Error.Code, Details: raw.Error.Details}
	refs, _ := raw.Error.Details["references"].([]any)
	out.References = refs
	truncated, _ := raw.Error.Details["truncated"].(bool)
	out.Truncated = truncated
	return out
}

// TestWriteServiceNotFoundSortsAdmitsAndFilters pins the reference shape: the
// granted same-name deployment records sorted as file handles, with the
// wrong-type and wrong-name rows excluded even though the store returned
// them, and the ungranted repo-b record withheld from a scoped caller.
func TestWriteServiceNotFoundSortsAdmitsAndFilters(t *testing.T) {
	t.Parallel()

	store := &notFoundStubStore{rows: notFoundFixtureRows()}
	rec := httptest.NewRecorder()
	WriteServiceNotFound(rec, notFoundEnvelopeRequest([]string{"repo-a"}), "cap", "payments", store)

	got := decodeNotFoundTestEnvelope(t, rec.Body.Bytes())
	want := []any{
		map[string]any{"repo_id": "repo-a", "relative_path": "apps/payments.yaml", "entity_type": "ArgoCDApplicationSet", "entity_name": "payments"},
		map[string]any{"repo_id": "repo-a", "relative_path": "charts/payments/Chart.yaml", "entity_type": "HelmChart", "entity_name": "payments"},
	}
	if !reflect.DeepEqual(got.References, want) {
		t.Errorf("references = %v, want %v", got.References, want)
	}
	if got.Truncated {
		t.Error("truncated = true, want false")
	}
	for _, entityType := range serviceNotFoundReferenceTypes {
		if limit := store.limits[entityType]; limit != serviceNotFoundReferenceCap+1 {
			t.Errorf("type %s probed with limit %d, want %d", entityType, limit, serviceNotFoundReferenceCap+1)
		}
	}
}

// TestWriteServiceNotFoundCapsWithTruncated pins the bound: more granted rows
// than the cap surface exactly the cap, sorted, with truncated set.
func TestWriteServiceNotFoundCapsWithTruncated(t *testing.T) {
	t.Parallel()

	var rows []querycontract.EntityContent
	for i := 0; i < serviceNotFoundReferenceCap+5; i++ {
		rows = append(rows, querycontract.EntityContent{
			RepoID:       "repo-a",
			RelativePath: fmt.Sprintf("apps/%02d.yaml", i),
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments",
		})
	}
	store := &notFoundStubStore{rows: rows}
	rec := httptest.NewRecorder()
	WriteServiceNotFound(rec, notFoundEnvelopeRequest(nil), "cap", "payments", store)

	got := decodeNotFoundTestEnvelope(t, rec.Body.Bytes())
	if len(got.References) != serviceNotFoundReferenceCap {
		t.Fatalf("references holds %d handles, want %d", len(got.References), serviceNotFoundReferenceCap)
	}
	if !got.Truncated {
		t.Error("truncated = false, want true")
	}
	first, _ := got.References[0].(map[string]any)["relative_path"].(string)
	if first != "apps/00.yaml" {
		t.Errorf("first reference = %v, want apps/00.yaml (sorted)", first)
	}
}

// TestWriteServiceNotFoundFailClosed pins the zero-evidence floors: a reader
// failure fails its type closed while the surviving types still surface; a
// nil store and an empty service name probe nothing and answer no references.
func TestWriteServiceNotFoundFailClosed(t *testing.T) {
	t.Parallel()

	failing := &notFoundStubStore{
		rows:     notFoundFixtureRows(),
		errTypes: map[string]bool{"ArgoCDApplication": true, "ArgoCDApplicationSet": true},
	}
	rec := httptest.NewRecorder()
	WriteServiceNotFound(rec, notFoundEnvelopeRequest(nil), "cap", "payments", failing)
	got := decodeNotFoundTestEnvelope(t, rec.Body.Bytes())
	// The surviving HelmChart probe still surfaces every deployment-typed,
	// name-matching record; only the two failed types contribute nothing.
	if len(got.References) != 3 {
		t.Errorf("references holds %d handles, want 3 (failed types contribute nothing, the survivor still surfaces)", len(got.References))
	}
	if failing.calls != len(serviceNotFoundReferenceTypes) {
		t.Errorf("probed %d types, want %d (a failed type is skipped, not abortive)", failing.calls, len(serviceNotFoundReferenceTypes))
	}

	probeless := &notFoundStubStore{rows: notFoundFixtureRows()}
	for _, tc := range []struct {
		name  string
		store querycontract.ContentStore
		svc   string
	}{
		{"nil store", nil, "payments"},
		{"empty service name", probeless, ""},
	} {
		rec := httptest.NewRecorder()
		WriteServiceNotFound(rec, notFoundEnvelopeRequest(nil), "cap", tc.svc, tc.store)
		got := decodeNotFoundTestEnvelope(t, rec.Body.Bytes())
		if got.Code != string(querycontract.ErrorCodeServiceNotFound) {
			t.Errorf("%s: code = %q, want service_not_found", tc.name, got.Code)
		}
		if len(got.References) != 0 || got.Truncated {
			t.Errorf("%s: details = %v, want zero-evidence shape", tc.name, got.Details)
		}
	}
	if probeless.calls != 0 {
		t.Errorf("empty service name issued %d probes, want 0", probeless.calls)
	}
}

// TestWriteServiceNotFoundPlainAndEmptyGrantSkipTheProbe pins the two
// probe-free floors: a plain client (which cannot see details) and an empty
// grant answer the bare shape without touching the content store.
func TestWriteServiceNotFoundPlainAndEmptyGrantSkipTheProbe(t *testing.T) {
	t.Parallel()

	plain := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", nil)
	emptyGrant := notFoundEnvelopeRequest([]string{})
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"plain", plain},
		{"empty grant", emptyGrant},
	} {
		store := &notFoundStubStore{rows: notFoundFixtureRows()}
		rec := httptest.NewRecorder()
		WriteServiceNotFound(rec, tc.req, "cap", "payments", store)
		if store.calls != 0 {
			t.Errorf("%s: issued %d probes, want 0", tc.name, store.calls)
		}
		if tc.name == "plain" {
			const want = "{\"detail\":\"service not found\",\"error\":\"Not Found\"}\n"
			if rec.Body.String() != want {
				t.Errorf("plain body = %q, want %q", rec.Body.String(), want)
			}
			continue
		}
		got := decodeNotFoundTestEnvelope(t, rec.Body.Bytes())
		if len(got.References) != 0 || got.Truncated {
			t.Errorf("%s: details = %v, want zero-evidence shape", tc.name, got.Details)
		}
	}
}
