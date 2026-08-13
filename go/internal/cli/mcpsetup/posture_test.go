// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// probeTestClient returns an *http.Client with the given timeout, matching
// the dedicated short-timeout client probeAuthPosture must use (never the
// 30s APIClient default).
func probeTestClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func TestProbeAuthPosture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantPosture AuthPosture
		wantIssuers []string
		wantClient  string
		wantWarning bool
	}{
		{
			name: "200 valid doc with issuers is SSO",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"resource": "https://eshu.example.com/mcp",
					"authorization_servers": ["https://idp.example.com/oauth2/aus123"],
					"eshu_preregistered_client_id": "eshu-mcp-client"
				}`))
			},
			wantPosture: PostureSSO,
			wantIssuers: []string{"https://idp.example.com/oauth2/aus123"},
			wantClient:  "eshu-mcp-client",
			wantWarning: false,
		},
		{
			name: "404 is token with no warning",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantPosture: PostureToken,
			wantWarning: false,
		},
		{
			name: "500 is token with warning",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantPosture: PostureToken,
			wantWarning: true,
		},
		{
			name: "malformed JSON is token with warning",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("not json"))
			},
			wantPosture: PostureToken,
			wantWarning: true,
		},
		{
			name: "empty authorization_servers is token with warning (defensive)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"resource": "https://eshu.example.com/mcp", "authorization_servers": []}`))
			},
			wantPosture: PostureToken,
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			got := probeAuthPosture(probeTestClient(3*time.Second), server.URL)
			if got.Posture != tt.wantPosture {
				t.Fatalf("Posture = %v, want %v", got.Posture, tt.wantPosture)
			}
			if len(tt.wantIssuers) > 0 {
				if len(got.Issuers) != len(tt.wantIssuers) || got.Issuers[0] != tt.wantIssuers[0] {
					t.Fatalf("Issuers = %v, want %v", got.Issuers, tt.wantIssuers)
				}
			}
			if tt.wantClient != "" && got.PreregisteredClientID != tt.wantClient {
				t.Fatalf("PreregisteredClientID = %q, want %q", got.PreregisteredClientID, tt.wantClient)
			}
			gotWarning := strings.TrimSpace(got.Warning) != ""
			if gotWarning != tt.wantWarning {
				t.Fatalf("Warning present = %v (%q), want %v", gotWarning, got.Warning, tt.wantWarning)
			}
		})
	}
}

func TestProbeAuthPostureConnectionRefused(t *testing.T) {
	t.Parallel()
	// Port 1 is a privileged/unassigned port; nothing listens there, so the
	// dial fails immediately with connection refused.
	got := probeAuthPosture(probeTestClient(3*time.Second), "http://127.0.0.1:1")
	if got.Posture != PostureToken {
		t.Fatalf("Posture = %v, want PostureToken on connection refused", got.Posture)
	}
	if strings.TrimSpace(got.Warning) == "" {
		t.Fatal("expected a non-empty warning on connection refused")
	}
}

func TestProbeAuthPostureTimeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resource":"x","authorization_servers":["https://idp.example.com"]}`))
	}))
	defer server.Close()

	got := probeAuthPosture(probeTestClient(50*time.Millisecond), server.URL)
	if got.Posture != PostureToken {
		t.Fatalf("Posture = %v, want PostureToken on timeout", got.Posture)
	}
	if strings.TrimSpace(got.Warning) == "" {
		t.Fatal("expected a non-empty warning on timeout")
	}
}

