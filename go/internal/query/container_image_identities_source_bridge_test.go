// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestSupplyChainListContainerImageIdentitiesUsesSourceRepositoryBridge(t *testing.T) {
	t.Parallel()

	sourceRepoID := "repo://example/payments-api"
	store := &recordingContainerImageIdentityStore{
		rows: []ContainerImageIdentityRow{
			{
				IdentityID:          "identity-1",
				Digest:              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ImageRef:            "registry.example.com/team/payments-api:prod",
				RepositoryID:        "oci-registry://registry.example.com/team/payments-api",
				SourceRepositoryIDs: []string{sourceRepoID},
				Outcome:             "tag_resolved",
				IdentityStrength:    "tag_observation_with_digest",
				SourceLayers:        []string{"source_declaration", "observed_resource"},
				EvidenceFactIDs:     []string{"content-entity-1", "oci-tag-1"},
				SourceFreshness:     "active",
				SourceConfidence:    "inferred",
			},
		},
	}
	handler := &SupplyChainHandler{ContainerImageIdentities: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities?source_repository_id="+sourceRepoID+"&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.SourceRepositoryID, sourceRepoID; got != want {
		t.Fatalf("SourceRepositoryID = %q, want %q", got, want)
	}
	if store.lastFilter.RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty OCI repository filter", store.lastFilter.RepositoryID)
	}

	var resp struct {
		Identities   []ContainerImageIdentityResult     `json:"identities"`
		SourceBridge ContainerImageIdentitySourceBridge `json:"source_bridge"`
		Count        int                                `json:"count"`
		Limit        int                                `json:"limit"`
		Truncated    bool                               `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.Identities[0].RepositoryID, "oci-registry://registry.example.com/team/payments-api"; got != want {
		t.Fatalf("identity repository_id = %q, want OCI repository %q", got, want)
	}
	if !slices.Contains(resp.Identities[0].SourceRepositoryIDs, sourceRepoID) {
		t.Fatalf("source_repository_ids = %#v, want %q", resp.Identities[0].SourceRepositoryIDs, sourceRepoID)
	}
	if got, want := resp.SourceBridge.SourceRepositoryID, sourceRepoID; got != want {
		t.Fatalf("source_bridge.source_repository_id = %q, want %q", got, want)
	}
	if !slices.Contains(resp.SourceBridge.ImageRepositoryIDs, "oci-registry://registry.example.com/team/payments-api") {
		t.Fatalf("source_bridge.image_repository_ids = %#v, want OCI repository", resp.SourceBridge.ImageRepositoryIDs)
	}
	if len(resp.SourceBridge.MissingEvidence) != 0 {
		t.Fatalf("source_bridge.missing_evidence = %#v, want empty", resp.SourceBridge.MissingEvidence)
	}
}

func TestContainerImageSourceBridgeMissingEvidenceMatrix(t *testing.T) {
	t.Parallel()

	sourceRepoID := "repo://example/payments-api"
	fullRow := ContainerImageIdentityResult{
		IdentityID:          "identity-full",
		Digest:              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ImageRef:            "registry.example.com/team/payments-api:prod",
		RepositoryID:        "oci-registry://registry.example.com/team/payments-api",
		SourceRepositoryIDs: []string{sourceRepoID},
	}

	for _, tc := range []struct {
		name         string
		rows         []ContainerImageIdentityResult
		wantMissing  []string
		wantWarnings []string
	}{
		{
			name: "deployment manifest only",
			rows: []ContainerImageIdentityResult{{
				IdentityID:          "identity-manifest-only",
				ImageRef:            "registry.example.com/team/payments-api:prod",
				SourceRepositoryIDs: []string{sourceRepoID},
			}},
			wantMissing: []string{"image_registry_observation_missing"},
		},
		{
			name: "oci observation only",
			rows: []ContainerImageIdentityResult{{
				IdentityID:   "identity-oci-only",
				Digest:       "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				RepositoryID: "oci-registry://registry.example.com/team/payments-api",
			}},
			wantMissing: []string{"deployment_image_reference_missing", "source_to_image_correlation_missing"},
		},
		{
			name: "cloud image reference only",
			rows: []ContainerImageIdentityResult{{
				IdentityID:          "identity-cloud-only",
				Digest:              "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				ImageRef:            "123456789012.dkr.ecr.us-east-1.amazonaws.com/payments-api@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				SourceRepositoryIDs: []string{sourceRepoID},
			}},
			wantMissing: []string{"image_registry_observation_missing"},
		},
		{
			name:        "full bridge",
			rows:        []ContainerImageIdentityResult{fullRow},
			wantMissing: nil,
		},
		{
			name: "ambiguous image repository",
			rows: []ContainerImageIdentityResult{
				fullRow,
				{
					IdentityID:          "identity-other-repo",
					Digest:              "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
					ImageRef:            "registry.example.com/team/payments-worker:prod",
					RepositoryID:        "oci-registry://registry.example.com/team/payments-worker",
					SourceRepositoryIDs: []string{sourceRepoID},
				},
			},
			wantWarnings: []string{"ambiguous_image_repository"},
		},
		{
			name:        "no evidence",
			rows:        nil,
			wantMissing: []string{"deployment_image_reference_missing", "image_registry_observation_missing", "source_to_image_correlation_missing"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildContainerImageIdentitySourceBridge(sourceRepoID, tc.rows)
			for _, want := range tc.wantMissing {
				if !slices.Contains(got.MissingEvidence, want) {
					t.Fatalf("missing_evidence = %#v, want %q", got.MissingEvidence, want)
				}
			}
			if len(tc.wantMissing) == 0 && len(got.MissingEvidence) != 0 {
				t.Fatalf("missing_evidence = %#v, want empty", got.MissingEvidence)
			}
			for _, want := range tc.wantWarnings {
				if !slices.Contains(got.Warnings, want) {
					t.Fatalf("warnings = %#v, want %q", got.Warnings, want)
				}
			}
		})
	}
}

func TestContainerImageIdentityQueryUsesSourceRepositoryAnchor(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
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
