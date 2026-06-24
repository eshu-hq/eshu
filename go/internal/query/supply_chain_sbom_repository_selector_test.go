// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type canonicalRepositorySBOMAttachmentStore struct {
	page       SBOMAttestationAttachmentPage
	lastFilter SBOMAttestationAttachmentFilter
	calls      int
}

func (s *canonicalRepositorySBOMAttachmentStore) ListSBOMAttestationAttachments(
	_ context.Context,
	filter SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	s.calls++
	s.lastFilter = filter
	if filter.RepositoryID != "repo://example/api" {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("repository_id = %q, want repo://example/api", filter.RepositoryID)
	}
	page := s.page
	page.Attachments = append([]SBOMAttestationAttachmentRow(nil), s.page.Attachments...)
	page.MissingEvidence = append([]string(nil), s.page.MissingEvidence...)
	return page, nil
}

func TestSupplyChainListSBOMAttestationAttachmentsResolvesRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		selector   string
		wantLookup int
	}{
		{name: "internal id", selector: "repo://example/api", wantLookup: 0},
		{name: "name", selector: "payments-api", wantLookup: 1},
		{name: "slug", selector: "example/payments-api", wantLookup: 1},
		{name: "path", selector: "/srv/payments-api", wantLookup: 1},
		{name: "remote url", selector: "https://github.com/example/payments-api.git", wantLookup: 1},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := selectorSBOMContentStore()
			store := &canonicalRepositorySBOMAttachmentStore{
				page: SBOMAttestationAttachmentPage{
					Attachments: []SBOMAttestationAttachmentRow{{
						AttachmentID:     "attachment-1",
						AttachmentStatus: "attached_verified",
						RepositoryIDs:    []string{"repo://example/api"},
					}},
				},
			}
			handler := &SupplyChainHandler{
				Content:         content,
				SBOMAttachments: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/sbom-attestations/attachments?"+url.Values{
					"repository_id": []string{tc.selector},
					"limit":         []string{"10"},
				}.Encode(),
				nil,
			)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got := content.matchCalls; got != tc.wantLookup {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantLookup)
			}

			var resp struct {
				Attachments []SBOMAttestationAttachmentResult `json:"attachments"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got := len(resp.Attachments); got != 1 {
				t.Fatalf("len(attachments) = %d, want 1", got)
			}
		})
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsRejectsInvalidRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		content      *countingRepositoryContentStore
		wantStatus   int
		wantMessages []string
	}{
		{
			name:         "unknown",
			content:      selectorSBOMContentStore(),
			wantStatus:   http.StatusNotFound,
			wantMessages: []string{"repository selector", "unknown-repo", "did not match"},
		},
		{
			name: "ambiguous",
			content: &countingRepositoryContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{
						{ID: "repo://example/api", Name: "payments-api"},
						{ID: "repo://example/worker", Name: "payments-api"},
					},
				},
			},
			wantStatus:   http.StatusBadRequest,
			wantMessages: []string{"repository selector", "payments-api", "matched multiple repositories"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingSBOMAttestationAttachmentStore{}
			handler := &SupplyChainHandler{
				Content:         tc.content,
				SBOMAttachments: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			selector := "unknown-repo"
			if tc.name == "ambiguous" {
				selector = "payments-api"
			}
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/sbom-attestations/attachments?"+url.Values{
					"repository_id": []string{selector},
					"limit":         []string{"10"},
				}.Encode(),
				nil,
			)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got := w.Code; got != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", got, tc.wantStatus, w.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0 for invalid selector", store.calls)
			}
			body := w.Body.String()
			for _, want := range tc.wantMessages {
				if !strings.Contains(body, want) {
					t.Fatalf("body = %q, want substring %q", body, want)
				}
			}
		})
	}
}

func TestSBOMAttestationAttachmentAggregateRoutesResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		wantCounts int
		wantInvs   int
		wantLookup int
	}{
		{
			name:       "count internal id",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo://example/api",
			wantCounts: 1,
			wantLookup: 0,
		},
		{
			name:       "count repository name",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=payments-api",
			wantCounts: 1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository slug",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=example/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository path",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=/srv/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := selectorSBOMContentStore()
			store := &stubSBOMAttestationAttachmentAggregateStore{
				count: SBOMAttestationAttachmentAggregateCount{
					TotalAttachments:   1,
					ByAttachmentStatus: map[string]int{"attached_verified": 1},
					ByArtifactKind:     map[string]int{"sbom": 1},
				},
				inventory: []SBOMAttestationAttachmentInventoryRow{{
					Dimension: SBOMAttestationAttachmentInventoryByAttachmentStatus,
					Value:     "attached_verified",
					Count:     1,
				}},
			}
			handler := &SupplyChainHandler{
				Content:                  content,
				SBOMAttachmentAggregates: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got := store.countCalls; got != tc.wantCounts {
				t.Fatalf("Count calls = %d, want %d", got, tc.wantCounts)
			}
			if got := store.invCalls; got != tc.wantInvs {
				t.Fatalf("Inventory calls = %d, want %d", got, tc.wantInvs)
			}
			if got := content.matchCalls; got != tc.wantLookup {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantLookup)
			}
		})
	}
}

func TestSBOMAttestationAttachmentAggregateRoutesRejectInvalidRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		content    *countingRepositoryContentStore
		wantStatus int
	}{
		{
			name:       "count unknown",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=unknown-repo",
			content:    selectorSBOMContentStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "inventory unknown",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=unknown-repo&limit=10",
			content:    selectorSBOMContentStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name: "count ambiguous",
			target: "/api/v0/supply-chain/sbom-attestations/attachments/count" +
				"?repository_id=payments-api",
			content: &countingRepositoryContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{
						{ID: "repo://example/api", Name: "payments-api"},
						{ID: "repo://example/worker", Name: "payments-api"},
					},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &stubSBOMAttestationAttachmentAggregateStore{}
			handler := &SupplyChainHandler{
				Content:                  tc.content,
				SBOMAttachmentAggregates: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if got := w.Code; got != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", got, tc.wantStatus, w.Body.String())
			}
			if store.countCalls != 0 || store.invCalls != 0 {
				t.Fatalf("store called for invalid selector (count=%d inventory=%d)", store.countCalls, store.invCalls)
			}
			if body := w.Body.String(); !strings.Contains(body, "repository selector") {
				t.Fatalf("body = %q, want repository selector error", body)
			}
		})
	}
}

func selectorSBOMContentStore() *countingRepositoryContentStore {
	return &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo://example/api",
				Name:      "payments-api",
				LocalPath: "/srv/payments-api",
				RemoteURL: "https://github.com/example/payments-api.git",
				RepoSlug:  "example/payments-api",
				HasRemote: true,
			}},
		},
	}
}
