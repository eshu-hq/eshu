// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// scriptedCollectorReadinessStore answers the collector-configured probe
// with a fixed configured bit or a fixed error.
type scriptedCollectorReadinessStore struct {
	configured bool
	err        error
}

func (s scriptedCollectorReadinessStore) CollectorConfigured(context.Context, scope.CollectorKind) (bool, error) {
	return s.configured, s.err
}

// serveCollectorReadinessPage serves one gated list route with no scoped-token
// grants, so both handlers take their empty-grant short-circuit page straight
// into the attach step, and returns the decoded collector_readiness envelope
// (nil when the attach step leaves the key off).
func serveCollectorReadinessPage(t *testing.T, mux *http.ServeMux, target string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	// Empty scoped grants: both handlers take their empty-grant
	// short-circuit page straight into the attach step. (No auth context
	// would mean all-scopes in this binary, which sails past the
	// short-circuit into the stores.)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(),
		queryauth.AuthContext{Mode: queryauth.AuthModeScoped}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want %d: %s", target, rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s: decode body: %v", target, err)
	}
	env, ok := body["collector_readiness"]
	if !ok {
		return nil
	}
	envMap, ok := env.(map[string]any)
	if !ok {
		t.Fatalf("GET %s: collector_readiness is %T, want object", target, env)
	}
	return envMap
}

// normalizeReadinessEnvelope drops the route-specific collector kind so the
// readiness STATE machine of two families can be compared.
func normalizeReadinessEnvelope(env map[string]any) map[string]any {
	if env == nil {
		return nil
	}
	out := make(map[string]any, len(env))
	for k, v := range env {
		if k == "collector_kind" {
			continue
		}
		out[k] = v
	}
	return out
}

// TestCollectorListReadinessMatchesHub pins the two family-local copies of
// the attach step -- root's attachCollectorListReadiness and the supplychain
// hub's -- to identical envelopes over the shared probe matrix. Root's
// collector_list_readiness.go deliberately keeps request-time orchestration
// out of the dependency-neutral leaf (prior review decision), so each family
// owns its copy; this test is the drift tripwire (#6542 review). A nil store
// leaves the key off on both sides; otherwise both sides must agree on the
// readiness state and counts for an empty page.
func TestCollectorListReadinessMatchesHub(t *testing.T) {
	t.Parallel()

	errProbe := errors.New("probe boom")
	cases := []struct {
		name       string
		store      querycontract.CollectorListReadinessStore
		wantAbsent bool
	}{
		{"nil store leaves key off", nil, true},
		{"empty page configured probe", scriptedCollectorReadinessStore{configured: true}, false},
		{"empty page unconfigured probe", scriptedCollectorReadinessStore{configured: false}, false},
		{"empty page erroring probe", scriptedCollectorReadinessStore{err: errProbe}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hubHandler := &supplychain.SupplyChainHandler{
				Profile:            querycontract.ProfileProduction,
				CollectorReadiness: tc.store,
			}
			hubMux := http.NewServeMux()
			hubHandler.Mount(hubMux)
			hubEnv := serveCollectorReadinessPage(t, hubMux,
				"/api/v0/supply-chain/sbom-attestations/attachments?limit=10&subject_digest=sha256:abc")

			rootHandler := &CICDHandler{CollectorReadiness: tc.store}
			rootMux := http.NewServeMux()
			rootHandler.Mount(rootMux)
			rootEnv := serveCollectorReadinessPage(t, rootMux,
				"/api/v0/ci-cd/run-correlations?limit=10&repository_id=repo-1")

			if tc.wantAbsent {
				if hubEnv != nil {
					t.Fatalf("hub envelope = %v, want absent for nil store", hubEnv)
				}
				if rootEnv != nil {
					t.Fatalf("root envelope = %v, want absent for nil store", rootEnv)
				}
				return
			}
			if hubEnv == nil || rootEnv == nil {
				t.Fatalf("hub envelope = %v, root envelope = %v, want both present", hubEnv, rootEnv)
			}
			if !reflect.DeepEqual(normalizeReadinessEnvelope(hubEnv), normalizeReadinessEnvelope(rootEnv)) {
				t.Fatalf("readiness drift: hub = %v, root = %v", hubEnv, rootEnv)
			}
		})
	}
}
