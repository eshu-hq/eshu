// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fakeCollectorListReadinessStore is a deterministic configured-probe double.
// configured is the boolean every probe returns; err, when set, forces the
// readiness_unavailable path.
type fakeCollectorListReadinessStore struct {
	configured bool
	err        error
	lastKind   scope.CollectorKind
	calls      int
}

func (s *fakeCollectorListReadinessStore) CollectorConfigured(
	_ context.Context,
	kind scope.CollectorKind,
) (bool, error) {
	s.calls++
	s.lastKind = kind
	if s.err != nil {
		return false, s.err
	}
	return s.configured, nil
}

// collectorReadinessFromResponse extracts the collector_readiness envelope from a
// gated list response body.
func collectorReadinessFromResponse(t *testing.T, body []byte) CollectorListReadinessEnvelope {
	t.Helper()
	var resp struct {
		CollectorReadiness CollectorListReadinessEnvelope `json:"collector_readiness"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, body)
	}
	return resp.CollectorReadiness
}

// gatedListReadinessCase mounts one of the 7 gated list handlers with an empty
// page and the given configured probe, then issues request.
type gatedListReadinessCase struct {
	name    string
	kind    scope.CollectorKind
	request string
	mount   func(store CollectorListReadinessStore) http.Handler
}

func gatedListReadinessCases() []gatedListReadinessCase {
	emptyGraph := func() *recordingPackageRegistryGraphReader {
		return &recordingPackageRegistryGraphReader{runRows: []map[string]any{}}
	}
	return []gatedListReadinessCase{
		{
			name:    "list_sbom_attestation_attachments",
			kind:    scope.CollectorSBOMAttestation,
			request: "/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo://example/api&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &SupplyChainHandler{SBOMAttachments: &recordingSBOMAttestationAttachmentStore{}, CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_container_image_identities",
			kind:    scope.CollectorOCIRegistry,
			request: "/api/v0/supply-chain/container-images/identities?digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &SupplyChainHandler{ContainerImageIdentities: &recordingContainerImageIdentityStore{}, CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_package_registry_packages",
			kind:    scope.CollectorPackageRegistry,
			request: "/api/v0/package-registry/packages?ecosystem=npm&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &PackageRegistryHandler{Neo4j: emptyGraph(), CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_package_registry_versions",
			kind:    scope.CollectorPackageRegistry,
			request: "/api/v0/package-registry/versions?package_id=pkg:npm://registry.example/team-api&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &PackageRegistryHandler{Neo4j: emptyGraph(), CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_package_registry_dependencies",
			kind:    scope.CollectorPackageRegistry,
			request: "/api/v0/package-registry/dependencies?package_id=pkg:npm://registry.example/team-api&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &PackageRegistryHandler{Neo4j: emptyGraph(), CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_package_registry_correlations",
			kind:    scope.CollectorPackageRegistry,
			request: "/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &PackageRegistryHandler{Correlations: &recordingPackageRegistryCorrelationStore{}, CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
		{
			name:    "list_ci_cd_run_correlations",
			kind:    scope.CollectorCICDRun,
			request: "/api/v0/ci-cd/run-correlations?image_ref=registry.example.com/team/api:prod&limit=10",
			mount: func(store CollectorListReadinessStore) http.Handler {
				h := &CICDHandler{Correlations: &recordingCICDRunCorrelationStore{}, CollectorReadiness: store}
				mux := http.NewServeMux()
				h.Mount(mux)
				return mux
			},
		},
	}
}

func TestGatedListToolsReportNotConfiguredWhenCollectorDisabled(t *testing.T) {
	t.Parallel()

	for _, tc := range gatedListReadinessCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeCollectorListReadinessStore{configured: false}
			w := httptest.NewRecorder()
			tc.mount(store).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			env := collectorReadinessFromResponse(t, w.Body.Bytes())
			if env.State != CollectorListReadinessStateNotConfigured {
				t.Fatalf("readiness_state = %q, want %q", env.State, CollectorListReadinessStateNotConfigured)
			}
			if env.CollectorKind != string(tc.kind) {
				t.Fatalf("collector_kind = %q, want %q", env.CollectorKind, tc.kind)
			}
			if store.lastKind != tc.kind {
				t.Fatalf("probed kind = %q, want %q", store.lastKind, tc.kind)
			}
		})
	}
}

func TestGatedListToolsReportReadyZeroResultsWhenCollectorConfigured(t *testing.T) {
	t.Parallel()

	for _, tc := range gatedListReadinessCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeCollectorListReadinessStore{configured: true}
			w := httptest.NewRecorder()
			tc.mount(store).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			env := collectorReadinessFromResponse(t, w.Body.Bytes())
			if env.State != CollectorListReadinessStateReadyZeroResults {
				t.Fatalf("readiness_state = %q, want %q", env.State, CollectorListReadinessStateReadyZeroResults)
			}
		})
	}
}

func TestGatedListToolsReportReadinessUnavailableOnProbeError(t *testing.T) {
	t.Parallel()

	for _, tc := range gatedListReadinessCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeCollectorListReadinessStore{err: fmt.Errorf("probe boom")}
			w := httptest.NewRecorder()
			tc.mount(store).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			env := collectorReadinessFromResponse(t, w.Body.Bytes())
			if env.State != CollectorListReadinessStateReadinessUnavailable {
				t.Fatalf("readiness_state = %q, want %q", env.State, CollectorListReadinessStateReadinessUnavailable)
			}
		})
	}
}

func TestGatedListToolsOmitReadinessWhenStoreUnset(t *testing.T) {
	t.Parallel()

	for _, tc := range gatedListReadinessCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			tc.mount(nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if _, ok := resp["collector_readiness"]; ok {
				t.Fatalf("collector_readiness present with nil store; body = %s", w.Body.String())
			}
		})
	}
}
