// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func appTestPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// appMintAlertServer serves both the App mint endpoint and the Dependabot
// alerts endpoints on one fake API base. It counts mints and alert reads and
// records every credential it saw so tests prove freshness and sanitization.
func appMintAlertServer(t *testing.T, mintStatuses []int) (*httptest.Server, *atomic.Int64, *atomic.Int64, func() []string) {
	t.Helper()
	var mints, reads atomic.Int64
	var mu sync.Mutex
	var authorizations []string
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.URL.Path+"|"+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app/installations/") {
			index := int(calls.Add(1)) - 1
			status := mintStatuses[min(index, len(mintStatuses)-1)]
			mints.Add(1)
			if status == http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "minted-installation-token"})
				return
			}
			http.Error(w, "{}", status)
			return
		}
		reads.Add(1)
		_ = json.NewEncoder(w).Encode([]securityalerts.GitHubDependabotAlert{})
	}))
	t.Cleanup(server.Close)
	authSeen := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), authorizations...)
	}
	return server, &mints, &reads, authSeen
}

func appModeTarget(keyPEM string) TargetConfig {
	return TargetConfig{
		Provider:                ProviderGitHubDependabot,
		AuthMode:                AuthModeGitHubApp,
		ScopeID:                 "security-alert:github:example-org/example-repo",
		Repository:              "example-org/example-repo",
		AllowedRepositories:     []string{"example-org/example-repo"},
		GitHubAppID:             "42",
		GitHubAppInstallationID: "777",
		GitHubAppPrivateKey:     keyPEM,
		MaxPages:                1,
	}
}

func appModeWorkItem() workflow.WorkItem {
	return testSecurityAlertWorkItem(time.Now())
}

// TestValidateTargetDualMode pins the mirrored validation table: static mode
// keeps requiring a token and refuses App credentials, App mode requires all
// three App values and refuses a static token, and unknown modes fail.
func TestValidateTargetDualMode(t *testing.T) {
	t.Parallel()

	keyPEM := appTestPrivateKeyPEM(t)
	base := TargetConfig{
		Provider:            ProviderGitHubDependabot,
		ScopeID:             "security-alert:github:example-org/example-repo",
		Repository:          "example-org/example-repo",
		AllowedRepositories: []string{"example-org/example-repo"},
	}
	tests := []struct {
		name    string
		mutate  func(*TargetConfig)
		wantErr string
	}{
		{name: "token default valid", mutate: func(c *TargetConfig) { c.Token = "pat" }},
		{name: "token explicit valid", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeTokenEnv
			c.Token = "pat"
		}},
		{name: "app valid", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeGitHubApp
			c.GitHubAppID = "42"
			c.GitHubAppInstallationID = "777"
			c.GitHubAppPrivateKey = keyPEM
		}},
		{
			name: "token mode missing token", mutate: func(c *TargetConfig) {},
			wantErr: "token is required",
		},
		{name: "token mode refuses app id", mutate: func(c *TargetConfig) {
			c.Token = "pat"
			c.GitHubAppID = "42"
		}, wantErr: "github_app"},
		{name: "token mode refuses private key", mutate: func(c *TargetConfig) {
			c.Token = "pat"
			c.GitHubAppPrivateKey = keyPEM
		}, wantErr: "github_app"},
		{name: "app mode refuses static token", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeGitHubApp
			c.Token = "pat"
			c.GitHubAppID = "42"
			c.GitHubAppInstallationID = "777"
			c.GitHubAppPrivateKey = keyPEM
		}, wantErr: "token must be empty"},
		{name: "app mode missing app id", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeGitHubApp
			c.GitHubAppInstallationID = "777"
			c.GitHubAppPrivateKey = keyPEM
		}, wantErr: "github_app_id"},
		{name: "app mode missing installation", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeGitHubApp
			c.GitHubAppID = "42"
			c.GitHubAppPrivateKey = keyPEM
		}, wantErr: "github_app_installation_id"},
		{name: "app mode missing private key", mutate: func(c *TargetConfig) {
			c.AuthMode = AuthModeGitHubApp
			c.GitHubAppID = "42"
			c.GitHubAppInstallationID = "777"
		}, wantErr: "github_app_private_key"},
		{name: "unknown mode", mutate: func(c *TargetConfig) {
			c.AuthMode = "magic"
			c.Token = "pat"
		}, wantErr: "unsupported security alert auth_mode"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := base
			target.AllowedRepositories = append([]string(nil), base.AllowedRepositories...)
			tt.mutate(&target)
			validated, err := validateTarget(target)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateTarget() error = %v, want nil", err)
				}
				if validated.AuthMode == "" {
					t.Fatal("validateTarget() left auth_mode blank")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateTarget() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestAppModeMintsFreshTokenPerClaim pins per-claim freshness end to end:
// two sequential claims mint twice through the default client factory, and
// every alert read carries the minted token.
func TestAppModeMintsFreshTokenPerClaim(t *testing.T) {
	t.Parallel()

	keyPEM := appTestPrivateKeyPEM(t)
	server, mints, reads, authSeen := appMintAlertServer(t, []int{http.StatusOK})
	target := appModeTarget(keyPEM)
	target.APIBaseURL = server.URL
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := source.NextClaimed(context.Background(), appModeWorkItem()); err != nil {
			t.Fatalf("NextClaimed() #%d error = %v, want nil", i, err)
		}
	}
	if got := mints.Load(); got != 2 {
		t.Fatalf("mint calls = %d, want 2 (one per claim)", got)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("alert reads = %d, want 2", got)
	}
	for _, seen := range authSeen() {
		if strings.HasPrefix(seen, "/app/installations/") {
			continue
		}
		if !strings.HasSuffix(seen, "Bearer minted-installation-token") {
			t.Fatalf("alert authorization = %q, want the minted token", seen)
		}
	}
}

