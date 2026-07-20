// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeMCPProviderStore is a local query.AuthProviderStore stand-in; the
// query-package fakes are unexported to that package's tests.
type fakeMCPProviderStore struct {
	items []query.AuthProviderItem
}

func (f *fakeMCPProviderStore) ListLoginProviders(context.Context, string) ([]query.AuthProviderItem, error) {
	return f.items, nil
}

type fakeMCPIssuerLister struct {
	issuers []string
}

func (f *fakeMCPIssuerLister) ActiveIssuers(context.Context) []string {
	return f.issuers
}

func TestOAuthMetadataURL_ValidationTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		resource    string
		wantURL     string
		wantEnabled bool
	}{
		{"https no path", "https://eshu.example.test", "https://eshu.example.test/.well-known/oauth-protected-resource", true},
		{"https with path", "https://eshu.example.test/mcp", "https://eshu.example.test/.well-known/oauth-protected-resource/mcp", true},
		{"https with port", "https://eshu.example.test:8443/mcp", "https://eshu.example.test:8443/.well-known/oauth-protected-resource/mcp", true},
		{"loopback http allowed", "http://localhost:8080/mcp", "http://localhost:8080/.well-known/oauth-protected-resource/mcp", true},
		{"loopback ip allowed", "http://127.0.0.1/mcp", "http://127.0.0.1/.well-known/oauth-protected-resource/mcp", true},
		{"non-loopback http rejected", "http://eshu.example.test/mcp", "", false},
		{"query rejected", "https://eshu.example.test/mcp?x=1", "", false},
		{"fragment rejected", "https://eshu.example.test/mcp#frag", "", false},
		{"quote rejected", `https://eshu.example.test/m"cp`, "", false},
		{"percent-encoded quote rejected", "https://eshu.example.test/m%22cp", "", false},
		{"percent-encoded CRLF rejected", "https://eshu.example.test/a%0d%0ab", "", false},
		{"missing scheme rejected", "eshu.example.test/mcp", "", false},
		{"empty rejected", "", "", false},
		{"non-http scheme rejected", "ftp://eshu.example.test/mcp", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotURL, gotOK := oauthMetadataURL(tc.resource)
			if gotOK != tc.wantEnabled {
				t.Fatalf("oauthMetadataURL(%q) ok = %v, want %v", tc.resource, gotOK, tc.wantEnabled)
			}
			if gotURL != tc.wantURL {
				t.Fatalf("oauthMetadataURL(%q) = %q, want %q", tc.resource, gotURL, tc.wantURL)
			}
		})
	}
}

func TestBuildMCPOAuthDiscovery_DisabledWhenResourceUnsetOrInvalid(t *testing.T) {
	t.Parallel()

	providers := &fakeMCPProviderStore{items: []query.AuthProviderItem{{ProviderConfigID: "okta", ProviderKind: "oidc"}}}
	issuers := &fakeMCPIssuerLister{issuers: []string{"https://idp.example.test"}}

	for _, tc := range []struct {
		name     string
		resource string
	}{
		{"unset", ""},
		{"invalid non-loopback http", "http://eshu.example.test/mcp"},
		{"invalid with query", "https://eshu.example.test/mcp?a=b"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(k string) string {
				if k == envAuthResourceURI {
					return tc.resource
				}
				return ""
			}
			handler, challenge := buildMCPOAuthDiscovery(getenv, providers, nil, issuers, nil)
			if handler != nil {
				t.Errorf("handler = %v, want nil when discovery is disabled", handler)
			}
			if challenge != nil {
				t.Errorf("challenge = %v (%T), want a true nil interface when discovery is disabled", challenge, challenge)
			}
		})
	}
}

