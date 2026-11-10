// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// hostMintTargetsConfig builds a static config carrying a targets list with
// one entry for scopeID. An empty authMode omits the auth_mode key entirely,
// proving the non-opted-in path never mints.
func hostMintTargetsConfig(scopeID, authMode string) map[string]any {
	entry := map[string]any{"scope_id": scopeID}
	if authMode != "" {
		entry["auth_mode"] = authMode
	}
	return map[string]any{
		"fixture": "scorecard",
		"targets": []any{entry},
	}
}

// TestHostMintOptedInInjectsToken proves candidate (a) of #10483: a provider
// set plus a claimed scope opted in via auth_mode host_minted mints once and
// the minted token reaches the satellite as {token, scope_id} with static
// config preserved.
func TestHostMintOptedInInjectsToken(t *testing.T) {
	t.Parallel()

	calls := 0
	provider := func(context.Context, workflow.WorkItem) (string, error) {
		calls++
		return "host-token-opted-in", nil
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, provider, hostMintTargetsConfig(item.ScopeID, HostMintedAuthMode))

	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1 for the opted-in claim", calls)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want 1", len(runner.requests))
	}
	got, ok := runner.requests[0].Config[HostMintedGitHubTokenKey].(map[string]any)
	if !ok {
		t.Fatalf("config[%q] = %v, want object {token, scope_id}", HostMintedGitHubTokenKey, runner.requests[0].Config[HostMintedGitHubTokenKey])
	}
	if got["token"] != "host-token-opted-in" {
		t.Fatalf("config[%q].token = %v, want minted token", HostMintedGitHubTokenKey, got["token"])
	}
	if got["scope_id"] != item.ScopeID {
		t.Fatalf("config[%q].scope_id = %v, want %q", HostMintedGitHubTokenKey, got["scope_id"], item.ScopeID)
	}
	if got := runner.requests[0].Config["fixture"]; got != "scorecard" {
		t.Fatalf("static config lost: fixture = %v", got)
	}
}

// TestHostMintNonOptedInSkipsProvider proves the gate's safe direction: a
// provider set plus a claimed scope with no auth_mode opt-in never consults
// the provider (no mint, no GitHub spend) and the static config passes
// through byte-identical with no host_github_app_token key.
func TestHostMintNonOptedInSkipsProvider(t *testing.T) {
	t.Parallel()

	provider := func(context.Context, workflow.WorkItem) (string, error) {
		t.Error("provider consulted for a non-opted-in claim, want zero calls")
		return "must-not-mint", nil
	}
	item := testWorkItem()
	static := map[string]any{
		"token_env": "ESHU_PAT",
		"targets":   []any{map[string]any{"scope_id": item.ScopeID}},
	}
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProvider(t, runner, &recordingStatusRecorder{}, provider, static)

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
		if got[key] == nil {
			t.Fatalf("request config[%q] missing, want %v", key, want)
		}
	}
	if _, present := got[HostMintedGitHubTokenKey]; present {
		t.Fatalf("request config carries %q for a non-opted-in claim", HostMintedGitHubTokenKey)
	}
}

// TestHostMintOptedInUnmappedFailsTerminal proves the fail-closed path is
// preserved under the gate: an opted-in scope with no mapped installation
// fails the claim terminal auth_denied without ever starting the satellite.
func TestHostMintOptedInUnmappedFailsTerminal(t *testing.T) {
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
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingResultRecorder{}
	source := mustNewSourceWithProvider(t, runner, recorder, provider, hostMintTargetsConfig(item.ScopeID, HostMintedAuthMode))

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal mint denial")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false on unmapped opted-in scope")
	}
	assertFailure(t, err, "auth_denied", true)
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0 (satellite never launched)", len(runner.requests))
	}
	if len(recorder.results) != 1 {
		t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
	}
	record := recorder.results[0]
	if record.Origin != ResultOriginHost || record.State != sdkcollector.ResultTerminal {
		t.Fatalf("result record = %+v, want host terminal verdict", record)
	}
}