// TestAppModePreflightMintsEndToEnd pins that --preflight-provider-access
// works for github_app targets: one mint plus one bounded alert read.
func TestAppModePreflightMintsEndToEnd(t *testing.T) {
	t.Parallel()

	keyPEM := appTestPrivateKeyPEM(t)
	server, mints, reads, _ := appMintAlertServer(t, []int{http.StatusOK})
	target := appModeTarget(keyPEM)
	target.APIBaseURL = server.URL
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	result, err := source.PreflightProviderAccess(context.Background())
	if err != nil {
		t.Fatalf("PreflightProviderAccess() error = %v, want nil", err)
	}
	if result.TargetCount != 1 {
		t.Fatalf("TargetCount = %d, want 1", result.TargetCount)
	}
	if got := mints.Load(); got != 1 {
		t.Fatalf("mint calls = %d, want 1", got)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("alert reads = %d, want 1", got)
	}
}

// TestAppModeMintDenialIsSanitized is the runtime credential canary: a mint
// 404 surfaces a terminal auth_denied whose message carries no App ID,
// private key, JWT, or token.
func TestAppModeMintDenialIsSanitized(t *testing.T) {
	t.Parallel()

	keyPEM := appTestPrivateKeyPEM(t)
	server, _, _, authSeen := appMintAlertServer(t, []int{http.StatusNotFound})
	target := appModeTarget(keyPEM)
	target.APIBaseURL = server.URL
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, _, err = source.NextClaimed(context.Background(), appModeWorkItem())
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want mint denial")
	}
	failure, ok := err.(ProviderFailure)
	if !ok {
		t.Fatalf("error type = %T, want ProviderFailure", err)
	}
	if failure.FailureClass() != FailureAuthDenied || !failure.TerminalFailure() {
		t.Fatalf("class=%q terminal=%v, want auth_denied terminal",
			failure.FailureClass(), failure.TerminalFailure())
	}
	text := fmt.Sprintf("%+v", err)
	fragment := strings.TrimSpace(keyPEM)
	if len(fragment) > 200 {
		fragment = fragment[100:200]
	}
	for _, secret := range []string{fragment, "PRIVATE KEY", "minted-installation-token"} {
		if secret != "" && strings.Contains(text, secret) {
			t.Fatalf("failure carries credential material %q: %v", secret, err)
		}
	}
	for _, seen := range authSeen() {
		if !strings.HasPrefix(seen, "/app/installations/") {
			continue
		}
		header := strings.SplitN(seen, "|", 2)
		if len(header) == 2 && strings.Contains(text, strings.TrimPrefix(header[1], "Bearer ")) {
			t.Fatal("failure carries the signed JWT")
		}
	}
}