func TestResolveAuthPosture(t *testing.T) {
	t.Parallel()

	panicProbe := func(string) PostureProbeResult {
		panic("probe must not be called for this case")
	}

	t.Run("explicit sso never probes", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("sso", false, true, panicProbe, "https://eshu.example.com")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureSSO {
			t.Fatalf("Posture = %v, want PostureSSO", got.Posture)
		}
	})

	t.Run("explicit token never probes", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("token", false, true, panicProbe, "https://eshu.example.com")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureToken {
			t.Fatalf("Posture = %v, want PostureToken", got.Posture)
		}
	})

	t.Run("explicit shared-key never probes", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("shared-key", false, true, panicProbe, "https://eshu.example.com")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureSharedKey {
			t.Fatalf("Posture = %v, want PostureSharedKey", got.Posture)
		}
	})

	t.Run("--shared-key bool wins over --auth auto without probing", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("auto", true, true, panicProbe, "https://eshu.example.com")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureSharedKey {
			t.Fatalf("Posture = %v, want PostureSharedKey", got.Posture)
		}
	})

	t.Run("auto with local stdio never probes and defaults token", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("auto", false, false, panicProbe, "")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureToken {
			t.Fatalf("Posture = %v, want PostureToken", got.Posture)
		}
	})

	t.Run("auto with hosted calls probe and returns its result", func(t *testing.T) {
		t.Parallel()
		called := false
		probe := func(url string) PostureProbeResult {
			called = true
			if url != "https://eshu.example.com" {
				t.Fatalf("probe called with url = %q, want https://eshu.example.com", url)
			}
			return PostureProbeResult{Posture: PostureSSO, Issuers: []string{"https://idp.example.com"}}
		}
		got, err := ResolveAuthPosture("auto", false, true, probe, "https://eshu.example.com")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if !called {
			t.Fatal("expected probe to be called for auto+hosted")
		}
		if got.Posture != PostureSSO {
			t.Fatalf("Posture = %v, want PostureSSO", got.Posture)
		}
	})

	t.Run("empty --auth defaults to auto behavior", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("", false, false, panicProbe, "")
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got.Posture != PostureToken {
			t.Fatalf("Posture = %v, want PostureToken", got.Posture)
		}
	})

	t.Run("invalid --auth value errors listing accepted values", func(t *testing.T) {
		t.Parallel()
		_, err := ResolveAuthPosture("bogus", false, true, panicProbe, "https://eshu.example.com")
		if err == nil {
			t.Fatal("error = nil, want non-nil for invalid --auth value")
		}
		for _, want := range []string{"auto", "sso", "token", "shared-key"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not list accepted value %q", err.Error(), want)
			}
		}
	})

	// The hosted "auto" branch is the only one that calls probe. Before the
	// guard it dereferenced a nil probe and panicked, which is a poor contract
	// for an exported function: a caller that forgets the argument gets a stack
	// trace instead of the error return the signature already offers.
	t.Run("hosted auto with a nil probe errors instead of panicking", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveAuthPosture("auto", false, true, nil, "https://eshu.example.com")
		if err == nil {
			t.Fatal("error = nil, want non-nil when probe is nil on the hosted auto branch")
		}
		if !strings.Contains(err.Error(), "probe") {
			t.Fatalf("error %q does not name the missing probe argument", err.Error())
		}
		// The error must also tell the operator how to get moving again,
		// the way the invalid --auth error does. Spelled out rather than
		// compared against the constant, so rewording the constant cannot
		// quietly keep this assertion true.
		if want := "sso, token, or shared-key"; !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not offer the explicit --auth values %q", err.Error(), want)
		}
		// The result returned alongside the error must stay the safe zero
		// value. Handing back PostureSharedKey here would wire the legacy
		// shared admin key into a config on a path that failed.
		if got.Posture != PostureToken {
			t.Fatalf("Posture = %v on the error return, want the PostureToken zero value", got.Posture)
		}
	})

	// The guard must not fire on branches that never call probe -- a nil probe
	// is legitimate for every one of them, and erroring there would break real
	// callers that pass nil deliberately.
	t.Run("nil probe is fine on every branch that does not probe", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name     string
			authFlag string
			shared   bool
			hosted   bool
		}{
			{"explicit sso", "sso", false, true},
			{"explicit token", "token", false, true},
			{"explicit shared-key", "shared-key", false, true},
			{"shared-key flag wins over auto", "auto", true, true},
			{"auto in local stdio mode", "auto", false, false},
			{"empty auth in local stdio mode", "", false, false},
		}
		for _, tc := range cases {
			if _, err := ResolveAuthPosture(tc.authFlag, tc.shared, tc.hosted, nil, "https://eshu.example.com"); err != nil {
				t.Fatalf("%s: error = %v, want nil with a nil probe", tc.name, err)
			}
		}
	})
}
