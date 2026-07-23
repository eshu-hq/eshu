// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingContainerImageIdentityStore struct {
	rows       []ContainerImageIdentityRow
	lastFilter ContainerImageIdentityFilter
}

func (s *recordingContainerImageIdentityStore) ListContainerImageIdentities(
	_ context.Context,
	filter ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	s.lastFilter = filter
	return append([]ContainerImageIdentityRow(nil), s.rows...), nil
}

func TestSupplyChainListContainerImageIdentitiesRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ContainerImageIdentities: &recordingContainerImageIdentityStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/container-images/identities?limit=10",
		"/api/v0/supply-chain/container-images/identities?digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSupplyChainListContainerImageIdentitiesRejectsUnsupportedOutcome(t *testing.T) {
	t.Parallel()

	store := &recordingContainerImageIdentityStore{}
	handler := &SupplyChainHandler{ContainerImageIdentities: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities?outcome=ambiguous_tag&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := w.Body.String(), "outcome must be exact_digest or tag_resolved"; !strings.Contains(got, want) {
		t.Fatalf("body = %s, want %q", got, want)
	}
	if store.lastFilter.Outcome != "" {
		t.Fatalf("store was called with outcome %q, want no store call", store.lastFilter.Outcome)
	}
}

func TestSupplyChainListContainerImageIdentitiesUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingContainerImageIdentityStore{
		rows: []ContainerImageIdentityRow{
			{
				IdentityID:               "identity-1",
				Digest:                   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ImageRef:                 "registry.example.com/team/api:prod",
				RepositoryID:             "oci-registry://registry.example.com/team/api",
				Outcome:                  "tag_resolved",
				Reason:                   "single active tag observation resolved image reference",
				IdentityStrength:         "tag_observation_with_digest",
				SourceRevision:           "abc123def456",
				SourceRevisionProvenance: "ci_run_commit",
				CanonicalWrites:          1,
				CanonicalID:              "canonical:container_image_identity:scope:generation:image:tag_resolved",
				SourceLayers:             []string{"source_declaration", "observed_resource"},
				EvidenceFactIDs:          []string{"content-entity-1", "oci-tag-1"},
				SourceFreshness:          "active",
				SourceConfidence:         "inferred",
			},
			{IdentityID: "identity-2", Outcome: "tag_resolved"},
		},
	}
	handler := &SupplyChainHandler{ContainerImageIdentities: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities?digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.Digest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("Digest = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Identities []ContainerImageIdentityResult `json:"identities"`
		Count      int                            `json:"count"`
		Limit      int                            `json:"limit"`
		Truncated  bool                           `json:"truncated"`
		NextCursor map[string]string              `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Identities), 1; got != want {
		t.Fatalf("len(identities) = %d, want %d", got, want)
	}
	if got, want := resp.Identities[0].IdentityStrength, "tag_observation_with_digest"; got != want {
		t.Fatalf("IdentityStrength = %q, want %q", got, want)
	}
	if got, want := resp.Identities[0].SourceRevision, "abc123def456"; got != want {
		t.Fatalf("SourceRevision = %q, want %q", got, want)
	}
	if got, want := resp.Identities[0].SourceRevisionProvenance, "ci_run_commit"; got != want {
		t.Fatalf("SourceRevisionProvenance = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_identity_id"], "identity-1"; got != want {
		t.Fatalf("next_cursor.after_identity_id = %q, want %q", got, want)
	}
}

func TestPostgresContainerImageIdentityStoreReportsPaginationLimit(t *testing.T) {
	t.Parallel()

	store := NewPostgresContainerImageIdentityStore(unusedSupplyChainImpactFindingQueryer{})
	_, err := store.ListContainerImageIdentities(context.Background(), ContainerImageIdentityFilter{
		Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Limit:  containerImageIdentityMaxLimit + 2,
	})
	if err == nil {
		t.Fatal("ListContainerImageIdentities() error = nil, want limit error")
	}
	want := fmt.Sprintf("limit must be between 1 and %d for internal pagination", containerImageIdentityMaxLimit+1)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestContainerImageIdentityQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'digest' = $2",
		"fact.payload->>'image_ref' = $3",
		"fact.payload->'source_repository_ids' ? $4",
		"fact.payload->>'repository_id' = $5",
		"fact.payload->>'outcome' = $6",
		"fact.fact_id > $7",
	} {
		if !strings.Contains(listContainerImageIdentitiesQuery, want) {
			t.Fatalf("listContainerImageIdentitiesQuery missing %q:\n%s", want, listContainerImageIdentitiesQuery)
		}
	}
}

func TestExplainContainerImageCandidateQueryUsesBoundedOCIScopeReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.scope_id = $1",
		"scope.collector_kind = 'oci_registry'",
		"scope.scope_kind = 'container_registry_repository'",
		"FROM workflow_work_items AS work",
		"work.collector_kind = 'oci_registry'",
		"work.scope_id = $1",
		"fact.fact_kind = 'oci_registry.warning'",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'repository_id' = $1",
		"LIMIT 1",
	} {
		if !strings.Contains(explainContainerImageCandidateQuery, want) {
			t.Fatalf("explainContainerImageCandidateQuery missing %q:\n%s", want, explainContainerImageCandidateQuery)
		}
	}
}
