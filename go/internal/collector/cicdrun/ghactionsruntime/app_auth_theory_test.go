// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestGitHubAppAuthTheoryProvesRestrictedToken404 pins the observed
// provider difference before the command or runtime App wiring changes.
func TestGitHubAppAuthTheoryProvesRestrictedToken404(t *testing.T) {
	t.Parallel()
	const restrictedToken = "restricted-token-canary"
	const mintedToken = "installation-token-canary"
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	var mintRequests, actionRequests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			mintRequests++
			if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
				t.Errorf("mint request lacks signed App JWT")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"token":"` + mintedToken + `"}`))
		case "/repos/example/repo/actions/runs":
			actionRequests++
			switch r.Header.Get("Authorization") {
			case "Bearer " + restrictedToken:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("provider body contains " + restrictedToken))
			case "Bearer " + mintedToken:
				_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1001,"workflow_id":42}]}`))
			default:
				t.Errorf("unexpected Actions credential")
				w.WriteHeader(http.StatusUnauthorized)
			}
		case "/repos/example/repo/actions/runs/1001/jobs":
			_, _ = w.Write([]byte(`{"total_count":0,"jobs":[]}`))
		case "/repos/example/repo/actions/runs/1001/artifacts":
			_, _ = w.Write([]byte(`{"total_count":0,"artifacts":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	target := TargetConfig{
		ScopeID: "ci:example/repo", Repository: "example/repo",
		Token: restrictedToken, AllowedRepositories: []string{"example/repo"},
		APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
	}
	client := GitHubClient{HTTPClient: server.Client()}
	_, err = client.FetchRuns(t.Context(), target)
	var httpErr sdk.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("restricted token error = %v, want sanitized HTTP 404", err)
	}
	if strings.Contains(err.Error(), restrictedToken) {
		t.Fatalf("restricted token leaked through error: %v", err)
	}
	installationToken, err := app.MintInstallationToken(t.Context(), app.Config{
		BaseURL: server.URL, AppID: "456", InstallationID: "123",
		PrivateKey: string(keyPEM), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("mint installation token: %v", err)
	}
	target.Token = installationToken
	page, err := client.FetchRuns(t.Context(), target)
	if err != nil {
		t.Fatalf("installation token Actions fetch: %v", err)
	}
	if len(page.Snapshots) != 1 || mintRequests != 1 || actionRequests != 2 {
		t.Fatalf("snapshots=%d mint_requests=%d action_requests=%d, want 1/1/2", len(page.Snapshots), mintRequests, actionRequests)
	}
}

func TestClaimedSourceMintsOnceForRunsAndDeployments(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	var mintRequests, providerRequests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/123/access_tokens" {
			mintRequests.Add(1)
			_, _ = w.Write([]byte(`{"token":"installation-token"}`))
			return
		}
		providerRequests.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer installation-token" {
			t.Errorf("provider request used wrong bearer token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/example/repo/actions/runs":
			_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1001,"workflow_id":42,"run_attempt":1,"run_number":88,"name":"Publish","event":"push","status":"completed","conclusion":"success","head_branch":"main","head_sha":"0123456789abcdef0123456789abcdef01234567","run_started_at":"2026-06-07T14:59:00Z","updated_at":"2026-06-07T15:00:00Z","html_url":"https://github.com/example/repo/actions/runs/1001","repository":{"full_name":"example/repo","html_url":"https://github.com/example/repo"}}]}`))
		case "/repos/example/repo/actions/runs/1001/jobs":
			_, _ = w.Write([]byte(`{"total_count":0,"jobs":[]}`))
		case "/repos/example/repo/actions/runs/1001/artifacts":
			_, _ = w.Write([]byte(`{"total_count":0,"artifacts":[]}`))
		case "/repos/example/repo/deployments":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected provider path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-primary",
		Client:              GitHubClient{HTTPClient: server.Client()},
		Targets: []TargetConfig{{
			ScopeID: "ci:example/repo", Repository: "example/repo", AuthMode: AuthModeGitHubApp,
			GitHubAppID: "456", GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM),
			AppHTTPClient: server.Client(), AllowedRepositories: []string{"example/repo"},
			APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
		}},
	})
	if err != nil {
		t.Fatalf("new App source: %v", err)
	}
	for index := range 2 {
		_, ok, err := source.NextClaimed(t.Context(), workflow.WorkItem{
			CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-primary",
			ScopeID: "ci:example/repo", GenerationID: fmt.Sprintf("generation-%d", index),
			CurrentFencingToken: int64(index + 1),
		})
		if err != nil || !ok {
			t.Fatalf("NextClaimed(%d) = ok=%v err=%v", index, ok, err)
		}
	}
	if got := mintRequests.Load(); got != 2 {
		t.Fatalf("mint requests = %d, want 1 per claim", got)
	}
	if got := providerRequests.Load(); got != 8 {
		t.Fatalf("provider requests = %d, want 4 per claim", got)
	}
}

