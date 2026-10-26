// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func mustNewSourceWithProvider(
	t *testing.T,
	runner Runner,
	recorder StatusRecorder,
	provider TokenProvider,
	config map[string]any,
) *Source {
	t.Helper()
	if config == nil {
		config = map[string]any{"fixture": "scorecard"}
	}
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
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// TestHostMintedTokenInjectedPerClaim proves a configured provider mints once
// per claim and the minted token reaches the satellite through the stdio
// config channel, while static config passes through and no key material
// crosses the boundary.
func TestHostMintedTokenInjectedPerClaim(t *testing.T) {
	t.Parallel()

	const privateKey = "-----BEGIN RSA PRIVATE KEY-----\nhost-only-key-material\n-----END RSA PRIVATE KEY-----"
	calls := 0
	provider := func(context.Context, workflow.WorkItem) (string, error) {
		calls++
		if calls == 1 {
			return "host-token-1", nil
		}
		return "host-token-2", nil
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, provider, nil)

	for i := 0; i < 2; i++ {
		if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
			t.Fatalf("NextClaimed() #%d error = %v, want nil", i+1, err)
		}
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 1 per claim (2)", calls)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("runner requests = %d, want 2", len(runner.requests))
	}
	for i, request := range runner.requests {
		want := "host-token-" + string(rune('1'+i))
		got, ok := request.Config[HostMintedGitHubTokenKey].(map[string]any)
		if !ok {
			t.Fatalf("request #%d config[%q] = %v, want object {token, scope_id}", i+1, HostMintedGitHubTokenKey, request.Config[HostMintedGitHubTokenKey])
		}
		if got["token"] != want {
			t.Fatalf("request #%d config[%q].token = %v, want %q", i+1, HostMintedGitHubTokenKey, got["token"], want)
		}
		if got["scope_id"] != item.ScopeID {
			t.Fatalf("request #%d config[%q].scope_id = %v, want %q", i+1, HostMintedGitHubTokenKey, got["scope_id"], item.ScopeID)
		}
		if got := request.Config["fixture"]; got != "scorecard" {
			t.Fatalf("request #%d static config lost: fixture = %v", i+1, got)
		}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal request #%d: %v", i+1, err)
		}
		if strings.Contains(string(raw), privateKey) {
			t.Fatalf("request #%d carries host key material", i+1)
		}
	}
}

// TestHostMintDenialFailsTerminalBounded proves a mint denial (bad
// installation ID, revoked credentials, misconfiguration) fails the claim
// terminal with a bounded operator-actionable message: no retry storm, no
// credential or provider-error text in the failure.
func TestHostMintDenialFailsTerminalBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "auth denied 401", err: &app.AuthDeniedError{StatusCode: 401}},
		{name: "auth denied 403", err: &app.AuthDeniedError{StatusCode: 403}},
		{name: "installation not found", err: &app.NotFoundError{}},
		{name: "config error", err: app.NewConfigError("parse github app private key")},
		{name: "unbounded provider error", err: errors.New("provider leaked SECRET-abc-123")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider := func(context.Context, workflow.WorkItem) (string, error) {
				return "", tc.err
			}
			item := testWorkItem()
			runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
			recorder := &recordingResultRecorder{}
			source := mustNewSourceWithProvider(t, runner, recorder, provider, nil)

			_, ok, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatal("NextClaimed() error = nil, want terminal mint denial")
			}
			if ok {
				t.Fatal("NextClaimed() ok = true, want false on mint denial")
			}
			assertFailure(t, err, "auth_denied", true)
			if len(runner.requests) != 0 {
				t.Fatalf("runner requests = %d, want 0 (satellite never launched)", len(runner.requests))
			}
			if got := err.Error(); strings.Contains(got, "SECRET-abc-123") {
				t.Fatalf("mint denial carries provider text: %q", got)
			}
			if len(recorder.results) != 1 {
				t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
			}
			record := recorder.results[0]
			if record.Origin != ResultOriginHost || record.State != sdkcollector.ResultTerminal {
				t.Fatalf("result record = %+v, want host terminal verdict", record)
			}
			if record.FailureClass != "auth_denied" {
				t.Fatalf("result failure class = %q, want auth_denied", record.FailureClass)
			}
		})
	}
}

// TestHostMintRetryableStaysRetryable proves a transient mint fault keeps the
// claim retryable (with the provider's backoff) instead of failing terminal.
func TestHostMintRetryableStaysRetryable(t *testing.T) {
	t.Parallel()

	provider := func(context.Context, workflow.WorkItem) (string, error) {
		return "", app.NewRetryableError(app.ClassRateLimited, 3, time.Minute, errors.New("mint github app token: status 429"))
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingResultRecorder{}
	source := mustNewSourceWithProvider(t, runner, recorder, provider, nil)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want retryable mint failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false on mint failure")
	}
	assertFailure(t, err, app.ClassRateLimited, false)
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0", len(runner.requests))
	}
	var classified interface{ RetryAfterDelay() time.Duration }
	if !errors.As(err, &classified) {
		t.Fatal("mint failure does not expose RetryAfterDelay")
	}
	if got := classified.RetryAfterDelay(); got != time.Minute {
		t.Fatalf("RetryAfterDelay() = %v, want 1m", got)
	}
}

// TestHostMintEmptyTokenFailsTerminal proves a provider that returns no token
// fails closed instead of launching the satellite unauthenticated.
func TestHostMintEmptyTokenFailsTerminal(t *testing.T) {
	t.Parallel()

	provider := func(context.Context, workflow.WorkItem) (string, error) {
		return "   ", nil
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, provider, nil)

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal empty-token failure")
	}
	assertFailure(t, err, "auth_denied", true)
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0", len(runner.requests))
	}
}

// TestProviderReservedKeyRejectedAtConstruction proves a static config that
// already defines the host-minted token key fails closed at activation when a
// provider is set, instead of silently overwriting per claim.
func TestProviderReservedKeyRejectedAtConstruction(t *testing.T) {
	t.Parallel()

	provider := func(context.Context, workflow.WorkItem) (string, error) {
		return "host-token", nil
	}
	_, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{HostMintedGitHubTokenKey: "operator-token"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		TokenProvider:       provider,
	})
	if err == nil {
		t.Fatal("NewSource() error = nil, want reserved-key rejection")
	}
	if got := err.Error(); strings.Contains(got, "operator-token") {
		t.Fatalf("construction error echoes credential: %q", got)
	}
}

// TestNilProviderLeavesConfigUntouched proves the token_env escape hatch:
// without a provider the stdio config is the byte-identical static clone.
func TestNilProviderLeavesConfigUntouched(t *testing.T) {
	t.Parallel()

	static := map[string]any{"token_env": "ESHU_PAT", "depth": float64(3)}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, nil, static)

	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want 1", len(runner.requests))
	}
	got := runner.requests[0].Config
	if len(got) != len(static) {
		t.Fatalf("request config = %v, want byte-identical %v", got, static)
	}
	for key, want := range static {
		if got[key] != want {
			t.Fatalf("request config[%q] = %v, want %v", key, got[key], want)
		}
	}
	if _, present := got[HostMintedGitHubTokenKey]; present {
		t.Fatalf("request config carries %q without a provider", HostMintedGitHubTokenKey)
	}
}
