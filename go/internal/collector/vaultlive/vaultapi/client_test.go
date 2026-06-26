// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// mockVault is an httptest handler that serves canned metadata responses and
// records every requested path so tests can assert the adapter stays
// metadata-only.
type mockVault struct {
	mu      sync.Mutex
	paths   []string
	queries []string
}

func (m *mockVault) recordRequest(r *http.Request) {
	m.mu.Lock()
	m.paths = append(m.paths, r.URL.Path)
	m.queries = append(m.queries, r.URL.RawQuery)
	m.mu.Unlock()
}

func (m *mockVault) requestedQueries() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.queries...)
}

func (m *mockVault) requested() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.paths...)
}

func newMockVault(t *testing.T) (*httptest.Server, *mockVault) {
	t.Helper()
	m := &mockVault{}
	responses := map[string]string{
		"GET /v1/sys/auth":                          `{"data":{"kubernetes/":{"type":"kubernetes","accessor":"auth_k8s","local":false,"config":{"default_lease_ttl":3600,"max_lease_ttl":7200}}}}`,
		"GET /v1/sys/mounts":                        `{"data":{"secret/":{"type":"kv","accessor":"kv_acc","options":{"version":"2"}}}}`,
		"LIST /v1/sys/policies/acl":                 `{"data":{"keys":["payments-read"]}}`,
		"GET /v1/sys/policies/acl/payments-read":    `{"data":{"name":"payments-read","policy":"path \"secret/metadata/payments\" { capabilities = [\"read\"] }"}}`,
		"LIST /v1/auth/kubernetes/role":             `{"data":{"keys":["payments-api"]}}`,
		"GET /v1/auth/kubernetes/role/payments-api": `{"data":{"bound_service_account_names":["payments"],"bound_service_account_namespaces":["prod"],"token_policies":["payments-read"],"token_ttl":3600}}`,
		"LIST /v1/identity/entity/id":               `{"data":{"keys":["ent-1"]}}`,
		"GET /v1/identity/entity/id/ent-1":          `{"data":{"name":"payments","aliases":[{}],"group_ids":["g1"],"disabled":false}}`,
		"LIST /v1/identity/entity-alias/id":         `{"data":{"keys":["alias-1"]}}`,
		"GET /v1/identity/entity-alias/id/alias-1":  `{"data":{"canonical_id":"ent-1","mount_accessor":"auth_k8s","mount_path":"auth/kubernetes/","name":"payments"}}`,
		"LIST /v1/secret/metadata":                  `{"data":{"keys":["db","app/"]}}`,
		"LIST /v1/secret/metadata/app":              `{"data":{"keys":["config"]}}`,
		"GET /v1/secret/metadata/db":                `{"data":{"current_version":3,"max_versions":10,"cas_required":true,"delete_version_after":"0s","custom_metadata":{"owner":"team"}}}`,
		"GET /v1/secret/metadata/app/config":        `{"data":{"current_version":1}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.recordRequest(r)
		method := r.Method
		if r.URL.Query().Get("list") == "true" {
			method = "LIST"
		}
		if body, ok := responses[method+" "+r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func newTestAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a, err := New(Config{Address: srv.URL, Token: "test-token", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestAdapterMapsAllFamiliesAndNeverReadsData(t *testing.T) {
	t.Parallel()
	srv, mock := newMockVault(t)
	a := newTestAdapter(t, srv)
	ctx := context.Background()

	mounts, err := a.ListAuthMounts(ctx)
	if err != nil || len(mounts) != 1 || mounts[0].Method != "kubernetes" || mounts[0].DefaultLeaseTTLSeconds != 3600 {
		t.Fatalf("ListAuthMounts = %+v, err=%v", mounts, err)
	}
	roles, err := a.ListAuthRoles(ctx)
	if err != nil || len(roles) != 1 || roles[0].RoleName != "payments-api" ||
		len(roles[0].BoundServiceAccountNames) != 1 || roles[0].TokenTTLSeconds != 3600 {
		t.Fatalf("ListAuthRoles = %+v, err=%v", roles, err)
	}
	policies, err := a.ListACLPolicies(ctx)
	if err != nil || len(policies) != 1 || policies[0].PolicyName != "payments-read" ||
		!strings.HasPrefix(policies[0].PolicyHash, "sha256:") {
		t.Fatalf("ListACLPolicies = %+v, err=%v", policies, err)
	}
	entities, err := a.ListIdentityEntities(ctx)
	if err != nil || len(entities) != 1 || entities[0].EntityName != "payments" || entities[0].AliasCount != 1 {
		t.Fatalf("ListIdentityEntities = %+v, err=%v", entities, err)
	}
	aliases, err := a.ListIdentityAliases(ctx)
	if err != nil || len(aliases) != 1 || aliases[0].EntityID != "ent-1" ||
		aliases[0].MountAccessor != "auth_k8s" || aliases[0].MountPath != "auth/kubernetes/" {
		t.Fatalf("ListIdentityAliases = %+v, err=%v (mount anchors are the downstream join fields)", aliases, err)
	}
	engines, err := a.ListSecretEngineMounts(ctx)
	if err != nil || len(engines) != 1 || engines[0].MountType != "kv" || engines[0].KVVersion != "2" {
		t.Fatalf("ListSecretEngineMounts = %+v, err=%v", engines, err)
	}
	kv, err := a.ListKVMetadata(ctx)
	if err != nil || len(kv) != 2 {
		t.Fatalf("ListKVMetadata len = %d, err=%v: %+v", len(kv), err, kv)
	}
	// Recursive walk reached the nested path and the policy body was hashed away.
	var paths []string
	for _, m := range kv {
		paths = append(paths, m.Path)
	}
	if got := strings.Join(paths, ","); got != "app/config,db" {
		t.Fatalf("kv paths = %q, want app/config,db", got)
	}

	// The metadata-only guarantee: no request ever touched a KV data path.
	for _, p := range mock.requested() {
		if isKVDataPath(strings.TrimPrefix(p, "/v1/")) {
			t.Fatalf("adapter requested a KV data path %q", p)
		}
	}
}

// hostileVault serves a KV metadata LIST whose keys are attacker-controlled,
// to prove the adapter neither traverses out of the metadata subtree nor reads
// a secret value, and still collects a secret legitimately named "data".
func hostileVault(t *testing.T, listKeys string) (*httptest.Server, *mockVault) {
	t.Helper()
	m := &mockVault{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.recordRequest(r)
		method := r.Method
		if r.URL.Query().Get("list") == "true" {
			method = "LIST"
		}
		switch {
		case method == "GET" && r.URL.Path == "/v1/sys/mounts":
			_, _ = w.Write([]byte(`{"data":{"secret/":{"type":"kv","accessor":"a","options":{"version":"2"}}}}`))
		case method == "LIST" && r.URL.Path == "/v1/secret/metadata":
			_, _ = w.Write([]byte(`{"data":{"keys":[` + listKeys + `]}}`))
		case method == "GET" && r.URL.Path == "/v1/secret/metadata/data":
			// A secret legitimately named "data" must be collected, not rejected.
			_, _ = w.Write([]byte(`{"data":{"current_version":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func TestKVWalkCollectsSecretNamedData(t *testing.T) {
	t.Parallel()
	srv, _ := hostileVault(t, `"data"`)
	kv, err := newTestAdapter(t, srv).ListKVMetadata(context.Background())
	if err != nil {
		t.Fatalf("ListKVMetadata err = %v, want nil (secret named 'data' must be collected)", err)
	}
	if len(kv) != 1 || kv[0].Path != "data" {
		t.Fatalf("kv = %+v, want one entry with path 'data'", kv)
	}
}

func TestKVWalkRejectsTraversalKeyWithoutRequestingIt(t *testing.T) {
	t.Parallel()
	srv, mock := hostileVault(t, `"../../sys/health"`)
	_, err := newTestAdapter(t, srv).ListKVMetadata(context.Background())
	if err != errForbiddenPathTraversal {
		t.Fatalf("ListKVMetadata err = %v, want errForbiddenPathTraversal", err)
	}
	for _, p := range mock.requested() {
		if strings.Contains(p, "sys/health") || strings.Contains(p, "..") {
			t.Fatalf("adapter requested a traversed path %q", p)
		}
	}
}

func TestKVWalkEscapesQueryInjectionKey(t *testing.T) {
	t.Parallel()
	srv, mock := hostileVault(t, `"x?version=99"`)
	// The key is a leaf; the read is escaped (404 from the mock) and must never
	// arrive as a raw query parameter. A 404 is surfaced as VaultNotFoundError
	// rather than silently dropped.
	_, err := newTestAdapter(t, srv).ListKVMetadata(context.Background())
	if err == nil {
		t.Fatal("ListKVMetadata error = nil, want VaultNotFoundError for injected key read")
	}
	if !errors.Is(err, ErrVaultNotFound) {
		t.Fatalf("ListKVMetadata error = %v, want VaultNotFoundError", err)
	}
	// The "?version=99" must have been percent-escaped into the path, so it can
	// never appear as a real query parameter (only "list=true" is legitimate).
	for _, q := range mock.requestedQueries() {
		if strings.Contains(q, "version") {
			t.Fatalf("query injection reached the server as a raw query: %q", q)
		}
	}
}

func TestDataPathGuardRejectsBeforeRequest(t *testing.T) {
	t.Parallel()
	srv, mock := newMockVault(t)
	a := newTestAdapter(t, srv)

	if _, err := a.listKeys(context.Background(), "secret/data/payments"); err != errForbiddenDataPath {
		t.Fatalf("listKeys on data path err = %v, want errForbiddenDataPath", err)
	}
	for _, p := range mock.requested() {
		if strings.Contains(p, "/data/") {
			t.Fatalf("a data-path request escaped the guard: %q", p)
		}
	}
}

func TestIsKVDataPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"secret/data/payments":     true,
		"secret/data":              true,
		"secret/metadata/payments": false,
		"sys/auth":                 false,
		"auth/kubernetes/role":     false,
		"kv/data/app/config":       true,
	}
	for path, want := range cases {
		if got := isKVDataPath(path); got != want {
			t.Fatalf("isKVDataPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestNewValidatesConfig(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{Token: "t"}); err == nil {
		t.Fatal("New with empty address: want error")
	}
	if _, err := New(Config{Address: "https://vault"}); err == nil {
		t.Fatal("New with empty token: want error")
	}
	errURL := "https://user:secret@vault.example.test"
	if _, err := New(Config{Address: errURL, Token: "t"}); err == nil {
		t.Fatal("New with credential-bearing address: want error")
	} else if strings.Contains(err.Error(), "user") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("New leaked credential-bearing address in error: %v", err)
	}
	if _, err := New(Config{Address: "ftp://vault.example.test", Token: "t"}); err == nil {
		t.Fatal("New with non-http address: want error")
	}
}

func TestDoRequestStatusFailuresWrapSDKHTTPErrorWithoutLeakingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "forbidden", statusCode: http.StatusForbidden},
		{name: "server", statusCode: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "secret policy path sys/auth", tt.statusCode)
			}))
			defer server.Close()
			adapter := newTestAdapter(t, server)

			_, err := adapter.doRequest(context.Background(), "sys/auth", false, nil)
			if err == nil {
				t.Fatal("doRequest() error = nil, want status failure")
			}
			if tt.statusCode == http.StatusForbidden {
				var authErr VaultAuthError
				if !errors.As(err, &authErr) {
					t.Fatalf("doRequest() error = %T %[1]v, want VaultAuthError", err)
				}
				if authErr.StatusCode != tt.statusCode {
					t.Fatalf("VaultAuthError StatusCode = %d, want %d", authErr.StatusCode, tt.statusCode)
				}
			} else {
				var httpErr sdk.HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("doRequest() error = %T %[1]v, want SDK HTTPError cause", err)
				}
				if got := httpErr.StatusCode; got != tt.statusCode {
					t.Fatalf("SDK HTTPError StatusCode = %d, want %d", got, tt.statusCode)
				}
			}
			for _, leaked := range []string{"sys/auth", "secret policy", server.URL} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("status failure leaked %q: %v", leaked, err)
				}
			}
		})
	}
}

