// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ghsaWindowTestPolicy arms every window claim so the mint-inherit
// acceptance path proves launch instead of the unarmed cursor refusal.
type ghsaWindowTestPolicy struct{}

func (ghsaWindowTestPolicy) CommitPolicy(workflow.WorkItem, time.Time, []facts.Envelope) (ClaimCommitDecision, error) {
	return ClaimCommitDecision{Handled: true, WriteOnce: true}, nil
}

var _ ClaimCommitPolicy = ghsaWindowTestPolicy{}

// newGHSAWindowMintSource builds a claim plane with both the token provider
// and a commit policy able to arm a window generation.
func newGHSAWindowMintSource(
	t *testing.T,
	runner Runner,
	recorder StatusRecorder,
	provider TokenProvider,
	config map[string]any,
) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              config,
		Runner:              runner,
		StatusRecorder:      recorder,
		Clock:               testObservedAt,
		TokenProvider:       provider,
		ClaimCommitPolicy:   ghsaWindowTestPolicy{},
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// GHSA mint-inheritance vectors (eshu-hq/eshu#10533): a coordinator-minted
// window scope inherits its anchor's opt-in and installation mapping at both
// host gates, resolved only via the strict vulnsource parser. Window scopes
// carry no opt-in entries of their own.
const (
	ghsaAnchorReviewed = "vuln-intel://ghsa/reviewed"
	ghsaWindowReviewed = "vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000"
	// ghsaWindowLookalike contains the window infix but fails
	// ParseGHSAWindowScope (non-digit bounds), so it inherits nothing.
	ghsaWindowLookalike = "vuln-intel://ghsa/reviewed/_window/abc-def"
)

// TestGHSAWindowMintInheritOptIn pins the opt-in gate table for #10533:
// (ii) anchor absent, (iii) anchor other mode, (v) parser-rejected
// lookalike, (vii) exact window entry decides before the anchor fallback,
// and the positive inherit case.
func TestGHSAWindowMintInheritOptIn(t *testing.T) {
	t.Parallel()

	anchorOpted := hostMintTargetsConfig(ghsaAnchorReviewed, HostMintedAuthMode)
	tests := []struct {
		name   string
		config map[string]any
		scope  string
		want   bool
	}{
		{
			name:   "window inherits opted anchor",
			config: anchorOpted,
			scope:  ghsaWindowReviewed,
			want:   true,
		},
		{
			name:   "window anchor absent stays static",
			config: map[string]any{"fixture": "scorecard"},
			scope:  ghsaWindowReviewed,
			want:   false,
		},
		{
			name:   "window anchor other mode stays static",
			config: hostMintTargetsConfig(ghsaAnchorReviewed, "token_env"),
			scope:  ghsaWindowReviewed,
			want:   false,
		},
		{
			name:   "lookalike inherits nothing",
			config: anchorOpted,
			scope:  ghsaWindowLookalike,
			want:   false,
		},
		{
			name: "exact window entry denies before anchor fallback",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": ghsaWindowReviewed},
				map[string]any{"scope_id": ghsaAnchorReviewed, "auth_mode": HostMintedAuthMode},
			}},
			scope: ghsaWindowReviewed,
			want:  false,
		},
		{
			name: "exact window entry opts in without anchor",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": ghsaWindowReviewed, "auth_mode": HostMintedAuthMode},
			}},
			scope: ghsaWindowReviewed,
			want:  true,
		},
		{
			name:   "anchor itself still exact",
			config: anchorOpted,
			scope:  ghsaAnchorReviewed,
			want:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hostMintOptedIn(tc.config, tc.scope); got != tc.want {
				t.Fatalf("hostMintOptedIn(%q) = %t, want %t", tc.scope, got, tc.want)
			}
		})
	}
}