func TestClaimedSourceRejectsAppMintFailureBeforeProviderFetch(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/app/installations/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		t.Errorf("provider endpoint called after App mint failure: %s", r.URL.Path)
	}))
	t.Cleanup(server.Close)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	source, err := NewClaimedSource(SourceConfig{CollectorInstanceID: "ci-primary", Client: GitHubClient{HTTPClient: server.Client()}, Targets: []TargetConfig{{
		ScopeID: "ci:example/repo", Repository: "example/repo", AuthMode: AuthModeGitHubApp,
		GitHubAppID: "456", GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM),
		AppHTTPClient: server.Client(), AllowedRepositories: []string{"example/repo"},
		APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
	}}})
	if err != nil {
		t.Fatalf("new App source: %v", err)
	}
	_, _, err = source.NextClaimed(t.Context(), workflow.WorkItem{
		CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-primary",
		ScopeID: "ci:example/repo", GenerationID: "generation-1", CurrentFencingToken: 1,
	})
	var missing *app.NotFoundError
	if !errors.As(err, &missing) || classifyProviderStatus(err) != "auth_error" {
		t.Fatalf("mint failure = %v, want terminal auth_error", err)
	}
}

func TestClaimedSourceAppMintRateLimitPreservesRetryDelay(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/app/installations/") {
			t.Errorf("provider endpoint called after App mint rate limit: %s", r.URL.Path)
		}
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	source, err := NewClaimedSource(SourceConfig{CollectorInstanceID: "ci-primary", Client: GitHubClient{HTTPClient: server.Client()}, Targets: []TargetConfig{{
		ScopeID: "ci:example/repo", Repository: "example/repo", AuthMode: AuthModeGitHubApp,
		GitHubAppID: "456", GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM),
		AppHTTPClient: server.Client(), AllowedRepositories: []string{"example/repo"},
		APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
	}}})
	if err != nil {
		t.Fatalf("new App source: %v", err)
	}
	_, _, err = source.NextClaimed(t.Context(), workflow.WorkItem{
		CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-primary",
		ScopeID: "ci:example/repo", GenerationID: "generation-1", CurrentFencingToken: 1,
	})
	var retryable *app.RetryableError
	var delay interface{ RetryAfterDelay() time.Duration }
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &retryable) || !errors.As(err, &delay) || delay.RetryAfterDelay() != 120*time.Second {
		t.Fatalf("App mint rate limit = %v, want retryable 120s delay", err)
	}
	if classifyProviderStatus(err) != "rate_limited" {
		t.Fatalf("App mint rate limit class = %q", classifyProviderStatus(err))
	}
}

func TestClaimedSourceAppRead401IsRetryable(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/123/access_tokens" {
			_, _ = w.Write([]byte(`{"token":"installation-token"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	source, err := NewClaimedSource(SourceConfig{CollectorInstanceID: "ci-primary", Client: GitHubClient{HTTPClient: server.Client()}, Targets: []TargetConfig{{
		ScopeID: "ci:example/repo", Repository: "example/repo", AuthMode: AuthModeGitHubApp,
		GitHubAppID: "456", GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM),
		AppHTTPClient: server.Client(), AllowedRepositories: []string{"example/repo"},
		APIBaseURL: server.URL, MaxRuns: 1, MaxJobs: 1, MaxArtifacts: 1,
	}}})
	if err != nil {
		t.Fatalf("new App source: %v", err)
	}
	_, _, err = source.NextClaimed(t.Context(), workflow.WorkItem{
		CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-primary",
		ScopeID: "ci:example/repo", GenerationID: "generation-1", CurrentFencingToken: 1,
	})
	if !collector.IsRetryable(err) || classifyProviderStatus(err) != "auth_error" || strings.Contains(err.Error(), "mint") {
		t.Fatalf("App read 401 = %v, want retryable read-specific auth_error", err)
	}
}

func TestStaticTokenRefusalRetainsGenericStatusClass(t *testing.T) {
	t.Parallel()
	refusal := sdk.HTTPError{StatusCode: http.StatusUnauthorized}
	if got := classifyProviderStatus(refusal); got != "error" {
		t.Fatalf("static-token 401 status class = %q, want error", got)
	}
	if got := classifyProviderStatusForMode(refusal, true); got != "auth_error" {
		t.Fatalf("direct-App 401 status class = %q, want auth_error", got)
	}
}

func TestRuntimeRejectsMixedCredentialModes(t *testing.T) {
	t.Parallel()
	base := TargetConfig{ScopeID: "ci:example/repo", Repository: "example/repo", AllowedRepositories: []string{"example/repo"}, MaxJobs: 1, MaxArtifacts: 1}
	cases := []struct {
		name     string
		mode     string
		token    string
		hostFile string
		appID    string
		want     string
	}{
		{"static with host", AuthModeTokenEnv, "token", "/token.json", "", "host_token_file"},
		{"host with App", AuthModeHostMinted, "", "/token.json", "app", "github_app"},
		{"App with host", AuthModeGitHubApp, "", "/token.json", "app", "host_token_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := base
			v.AuthMode, v.Token, v.HostTokenFile, v.GitHubAppID = tc.mode, tc.token, tc.hostFile, tc.appID
			v.GitHubAppInstallationID, v.GitHubAppPrivateKey = "install", "key"
			_, err := validateTarget(v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate target = %v, want %s", err, tc.want)
			}
		})
	}
}