func TestDoRequestTransportFailureWrapsSDKHTTPErrorWithoutLeakingPath(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial denied for vault.example.test")
	adapter, err := New(Config{
		Address: "https://vault.example.test",
		Token:   "test-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.doRequest(context.Background(), "sys/auth", false, nil)
	if err == nil {
		t.Fatal("doRequest() error = nil, want transport failure")
	}
	var transportErr2 VaultTransportError
	if !errors.As(err, &transportErr2) {
		t.Fatalf("doRequest() error = %T %[1]v, want VaultTransportError", err)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("doRequest() error = %v, want transport cause", err)
	}
	for _, leaked := range []string{"vault.example.test", "test-token"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("transport failure leaked %q: %v", leaked, err)
		}
	}
}

func TestAdapterReportsAPICallObservations(t *testing.T) {
	t.Parallel()
	srv, _ := newMockVault(t)
	var mu sync.Mutex
	got := map[string]string{}
	a, err := New(Config{
		Address: srv.URL, Token: "test-token", HTTPClient: srv.Client(),
		OnAPICall: func(operation, result string) {
			mu.Lock()
			got[operation] = result
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { _, e := a.ListAuthMounts(ctx); return e },
		func() error { _, e := a.ListAuthRoles(ctx); return e },
		func() error { _, e := a.ListACLPolicies(ctx); return e },
		func() error { _, e := a.ListIdentityEntities(ctx); return e },
		func() error { _, e := a.ListIdentityAliases(ctx); return e },
		func() error { _, e := a.ListKVMetadata(ctx); return e },
		func() error { _, e := a.ListSecretEngineMounts(ctx); return e },
	} {
		if e := call(); e != nil {
			t.Fatalf("list call: %v", e)
		}
	}
	for _, op := range []string{
		"list_auth_mounts", "list_auth_roles", "list_acl_policies",
		"list_identity_entities", "list_identity_aliases", "list_kv_metadata",
		"list_secret_engine_mounts",
	} {
		if got[op] != "success" {
			t.Fatalf("operation %q result = %q, want success (all: %v)", op, got[op], got)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
