// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// secretsIAMEndpoints is the full secrets/IAM read surface: each capability,
// its HTTP route, and its MCP tool name. The contract tests below assert these
// stay in lockstep (capability matrix, OpenAPI, profile gating) so a new
// endpoint cannot ship half-wired.
var secretsIAMEndpoints = []struct {
	capability string
	path       string
	tool       string
}{
	{secretsIAMIdentityTrustChainsCapability, "/api/v0/secrets-iam/identity-trust-chains", "list_secrets_iam_identity_trust_chains"},
	{secretsIAMPrivilegePostureObservationsCapability, "/api/v0/secrets-iam/privilege-posture-observations", "list_secrets_iam_privilege_posture_observations"},
	{secretsIAMSecretAccessPathsCapability, "/api/v0/secrets-iam/secret-access-paths", "list_secrets_iam_secret_access_paths"},
	{secretsIAMPostureGapsCapability, "/api/v0/secrets-iam/posture-gaps", "list_secrets_iam_posture_gaps"},
	{secretsIAMPostureSummaryCapability, "/api/v0/secrets-iam/posture-summary", "count_secrets_iam_posture"},
}

func fullSecretsIAMHandler(profile QueryProfile) *SecretsIAMHandler {
	return &SecretsIAMHandler{
		IdentityTrustChains:          &recordingSecretsIAMIdentityTrustChainStore{},
		PrivilegePostureObservations: &recordingPrivilegePostureStore{},
		SecretAccessPaths:            &recordingSecretAccessPathStore{},
		PostureGaps:                  &recordingPostureGapStore{},
		Summary:                      &recordingPostureSummaryStore{},
		Profile:                      profile,
	}
}

// TestSecretsIAMEndpointsUnsupportedOnLocalLightweight proves every secrets/IAM
// endpoint returns unsupported_capability (501) on the local-lightweight
// profile even when its store is wired, because the read model is reducer-owned
// and unavailable without the authoritative profile. This is the contract the
// capability matrix promises (local_lightweight: unsupported).
func TestSecretsIAMEndpointsUnsupportedOnLocalLightweight(t *testing.T) {
	t.Parallel()

	handler := fullSecretsIAMHandler(ProfileLocalLightweight)
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, ep := range secretsIAMEndpoints {
		ep := ep
		t.Run(ep.capability, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, ep.path+"?scope_id=s&limit=10", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusNotImplemented; got != want {
				t.Fatalf("%s: status = %d, want %d (unsupported on local_lightweight); body = %s",
					ep.capability, got, want, w.Body.String())
			}
		})
	}
}

// TestSecretsIAMEndpointsSupportedOnAuthoritative proves the same endpoints are
// served (not 501) once the profile is authoritative and the store is wired —
// guarding against a regression that left a capability unsupported everywhere.
func TestSecretsIAMEndpointsSupportedOnAuthoritative(t *testing.T) {
	t.Parallel()

	handler := fullSecretsIAMHandler(ProfileLocalAuthoritative)
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, ep := range secretsIAMEndpoints {
		ep := ep
		t.Run(ep.capability, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, ep.path+"?scope_id=s&limit=10", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code == http.StatusNotImplemented {
				t.Fatalf("%s: got 501 on authoritative profile, want it served", ep.capability)
			}
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("%s: status = %d, want %d; body = %s", ep.capability, got, want, w.Body.String())
			}
		})
	}
}

// TestSecretsIAMCapabilitiesHaveMatrixAndOpenAPI proves every secrets/IAM
// capability is registered in the Go capability matrix and exposed by a path in
// the OpenAPI spec, so a capability cannot exist without a documented wire
// contract.
func TestSecretsIAMCapabilitiesHaveMatrixAndOpenAPI(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, ep := range secretsIAMEndpoints {
		if _, ok := capabilityMatrix[ep.capability]; !ok {
			t.Errorf("capability %q missing from capabilityMatrix", ep.capability)
		}
		if !strings.Contains(spec, `"`+ep.path+`"`) {
			t.Errorf("OpenAPI spec missing path %q", ep.path)
		}
	}
}

// TestSecretsIAMEndpointsRejectMissingAnchor proves every endpoint rejects a
// request with a limit but no scope anchor (400), so no endpoint can issue an
// unbounded read.
func TestSecretsIAMEndpointsRejectMissingAnchor(t *testing.T) {
	t.Parallel()

	handler := fullSecretsIAMHandler(ProfileProduction)
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, ep := range secretsIAMEndpoints {
		ep := ep
		t.Run(ep.capability, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, ep.path+"?limit=10", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("%s: status = %d, want %d for missing anchor", ep.capability, got, want)
			}
		})
	}
}