func TestBuildMCPOAuthDiscovery_EnabledCarriesExtensionFields(t *testing.T) {
	t.Parallel()

	providers := &fakeMCPProviderStore{items: []query.AuthProviderItem{{ProviderConfigID: "okta", ProviderKind: "oidc"}}}
	issuers := &fakeMCPIssuerLister{issuers: []string{"https://idp.example.test"}}
	getenv := func(k string) string {
		switch k {
		case envAuthResourceURI:
			return "https://eshu.example.test/mcp"
		case envAuthResourceDocumentation:
			return "https://docs.example.test"
		case envAuthPreregisteredClientID:
			return "0oaClientId"
		default:
			return ""
		}
	}
	handler, challenge := buildMCPOAuthDiscovery(getenv, providers, nil, issuers, nil)
	if handler == nil || challenge == nil {
		t.Fatalf("handler=%v challenge=%v, want both non-nil when enabled", handler, challenge)
	}
	if handler.Resource != "https://eshu.example.test/mcp" {
		t.Errorf("Resource = %q, want the verbatim resource URI", handler.Resource)
	}
	if handler.ResourceDocumentation != "https://docs.example.test" || handler.PreregisteredClientID != "0oaClientId" {
		t.Errorf("extension fields not threaded: doc=%q client=%q", handler.ResourceDocumentation, handler.PreregisteredClientID)
	}
	if _, _, ok := challenge.OAuthChallenge(context.Background()); !ok {
		t.Error("challenge OAuthChallenge ok = false, want true with a configured provider")
	}
}

// TestComposedMux_WellKnownUnauthenticatedWhileTransportGated proves the §D
// placement: the discovery route mounted on the base adminMux is served
// unauthenticated by the composed mcp httpMux, while POST /mcp/message and
// /api/v0/* — wrapped by the SAME buildTransportAuthMiddleware — deny a
// headerless request. It also proves the 404-when-disabled path.
func TestComposedMux_WellKnownUnauthenticatedWhileTransportGated(t *testing.T) {
	t.Parallel()

	providers := &fakeMCPProviderStore{items: []query.AuthProviderItem{{ProviderConfigID: "okta", ProviderKind: "oidc"}}}
	issuers := &fakeMCPIssuerLister{issuers: []string{"https://idp.example.test"}}
	getenv := func(k string) string {
		if k == envAuthResourceURI {
			return "https://eshu.example.test"
		}
		return ""
	}
	handler, challenge := buildMCPOAuthDiscovery(getenv, providers, nil, issuers, nil)
	if handler == nil || challenge == nil {
		t.Fatal("discovery should be enabled for this test")
	}

	// enforcement true, no shared token: headerless credentialed routes deny.
	transportAuth := buildTransportAuthMiddleware("", nil, nil, true, challenge, nil)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := mcp.NewServer(transportAuth(apiMux), logger, mcp.WithTransportAuth(transportAuth))

	adminMux := http.NewServeMux()
	handler.Mount(adminMux)
	composed := server.Handler(adminMux)

	// Well-known: reachable with no credential, returns the real document.
	wkRec := httptest.NewRecorder()
	composed.ServeHTTP(wkRec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if wkRec.Code != http.StatusOK {
		t.Fatalf("GET /.well-known no-cred status = %d, want 200: %s", wkRec.Code, wkRec.Body.String())
	}
	var doc query.OAuthProtectedResourceMetadata
	if err := json.Unmarshal(wkRec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal well-known: %v", err)
	}
	if doc.Resource != "https://eshu.example.test" || len(doc.AuthorizationServers) != 1 {
		t.Fatalf("document = %+v, want resource + one authorization server", doc)
	}

	// POST /mcp/message: headerless denies with an augmented challenge.
	msgRec := httptest.NewRecorder()
	msgReq := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	composed.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != http.StatusUnauthorized {
		t.Fatalf("headerless POST /mcp/message status = %d, want 401", msgRec.Code)
	}
	if got := msgRec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "resource_metadata=") {
		t.Fatalf("POST /mcp/message WWW-Authenticate = %q, want an augmented challenge", got)
	}

	// GET /api/v0/*: headerless denies.
	apiRec := httptest.NewRecorder()
	composed.ServeHTTP(apiRec, httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil))
	if apiRec.Code != http.StatusUnauthorized {
		t.Fatalf("headerless GET /api/v0/repositories status = %d, want 401", apiRec.Code)
	}

	// Disabled deployment: the well-known route is never mounted -> 404.
	disabledAdmin := http.NewServeMux()
	disabledServer := mcp.NewServer(transportAuth(apiMux), logger, mcp.WithTransportAuth(transportAuth))
	disabledComposed := disabledServer.Handler(disabledAdmin)
	offRec := httptest.NewRecorder()
	disabledComposed.ServeHTTP(offRec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if offRec.Code != http.StatusNotFound {
		t.Fatalf("GET /.well-known with discovery disabled status = %d, want 404", offRec.Code)
	}
}
