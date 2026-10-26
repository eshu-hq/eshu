// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDeploymentReadFailureByAuthMode pins the optional Deployments permission
// boundary after an Actions read succeeds. A 403/404 preserves run facts in
// every mode; 401 and rate limiting retain each mode's claim semantics.
func TestDeploymentReadFailureByAuthMode(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate App test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var mintCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/123/access_tokens" {
			t.Errorf("unexpected mint path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mintCalls.Add(1)
		_, _ = w.Write([]byte(`{"token":"installation-token-canary"}`))
	}))
	t.Cleanup(server.Close)

	for _, mode := range []string{AuthModeTokenEnv, AuthModeHostMinted, AuthModeGitHubApp} {
		for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
			t.Run(mode+"/optional-"+http.StatusText(status), func(t *testing.T) {
				failure := sdk.HTTPError{Provider: "github_actions", StatusCode: status, Message: http.StatusText(status)}
				source, reader := deploymentAuthModeSource(t, mode, failure, server, string(keyPEM))
				collected, ok, err := source.NextClaimed(t.Context(), claimDeploymentWorkItem())
				if err != nil || !ok {
					t.Fatalf("NextClaimed() = ok=%v err=%v, want run facts and warning", ok, err)
				}
				envelopes := drainFacts(t, collected.Facts)
				requireFactKind(t, envelopes, facts.CICDRunFactKind)
				found := false
				for _, warning := range filterFactKind(envelopes, facts.CICDWarningFactKind) {
					found = found || warning.Payload["reason"] == "deployment_permission_denied"
				}
				if !found {
					t.Fatal("missing deployment_permission_denied warning")
				}
				wantClass := "error"
				if mode == AuthModeGitHubApp {
					wantClass = "auth_error"
				}
				assertCICDRunCounterPoint(t, collectCICDRunMetrics(t, reader), "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
					telemetry.MetricDimensionProvider: "github_actions", telemetry.MetricDimensionStatusClass: wantClass,
				})
			})
		}
		t.Run(mode+"/401", func(t *testing.T) {
			failure := sdk.HTTPError{Provider: "github_actions", StatusCode: http.StatusUnauthorized, Message: "Unauthorized"}
			source, _ := deploymentAuthModeSource(t, mode, failure, server, string(keyPEM))
			_, ok, err := source.NextClaimed(t.Context(), claimDeploymentWorkItem())
			if err == nil || ok {
				t.Fatalf("NextClaimed() = ok=%v err=%v, want failed claim", ok, err)
			}
			switch mode {
			case AuthModeTokenEnv:
				var raw sdk.HTTPError
				if !errors.As(err, &raw) || collector.IsRetryable(err) {
					t.Fatalf("static token error = %v, want original HTTP failure", err)
				}
			case AuthModeHostMinted:
				assertAuthDeniedTerminal(t, err, "host-token-canary")
			case AuthModeGitHubApp:
				if !collector.IsRetryable(err) {
					t.Fatalf("App 401 = %v, want retry with fresh mint", err)
				}
				before := mintCalls.Load()
				_, _, _ = source.NextClaimed(t.Context(), claimDeploymentWorkItem())
				if mintCalls.Load() != before+1 {
					t.Fatalf("App mint calls after retry = %d, want %d", mintCalls.Load(), before+1)
				}
			}
		})
		t.Run(mode+"/403-rate-limit", func(t *testing.T) {
			source, reader := deploymentAuthModeSource(t, mode, RateLimitError{StatusCode: http.StatusForbidden, RetryAfter: 45 * time.Second}, server, string(keyPEM))
			_, ok, err := source.NextClaimed(t.Context(), claimDeploymentWorkItem())
			var delayed interface{ RetryAfterDelay() time.Duration }
			var terminal interface{ TerminalFailure() bool }
			if err == nil || ok || !errors.Is(err, ErrRateLimited) || !errors.As(err, &delayed) || delayed.RetryAfterDelay() != 45*time.Second || (errors.As(err, &terminal) && terminal.TerminalFailure()) {
				t.Fatalf("rate limit = ok=%v err=%v, want retryable 45s throttle", ok, err)
			}
			assertCICDRunCounterPoint(t, collectCICDRunMetrics(t, reader), "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
				telemetry.MetricDimensionProvider: "github_actions", telemetry.MetricDimensionStatusClass: "rate_limited",
			})
		})
	}
}

func deploymentAuthModeSource(t *testing.T, mode string, deploymentErr error, server *httptest.Server, privateKey string) (ClaimedSource, *sdkmetric.ManualReader) {
	t.Helper()
	target := TargetConfig{
		AuthMode: mode, ScopeID: hostTokenTestScopeID, Repository: "example/repo",
		AllowedRepositories: []string{"example/repo"}, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
		APIBaseURL: server.URL,
	}
	switch mode {
	case AuthModeTokenEnv:
		target.Token = "static-token-canary"
	case AuthModeHostMinted:
		target.HostTokenFile = writeHostTokenFile(t, "host-token-canary", hostTokenTestScopeID)
	case AuthModeGitHubApp:
		target.GitHubAppID, target.GitHubAppInstallationID = "456", "123"
		target.GitHubAppPrivateKey, target.AppHTTPClient = privateKey, server.Client()
	}
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meter.Meter("deployment-auth-mode-test"))
	if err != nil {
		t.Fatalf("NewInstruments(): %v", err)
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary", Instruments: instruments,
		Client:  fakeDeploymentClient{fakeClient: fakeClient{page: RunPage{Snapshots: []RunSnapshot{telemetryTestSnapshot()}}}, deploymentErr: deploymentErr},
		Targets: []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource(): %v", err)
	}
	return source, reader
}