// TestGHSAWindowMintInheritProvider pins the installation gate for #10533: a
// window claim mints via its anchor's installation, an unmapped anchor fails
// closed, and a parser-rejected lookalike inherits nothing.
func TestGHSAWindowMintInheritProvider(t *testing.T) {
	t.Parallel()

	t.Run("window mints via anchor installation", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var seen []string
		server := appProviderTestServer(t, &mu, &seen)
		t.Cleanup(server.Close)
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:         "42",
			PrivateKey:    appProviderTestKey(t),
			Installations: map[string]string{ghsaAnchorReviewed: "117398917"},
			BaseURL:       server.URL,
			HTTPClient:    server.Client(),
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowReviewed
		token, err := provider(context.Background(), item)
		if err != nil {
			t.Fatalf("provider(window) error = %v, want nil", err)
		}
		if token != "tok-for-117398917" {
			t.Fatalf("provider(window) = %q, want anchor installation token", token)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(seen) != 1 {
			t.Fatalf("mint requests = %d, want 1", len(seen))
		}
	})

	t.Run("unmapped anchor fails closed", func(t *testing.T) {
		t.Parallel()
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:         "42",
			PrivateKey:    appProviderTestKey(t),
			Installations: map[string]string{"some-other-scope": "777"},
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowReviewed
		_, err = provider(context.Background(), item)
		if err == nil {
			t.Fatal("provider(window) error = nil, want unknown-scope denial")
		}
		var configErr *app.ConfigError
		if !errors.As(err, &configErr) {
			t.Fatalf("provider(window) error type = %T, want *app.ConfigError", err)
		}
	})

	t.Run("lookalike inherits nothing", func(t *testing.T) {
		t.Parallel()
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:         "42",
			PrivateKey:    appProviderTestKey(t),
			Installations: map[string]string{ghsaAnchorReviewed: "117398917"},
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowLookalike
		_, err = provider(context.Background(), item)
		if err == nil {
			t.Fatal("provider(lookalike) error = nil, want denial")
		}
	})

	t.Run("exact window installation wins over anchor", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var seen []string
		server := appProviderTestServer(t, &mu, &seen)
		t.Cleanup(server.Close)
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:      "42",
			PrivateKey: appProviderTestKey(t),
			Installations: map[string]string{
				ghsaWindowReviewed: "111",
				ghsaAnchorReviewed: "222",
			},
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowReviewed
		token, err := provider(context.Background(), item)
		if err != nil {
			t.Fatalf("provider(window) error = %v, want nil", err)
		}
		if token != "tok-for-111" {
			t.Fatalf("provider(window) = %q, want exact window installation", token)
		}
	})
}

// TestGHSAWindowMintInheritEndToEnd pins the two acceptance paths through the
// claim plane: (i) window + opted anchor + mapped anchor mints and stamps the
// WINDOW scope id, (iv) window + opted anchor + unmapped anchor fails terminal
// auth_denied before the satellite launches.
func TestGHSAWindowMintInheritEndToEnd(t *testing.T) {
	t.Parallel()

	t.Run("window mints and stamps window scope", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var seen []string
		server := appProviderTestServer(t, &mu, &seen)
		t.Cleanup(server.Close)
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:         "42",
			PrivateKey:    appProviderTestKey(t),
			Installations: map[string]string{ghsaAnchorReviewed: "117398917"},
			BaseURL:       server.URL,
			HTTPClient:    server.Client(),
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowReviewed
		runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
		source := newGHSAWindowMintSource(t, runner, &recordingStatusRecorder{}, provider,
			hostMintTargetsConfig(ghsaAnchorReviewed, HostMintedAuthMode))

		if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if len(runner.requests) != 1 {
			t.Fatalf("runner requests = %d, want 1", len(runner.requests))
		}
		got, ok := runner.requests[0].Config[HostMintedGitHubTokenKey].(map[string]any)
		if !ok {
			t.Fatalf("config[%q] = %v, want object {token, scope_id}", HostMintedGitHubTokenKey, runner.requests[0].Config[HostMintedGitHubTokenKey])
		}
		if got["scope_id"] != ghsaWindowReviewed {
			t.Fatalf("config scope_id = %v, want window %q", got["scope_id"], ghsaWindowReviewed)
		}
	})

	t.Run("opted but unmapped window fails terminal pre-launch", func(t *testing.T) {
		t.Parallel()
		provider, err := NewAppTokenProvider(AppTokenProviderConfig{
			AppID:         "42",
			PrivateKey:    "unusable-in-this-test",
			Installations: map[string]string{"some-other-scope": "777"},
		})
		if err != nil {
			t.Fatalf("NewAppTokenProvider() error = %v, want nil", err)
		}
		item := testWorkItem()
		item.ScopeID = ghsaWindowReviewed
		runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
		recorder := &recordingResultRecorder{}
		source := mustNewSourceWithProvider(t, runner, recorder, provider,
			hostMintTargetsConfig(ghsaAnchorReviewed, HostMintedAuthMode))

		_, ok, err := source.NextClaimed(context.Background(), item)
		if err == nil {
			t.Fatal("NextClaimed() error = nil, want terminal mint denial")
		}
		if ok {
			t.Fatal("NextClaimed() ok = true, want false on unmapped window anchor")
		}
		assertFailure(t, err, "auth_denied", true)
		if len(runner.requests) != 0 {
			t.Fatalf("runner requests = %d, want 0 (satellite never launched)", len(runner.requests))
		}
	})
}