// mustNewSourceWithProviderAndResolver builds a Source with both a token
// provider and a derived target resolver, for the derived-claim mint path.
func mustNewSourceWithProviderAndResolver(
	t *testing.T,
	runner Runner,
	recorder StatusRecorder,
	provider TokenProvider,
	resolver DerivedTargetResolver,
	config map[string]any,
) *Source {
	t.Helper()
	if config == nil {
		config = map[string]any{"fixture": "scorecard"}
	}
	source, err := NewSource(Config{
		Manifest:              testManifest(),
		CollectorInstanceID:   "scorecard-instance",
		ScopeKind:             scope.KindRepository,
		ConfigHandle:          "cfg-scorecard",
		Config:                config,
		Runner:                runner,
		StatusRecorder:        recorder,
		Clock:                 testObservedAt,
		TokenProvider:         provider,
		DerivedTargetResolver: resolver,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// staticDerivedResolver returns a fixed derived target for any claim, so the
// request config carries a host-resolved derived target with no static
// auth_mode opt-in.
func staticDerivedResolver(_ context.Context, item workflow.WorkItem) (map[string]any, error) {
	return map[string]any{
		"scope_id":          item.ScopeID,
		"repository_id":     "repository:r_1a2b3c4d",
		"source_commit_sha": testGoCommit,
		"clone_url":         "https://github.com/eshu-hq/widgets",
	}, nil
}

// TestHostMintDerivedClaimConsultsProvider proves the derived-claim exception
// to the opt-in gate: a claim carrying a host-resolved derived target consults
// the provider even with no static auth_mode opt-in, and the minted token
// reaches the satellite alongside the derived target.
func TestHostMintDerivedClaimConsultsProvider(t *testing.T) {
	t.Parallel()

	calls := 0
	provider := func(context.Context, workflow.WorkItem) (string, error) {
		calls++
		return "host-token-derived", nil
	}
	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithProviderAndResolver(t, runner, &recordingStatusRecorder{}, provider,
		staticDerivedResolver, map[string]any{"fixture": "scorecard"})

	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1 for the derived claim", calls)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want 1", len(runner.requests))
	}
	got, ok := runner.requests[0].Config[HostMintedGitHubTokenKey].(map[string]any)
	if !ok {
		t.Fatalf("config[%q] = %v, want object {token, scope_id}", HostMintedGitHubTokenKey, runner.requests[0].Config[HostMintedGitHubTokenKey])
	}
	if got["token"] != "host-token-derived" {
		t.Fatalf("config[%q].token = %v, want minted token", HostMintedGitHubTokenKey, got["token"])
	}
	if got["scope_id"] != item.ScopeID {
		t.Fatalf("config[%q].scope_id = %v, want %q", HostMintedGitHubTokenKey, got["scope_id"], item.ScopeID)
	}
	if _, ok := runner.requests[0].Config[HostDerivedTargetKey].(map[string]any); !ok {
		t.Fatalf("config lacks %q, want the derived target kept", HostDerivedTargetKey)
	}
}

// TestHostMintDerivedUnmappedFailsTerminal proves an unmapped derived pair
// fails the claim terminal auth_denied without ever starting the satellite,
// restoring the pre-gate behavior on the derived path.
func TestHostMintDerivedUnmappedFailsTerminal(t *testing.T) {
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
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingResultRecorder{}
	source := mustNewSourceWithProviderAndResolver(t, runner, recorder, provider,
		staticDerivedResolver, map[string]any{"fixture": "scorecard"})

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal mint denial")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false on unmapped derived scope")
	}
	assertFailure(t, err, "auth_denied", true)
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0 (satellite never launched)", len(runner.requests))
	}
	if len(recorder.results) != 1 {
		t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
	}
	record := recorder.results[0]
	if record.Origin != ResultOriginHost || record.State != sdkcollector.ResultTerminal {
		t.Fatalf("result record = %+v, want host terminal verdict", record)
	}
}

// TestHostMintOptedInPredicate pins the opt-in predicate's table: only an
// entry whose scope_id equals the claimed scope AND whose auth_mode is exactly
// host_minted opts in. Every other shape stays on the static path.
func TestHostMintOptedInPredicate(t *testing.T) {
	t.Parallel()

	const scopeID = "repo-eshu"
	tests := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{name: "nil config", config: nil, want: false},
		{name: "no targets key", config: map[string]any{"fixture": "x"}, want: false},
		{name: "targets wrong type", config: map[string]any{"targets": "repo-eshu"}, want: false},
		{
			name:   "opted-in entry",
			config: hostMintTargetsConfig(scopeID, HostMintedAuthMode),
			want:   true,
		},
		{
			name:   "entry without auth_mode",
			config: hostMintTargetsConfig(scopeID, ""),
			want:   false,
		},
		{
			name:   "entry with other auth_mode",
			config: hostMintTargetsConfig(scopeID, "token_env"),
			want:   false,
		},
		{
			name:   "auth_mode case differs",
			config: hostMintTargetsConfig(scopeID, "Host_Minted"),
			want:   false,
		},
		{
			name: "opt-in entry for another scope",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": "other-scope", "auth_mode": HostMintedAuthMode},
			}},
			want: false,
		},
		{
			name: "opt-in entry beside other entries",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": "other-scope"},
				map[string]any{"scope_id": scopeID, "auth_mode": HostMintedAuthMode},
			}},
			want: true,
		},
		{
			name: "non-map entry skipped",
			config: map[string]any{"targets": []any{
				"repo-eshu",
				map[string]any{"scope_id": scopeID, "auth_mode": HostMintedAuthMode},
			}},
			want: true,
		},
		{
			name: "duplicate scope first non-opted-in wins",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": scopeID},
				map[string]any{"scope_id": scopeID, "auth_mode": HostMintedAuthMode},
			}},
			want: false,
		},
		{
			name: "duplicate scope first opted-in wins",
			config: map[string]any{"targets": []any{
				map[string]any{"scope_id": scopeID, "auth_mode": HostMintedAuthMode},
				map[string]any{"scope_id": scopeID},
			}},
			want: true,
		},
		{
			name: "entry missing scope_id",
			config: map[string]any{"targets": []any{
				map[string]any{"auth_mode": HostMintedAuthMode},
			}},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hostMintOptedIn(tc.config, scopeID); got != tc.want {
				t.Fatalf("hostMintOptedIn() = %t, want %t", got, tc.want)
			}
		})
	}
}

// BenchmarkHostMintOptedIn measures the per-claim gate predicate cost for the
// #10483 evidence note: one short targets scan, first scope match decides.
func BenchmarkHostMintOptedIn(b *testing.B) {
	optedIn := hostMintTargetsConfig("repo-eshu", HostMintedAuthMode)
	notOptedIn := hostMintTargetsConfig("repo-eshu", "")
	b.Run("opted-in", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if !hostMintOptedIn(optedIn, "repo-eshu") {
				b.Fatal("hostMintOptedIn() = false, want true")
			}
		}
	})
	b.Run("non-opted-in", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if hostMintOptedIn(notOptedIn, "repo-eshu") {
				b.Fatal("hostMintOptedIn() = true, want false")
			}
		}
	})
}
