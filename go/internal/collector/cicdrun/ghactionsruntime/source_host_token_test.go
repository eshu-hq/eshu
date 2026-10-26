// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// tokenRecordingClient is a Client double that records every Bearer token it
// is asked to spend and answers per token, so rotation and stale-token tests
// can prove which file generation a claim actually spent.
type tokenRecordingClient struct {
	mu     sync.Mutex
	tokens []string
	answer func(token string) (RunPage, error)
}

func (c *tokenRecordingClient) FetchRuns(_ context.Context, target TargetConfig) (RunPage, error) {
	c.mu.Lock()
	c.tokens = append(c.tokens, target.Token)
	c.mu.Unlock()
	if c.answer != nil {
		return c.answer(target.Token)
	}
	return RunPage{Snapshots: []RunSnapshot{telemetryTestSnapshot()}}, nil
}

func (c *tokenRecordingClient) spentTokens() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.tokens...)
}

func writeHostTokenFile(t *testing.T, token, scopeID string) string {
	t.Helper()
	return rewriteHostTokenFile(t, token, scopeID)
}

// rewriteHostTokenFile stores {token, scope_id} with an atomic replace
// (temp file plus rename), mirroring the host refresh contract: a satellite
// read observes either the old or the new complete file, never a partial one.
func rewriteHostTokenFile(t *testing.T, token, scopeID string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "host-token.json")
	atomicRewriteHostTokenFile(t, path, token, scopeID)
	return path
}

func atomicRewriteHostTokenFile(t *testing.T, path, token, scopeID string) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(path), "host-token-*.tmp")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v, want nil", err)
	}
	tmpName := tmp.Name()
	if _, err := fmt.Fprintf(tmp, `{"token": %q, "scope_id": %q}`, token, scopeID); err != nil {
		_ = tmp.Close()
		t.Fatalf("write temp token file error = %v, want nil", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp token file error = %v, want nil", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		t.Fatalf("rename temp token file error = %v, want nil", err)
	}
}

const hostTokenTestScopeID = "ci-cd:github-actions:example/repo"

// errHostTokenTestUnauthorized simulates the provider refusing a stale
// host-minted Bearer the same way GitHub answers an expired installation
// token: HTTP 401 with no credential echo.
var errHostTokenTestUnauthorized = sdk.HTTPError{
	Provider:   "github_actions",
	StatusCode: http.StatusUnauthorized,
	Message:    http.StatusText(http.StatusUnauthorized),
}

func hostTokenTestTarget(path string) TargetConfig {
	return TargetConfig{
		AuthMode:            AuthModeHostMinted,
		ScopeID:             hostTokenTestScopeID,
		Repository:          "example/repo",
		HostTokenFile:       path,
		AllowedRepositories: []string{"example/repo"},
		MaxRuns:             1,
		MaxJobs:             10,
		MaxArtifacts:        10,
	}
}

func hostTokenTestWorkItem(generationID string) workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             hostTokenTestScopeID,
		GenerationID:        generationID,
		CurrentFencingToken: 7,
	}
}

func assertAuthDeniedTerminal(t *testing.T, err error, token string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want terminal auth_denied")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != "auth_denied" {
		class := ""
		if errors.As(err, &classified) {
			class = classified.FailureClass()
		}
		t.Fatalf("FailureClass() = %q, want %q (err = %v)", class, "auth_denied", err)
	}
	var terminal interface{ TerminalFailure() bool }
	if !errors.As(err, &terminal) || !terminal.TerminalFailure() {
		t.Fatalf("TerminalFailure() = false, want true (err = %v)", err)
	}
	if token != "" && strings.Contains(err.Error(), token) {
		t.Fatalf("error %q carries the token value, want bounded text only", err.Error())
	}
}

// TestResolveHostTokenFileReadsToken pins the arbiter file shape: the file
// holds {"token", "scope_id"} and resolves to the Bearer the claim spends.
func TestResolveHostTokenFileReadsToken(t *testing.T) {
	t.Parallel()

	path := writeHostTokenFile(t, "host-token-a", hostTokenTestScopeID)
	token, err := ResolveHostTokenFile(path, hostTokenTestScopeID)
	if err != nil {
		t.Fatalf("ResolveHostTokenFile() error = %v, want nil", err)
	}
	if token != "host-token-a" {
		t.Fatalf("ResolveHostTokenFile() = %q, want %q", token, "host-token-a")
	}
}

func TestResolveHostTokenFileRejectsClosedFailures(t *testing.T) {
	t.Parallel()

	path := writeHostTokenFile(t, "host-token-secret-value", hostTokenTestScopeID)
	cases := map[string]struct {
		rewrite func(t *testing.T, path string)
	}{
		"missing file": {
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatalf("Remove() error = %v, want nil", err)
				}
			},
		},
		"empty file": {
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v, want nil", err)
				}
			},
		},
		"malformed json": {
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(`{"token": `), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v, want nil", err)
				}
			},
		},
		"empty token": {
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				atomicRewriteHostTokenFile(t, path, "   ", hostTokenTestScopeID)
			},
		},
		"scope mismatch": {
			rewrite: func(t *testing.T, path string) {
				t.Helper()
				atomicRewriteHostTokenFile(t, path, "host-token-secret-value", "ci-cd:github-actions:other/repo")
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			target := filepath.Join(dir, "host-token.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v, want nil", err)
			}
			if err := os.WriteFile(target, raw, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v, want nil", err)
			}
			tc.rewrite(t, target)
			_, err = ResolveHostTokenFile(target, hostTokenTestScopeID)
			assertAuthDeniedTerminal(t, err, "host-token-secret-value")
		})
	}
}

// TestNewClaimedSourceHostMintedRequiresTokenFile pins fail-closed
// construction: a host_minted target without a token file is rejected, while
// an empty startup Token is accepted because the Bearer resolves per claim.
func TestNewClaimedSourceHostMintedRequiresTokenFile(t *testing.T) {
	t.Parallel()

	target := hostTokenTestTarget("")
	if _, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              &tokenRecordingClient{},
		Targets:             []TargetConfig{target},
	}); err == nil {
		t.Fatal("NewClaimedSource() error = nil, want host_token_file rejection")
	}

	path := writeHostTokenFile(t, "host-token-a", hostTokenTestScopeID)
	if _, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              &tokenRecordingClient{},
		Targets:             []TargetConfig{hostTokenTestTarget(path)},
	}); err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
}

// TestClaimedSourceHostMintedResolvesTokenPerClaim is the rotation proof:
// claim 1 spends file generation A, the host atomically rewrites the file,
// and claim 2 on the same source (no restart) spends generation B.
func TestClaimedSourceHostMintedResolvesTokenPerClaim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "host-token.json")
	atomicRewriteHostTokenFile(t, path, "host-token-a", hostTokenTestScopeID)
	client := &tokenRecordingClient{}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets:             []TargetConfig{hostTokenTestTarget(path)},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	if _, ok, err := source.NextClaimed(context.Background(), hostTokenTestWorkItem("generation-1")); err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	atomicRewriteHostTokenFile(t, path, "host-token-b", hostTokenTestScopeID)
	if _, ok, err := source.NextClaimed(context.Background(), hostTokenTestWorkItem("generation-2")); err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}

	if got, want := client.spentTokens(), []string{"host-token-a", "host-token-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("spent tokens = %q, want %q", got, want)
	}
}

// TestClaimedSourceHostMintedStaleTokenFailsBounded pins the stale-token
// contract: a well-formed but provider-refused Bearer fails the claim as
// bounded terminal auth_denied (no attempt-budget burn), and the next claim
// after a host refresh recovers without a restart.
func TestClaimedSourceHostMintedStaleTokenFailsBounded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "host-token.json")
	atomicRewriteHostTokenFile(t, path, "stale-token", hostTokenTestScopeID)
	client := &tokenRecordingClient{
		answer: func(token string) (RunPage, error) {
			if token != "fresh-token" {
				return RunPage{}, errHostTokenTestUnauthorized
			}
			return RunPage{Snapshots: []RunSnapshot{telemetryTestSnapshot()}}, nil
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets:             []TargetConfig{hostTokenTestTarget(path)},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), hostTokenTestWorkItem("generation-1"))
	assertAuthDeniedTerminal(t, err, "stale-token")

	atomicRewriteHostTokenFile(t, path, "fresh-token", hostTokenTestScopeID)
	collected, ok, err := source.NextClaimed(context.Background(), hostTokenTestWorkItem("generation-2"))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	if got := collected.Scope.ScopeID; got != hostTokenTestScopeID {
		t.Fatalf("ScopeID = %q, want %q", got, hostTokenTestScopeID)
	}
	if got, want := client.spentTokens(), []string{"stale-token", "fresh-token"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("spent tokens = %q, want %q", got, want)
	}
}

// TestClaimedSourceHostMintedRecordsAuthDeniedTelemetry pins the bounded
// token-resolution outcome label: a mid-life unreadable file records the
// fetch under status_class auth_denied, distinguishable from a provider
// "error", and the token value never reaches a metric label.
func TestClaimedSourceHostMintedRecordsAuthDeniedTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-host-token-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	path := writeHostTokenFile(t, "host-token-telemetry-secret", hostTokenTestScopeID)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              &tokenRecordingClient{},
		Instruments:         instruments,
		Targets:             []TargetConfig{hostTokenTestTarget(path)},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), hostTokenTestWorkItem("generation-1"))
	assertAuthDeniedTerminal(t, err, "host-token-telemetry-secret")

	rm := collectCICDRunMetrics(t, reader)
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
		telemetry.MetricDimensionProvider:    string(cicdrun.ProviderGitHubActions),
		telemetry.MetricDimensionStatusClass: "auth_denied",
	})
	assertCICDRunCounterLabelsExclude(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", "host-token-telemetry-secret", "example/repo")
}

// TestClaimedSourceHostMintedRotationRace runs concurrent claims across host
// refreshes: every claim spends either the old or the new complete token (the
// atomic-replace contract), never a partial or empty one. Run with -race.
func TestClaimedSourceHostMintedRotationRace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "host-token.json")
	atomicRewriteHostTokenFile(t, path, "host-token-a", hostTokenTestScopeID)
	client := &tokenRecordingClient{}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Targets:             []TargetConfig{hostTokenTestTarget(path)},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_, _, _ = source.NextClaimed(
					context.Background(),
					hostTokenTestWorkItem(fmt.Sprintf("generation-race-%d-%d", g, i)),
				)
			}
		}(g)
	}
	for i := 0; i < 25; i++ {
		if i%2 == 0 {
			atomicRewriteHostTokenFile(t, path, "host-token-a", hostTokenTestScopeID)
		} else {
			atomicRewriteHostTokenFile(t, path, "host-token-b", hostTokenTestScopeID)
		}
	}
	wg.Wait()

	for _, token := range client.spentTokens() {
		if token != "host-token-a" && token != "host-token-b" {
			t.Fatalf("spent token = %q, want only complete generations host-token-a or host-token-b", token)
		}
	}
	if len(client.spentTokens()) == 0 {
		t.Fatal("spent tokens is empty, want completed claim attempts")
	}
}
