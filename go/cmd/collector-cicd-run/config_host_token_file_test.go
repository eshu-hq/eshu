// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const hostFileTestScopeID = "ci-cd:github-actions:example-org/example-repo"

// hostFileRecordingClient spends whatever Bearer the claim resolved and
// records it, answering 401 for any token the test marks stale.
type hostFileRecordingClient struct {
	mu     sync.Mutex
	tokens []string
	stale  map[string]bool
}

func (c *hostFileRecordingClient) FetchRuns(_ context.Context, target ghactionsruntime.TargetConfig) (ghactionsruntime.RunPage, error) {
	c.mu.Lock()
	c.tokens = append(c.tokens, target.Token)
	c.mu.Unlock()
	if c.stale[target.Token] {
		return ghactionsruntime.RunPage{}, sdk.HTTPError{
			Provider:   "github_actions",
			StatusCode: http.StatusUnauthorized,
			Message:    http.StatusText(http.StatusUnauthorized),
		}
	}
	return ghactionsruntime.RunPage{Snapshots: []ghactionsruntime.RunSnapshot{hostFileTestSnapshot()}}, nil
}

func (c *hostFileRecordingClient) spentTokens() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.tokens...)
}

// hostFileTestSnapshot is a minimal valid run window: run identity plus one
// artifact row, mirroring the runtime package's own telemetry fixture.
func hostFileTestSnapshot() ghactionsruntime.RunSnapshot {
	return ghactionsruntime.RunSnapshot{
		Workflow: map[string]any{
			"id":   42,
			"name": "Publish",
		},
		Run: map[string]any{
			"id":             1001,
			"run_attempt":    1,
			"run_number":     88,
			"name":           "Publish",
			"event":          "push",
			"status":         "completed",
			"conclusion":     "success",
			"head_sha":       "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-06-07T14:59:00Z",
			"updated_at":     "2026-06-07T15:00:00Z",
		},
		JobsPartial:      true,
		ArtifactsPartial: true,
		Artifacts: []map[string]any{{
			"id":            501,
			"name":          "image-digest",
			"digest":        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"workflow_run":  map[string]any{"id": 1001},
			"created_at":    "2026-06-07T15:00:01Z",
			"expires_at":    "2026-06-14T15:00:01Z",
			"size_in_bytes": 128,
		}},
		Warnings: []map[string]any{{"reason": "provider_metadata_partial"}},
	}
}

func writeHostFileToken(t *testing.T, path, token, scopeID string) {
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

func hostFileTestInstancesJSON(tokenFile string) string {
	return `[{
		"instance_id": "cicd-run-primary",
		"collector_kind": "ci_cd_run",
		"mode": "continuous",
		"enabled": true,
		"claims_enabled": true,
		"configuration": {
			"targets": [{
				"provider": "github_actions",
				"scope_id": "` + hostFileTestScopeID + `",
				"repository": "example-org/example-repo",
				"auth_mode": "host_minted",
				"host_token_file": "` + tokenFile + `",
				"allowed_repositories": ["example-org/example-repo"],
				"api_base_url": "https://api.github.com",
				"max_runs": 1,
				"max_jobs": 25,
				"max_artifacts": 25
			}]
		}
	}]`
}

func hostFileTestWorkItem(generationID string) workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "cicd-run-primary",
		ScopeID:             hostFileTestScopeID,
		GenerationID:        generationID,
		CurrentFencingToken: 7,
	}
}

func loadHostFileSource(t *testing.T, tokenFile string, client *hostFileRecordingClient) ghactionsruntime.ClaimedSource {
	t.Helper()
	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return hostFileTestInstancesJSON(tokenFile)
		case envCollectorInstanceID:
			return "cicd-run-primary"
		case envOwnerID:
			return "pod-cicd-run"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if len(config.GitHubSource.Targets) != 1 {
		t.Fatalf("GitHubSource.Targets has %d entries, want 1", len(config.GitHubSource.Targets))
	}
	if got := config.GitHubSource.Targets[0].Token; got != "" {
		t.Fatalf("startup Token = %q, want empty (host_minted resolves per claim, never frozen at startup)", got)
	}
	source, err := ghactionsruntime.NewClaimedSource(ghactionsruntime.SourceConfig{
		CollectorInstanceID: config.GitHubSource.CollectorInstanceID,
		Client:              client,
		Targets:             config.GitHubSource.Targets,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func assertHostFileAuthDenied(t *testing.T, err error, token string) {
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

// TestLoadClaimedRuntimeConfigHostMintedRotationEndToEnd is the #9186
// failing-first regression for the prescribed channel: the whole startup path
// (instance validation, workflow validator, lane mapping with fail-closed
// file check) loads a host_minted target, claim 1 spends file generation A,
// the host atomically rewrites the file, and claim 2 on the same loaded
// source spends generation B with no restart.
func TestLoadClaimedRuntimeConfigHostMintedRotationEndToEnd(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host-token.json")
	writeHostFileToken(t, path, "host-token-a", hostFileTestScopeID)
	client := &hostFileRecordingClient{}
	source := loadHostFileSource(t, path, client)

	if _, ok, err := source.NextClaimed(context.Background(), hostFileTestWorkItem("generation-1")); err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	writeHostFileToken(t, path, "host-token-b", hostFileTestScopeID)
	collected, ok, err := source.NextClaimed(context.Background(), hostFileTestWorkItem("generation-2"))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	if got := collected.Scope.ScopeID; got != hostFileTestScopeID {
		t.Fatalf("ScopeID = %q, want %q", got, hostFileTestScopeID)
	}
	if got, want := client.spentTokens(), []string{"host-token-a", "host-token-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("spent tokens = %q, want %q", got, want)
	}
}

// TestLoadClaimedRuntimeConfigHostMintedStaleFailsBoundedThenRecovers pins
// the stale-token contract end to end: a provider-refused Bearer fails the
// claim as bounded terminal auth_denied, and the next claim after the host
// refreshes the file recovers on the same source.
func TestLoadClaimedRuntimeConfigHostMintedStaleFailsBoundedThenRecovers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host-token.json")
	writeHostFileToken(t, path, "stale-token", hostFileTestScopeID)
	client := &hostFileRecordingClient{stale: map[string]bool{"stale-token": true}}
	source := loadHostFileSource(t, path, client)

	_, _, err := source.NextClaimed(context.Background(), hostFileTestWorkItem("generation-1"))
	assertHostFileAuthDenied(t, err, "stale-token")

	writeHostFileToken(t, path, "fresh-token", hostFileTestScopeID)
	if _, ok, err := source.NextClaimed(context.Background(), hostFileTestWorkItem("generation-2")); err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
}

// TestLoadClaimedRuntimeConfigHostMintedPerClaimFailClosed pins the
// per-claim failure table after a clean startup: a file deleted mid-life, a
// malformed rewrite, and a scope-mismatched rewrite each fail the claim as
// bounded terminal auth_denied without spending a token.
func TestLoadClaimedRuntimeConfigHostMintedPerClaimFailClosed(t *testing.T) {
	t.Parallel()

	for name, rewrite := range map[string]func(t *testing.T, path string){
		"missing mid-life": func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove() error = %v, want nil", err)
			}
		},
		"malformed rewrite": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{"token": `), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v, want nil", err)
			}
		},
		"scope mismatch rewrite": func(t *testing.T, path string) {
			t.Helper()
			writeHostFileToken(t, path, "host-token-secret", "ci-cd:github-actions:other/repo")
		},
		"empty token rewrite": func(t *testing.T, path string) {
			t.Helper()
			writeHostFileToken(t, path, "   ", hostFileTestScopeID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "host-token.json")
			writeHostFileToken(t, path, "host-token-secret", hostFileTestScopeID)
			client := &hostFileRecordingClient{}
			source := loadHostFileSource(t, path, client)
			rewrite(t, path)
			_, _, err := source.NextClaimed(context.Background(), hostFileTestWorkItem("generation-1"))
			assertHostFileAuthDenied(t, err, "host-token-secret")
			if got := client.spentTokens(); len(got) != 0 {
				t.Fatalf("spent tokens = %q, want none (resolution must precede FetchRuns)", got)
			}
		})
	}
}

// TestLoadClaimedRuntimeConfigHostMintedFailClosedAtStartup pins fail-closed
// startup: an unresolvable host token file is a load error that names the
// file without carrying any credential value.
func TestLoadClaimedRuntimeConfigHostMintedFailClosedAtStartup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host-token.json")
	writeHostFileToken(t, path, "host-token-a", hostFileTestScopeID)
	for name, setup := range map[string]func(t *testing.T, path string){
		"missing file": func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove() error = %v, want nil", err)
			}
		},
		"malformed file": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{"token": `), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v, want nil", err)
			}
		},
		"scope mismatch file": func(t *testing.T, path string) {
			t.Helper()
			writeHostFileToken(t, path, "host-token-secret", "ci-cd:github-actions:other/repo")
		},
	} {
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
			setup(t, target)
			_, err = loadClaimedRuntimeConfig(func(key string) string {
				if key == envCollectorInstances {
					return hostFileTestInstancesJSON(target)
				}
				return ""
			})
			if err == nil {
				t.Fatal("loadClaimedRuntimeConfig() error = nil, want fail-closed startup error")
			}
			if !strings.Contains(err.Error(), target) {
				t.Fatalf("loadClaimedRuntimeConfig() error = %q, want it to name the file", err.Error())
			}
			if strings.Contains(err.Error(), "host-token-secret") {
				t.Fatalf("loadClaimedRuntimeConfig() error = %q, want no credential value", err.Error())
			}
		})
	}
}

// TestMapTargetHostMintedReadsFileAtStartup pins the lane selection: a
// github_actions target with auth_mode "host_minted" resolves no credential
// into the target (the Bearer resolves per claim), needs no token_env, and
// carries no App private key in satellite config, image, or env.
func TestMapTargetHostMintedReadsFileAtStartup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host-token.json")
	writeHostFileToken(t, path, "host-minted-token", "ci-cd:github-actions:example-org/example-repo")
	mapped, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		AuthMode:            ghactionsruntime.AuthModeHostMinted,
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		HostTokenFile:       path,
		AllowedRepositories: []string{"example-org/example-repo"},
		APIBaseURL:          "https://api.github.com",
		MaxRuns:             1,
		MaxJobs:             25,
		MaxArtifacts:        25,
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("mapTarget() error = %v, want nil", err)
	}
	if got := mapped.Token; got != "" {
		t.Fatalf("Token = %q, want empty (resolved per claim, never frozen at startup)", got)
	}
	if got, want := mapped.AuthMode, ghactionsruntime.AuthModeHostMinted; got != want {
		t.Fatalf("AuthMode = %q, want %q", got, want)
	}
	if got := mapped.HostTokenFile; got != path {
		t.Fatalf("HostTokenFile = %q, want %q", got, path)
	}
	// The mapped target must be admissible to the production runtime path,
	// which spends the per-claim Bearer either way.
	if _, err := ghactionsruntime.NewClaimedSource(ghactionsruntime.SourceConfig{
		CollectorInstanceID: "cicd-run-primary",
		Client:              &hostFileRecordingClient{},
		Targets:             []ghactionsruntime.TargetConfig{mapped},
	}); err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
}

// TestMapTargetTokenEnvByteIdentical is the #9186 compat proof: a target
// without auth_mode maps every pre-existing TargetConfig field exactly as
// before, so existing token_env activations behave byte-identically.
func TestMapTargetTokenEnvByteIdentical(t *testing.T) {
	t.Parallel()

	mapped, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		TokenEnv:            "GITHUB_TOKEN",
		AllowedRepositories: []string{"example-org/example-repo"},
		APIBaseURL:          "https://api.github.com",
		MaxRuns:             1,
		MaxJobs:             25,
		MaxArtifacts:        25,
	}, func(key string) string {
		if key == "GITHUB_TOKEN" {
			return "token-value"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("mapTarget() error = %v, want nil", err)
	}
	want := ghactionsruntime.TargetConfig{
		AuthMode:            ghactionsruntime.AuthModeTokenEnv,
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		Token:               "token-value",
		AllowedRepositories: []string{"example-org/example-repo"},
		APIBaseURL:          "https://api.github.com",
		SourceURI:           "https://github.com/example-org/example-repo",
		MaxRuns:             1,
		MaxJobs:             25,
		MaxArtifacts:        25,
	}
	if mapped.ScopeID != want.ScopeID ||
		mapped.Repository != want.Repository ||
		mapped.Token != want.Token ||
		mapped.AuthMode != want.AuthMode ||
		mapped.APIBaseURL != want.APIBaseURL ||
		mapped.SourceURI != want.SourceURI ||
		mapped.MaxRuns != want.MaxRuns ||
		mapped.MaxJobs != want.MaxJobs ||
		mapped.MaxArtifacts != want.MaxArtifacts ||
		len(mapped.AllowedRepositories) != len(want.AllowedRepositories) ||
		mapped.AllowedRepositories[0] != want.AllowedRepositories[0] {
		t.Fatalf("mapTarget() = %+v, want %+v", mapped, want)
	}
	if mapped.HostTokenFile != "" {
		t.Fatalf("HostTokenFile = %q, want empty in token_env mode", mapped.HostTokenFile)
	}
}

// TestMapTargetNeverMixesCredentialShapes mirrors the alertruntime dual-mode
// table: the two credential shapes never mix on one target, in either
// direction.
func TestMapTargetNeverMixesCredentialShapes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host-token.json")
	writeHostFileToken(t, path, "host-minted-token", "ci-cd:github-actions:example-org/example-repo")
	for name, target := range map[string]targetJSON{
		"host_minted with token_env": {
			Provider:            "github_actions",
			AuthMode:            ghactionsruntime.AuthModeHostMinted,
			ScopeID:             "ci-cd:github-actions:example-org/example-repo",
			Repository:          "example-org/example-repo",
			TokenEnv:            "GITHUB_TOKEN",
			HostTokenFile:       path,
			AllowedRepositories: []string{"example-org/example-repo"},
		},
		"token_env with host_token_file": {
			Provider:            "github_actions",
			ScopeID:             "ci-cd:github-actions:example-org/example-repo",
			Repository:          "example-org/example-repo",
			TokenEnv:            "GITHUB_TOKEN",
			HostTokenFile:       path,
			AllowedRepositories: []string{"example-org/example-repo"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mapTarget(target, func(string) string { return "some-token" })
			if err == nil {
				t.Fatal("mapTarget() error = nil, want cross-mode refusal")
			}
		})
	}
}

// TestMapTargetHostMintedRequiresResolvingFile pins fail-closed startup
// when the host has not populated the file: the error names the file
// without carrying any credential value.
func TestMapTargetHostMintedRequiresResolvingFile(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "no-such-token.json")
	for name, file := range map[string]string{
		"missing": missing,
		"blank":   "   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mapTarget(targetJSON{
				Provider:            "github_actions",
				AuthMode:            ghactionsruntime.AuthModeHostMinted,
				ScopeID:             "ci-cd:github-actions:example-org/example-repo",
				Repository:          "example-org/example-repo",
				HostTokenFile:       file,
				AllowedRepositories: []string{"example-org/example-repo"},
			}, func(string) string { return "" })
			if err == nil {
				t.Fatal("mapTarget() error = nil, want missing credential error")
			}
			if name == "missing" && !strings.Contains(err.Error(), missing) {
				t.Fatalf("mapTarget() error = %q, want it to name the file reference", err)
			}
		})
	}
}

// TestMapTargetRejectsUnknownAuthMode keeps the mode vocabulary closed so
// a typo fails at startup instead of silently running as a static token.
func TestMapTargetRejectsUnknownAuthMode(t *testing.T) {
	t.Parallel()

	_, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		AuthMode:            "magic",
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		TokenEnv:            "GITHUB_TOKEN",
		AllowedRepositories: []string{"example-org/example-repo"},
	}, func(key string) string { return "some-token" })
	if err == nil {
		t.Fatal("mapTarget() error = nil, want unsupported mode error")
	}
	if !strings.Contains(err.Error(), "magic") {
		t.Fatalf("mapTarget() error = %q, want it to name the mode", err)
	}
}

// TestMapGitLabTargetIgnoresTokenFileFields pins the #9186 non-scope:
// gitlab_ci targets keep their exact mapping with the new github_actions
// fields present, mirroring the MaxArtifacts precedent.
func TestMapGitLabTargetIgnoresTokenFileFields(t *testing.T) {
	t.Parallel()

	mapped, err := mapGitLabTarget(targetJSON{
		Provider:            "gitlab_ci",
		AuthMode:            ghactionsruntime.AuthModeHostMinted,
		ScopeID:             "ci-cd:gitlab-ci:example-group/example-project",
		Repository:          "example-group/example-project",
		TokenEnv:            "GITLAB_TOKEN",
		HostTokenFile:       "/var/run/secrets/github/token.json",
		AllowedRepositories: []string{"example-group/example-project"},
		MaxRuns:             1,
		MaxJobs:             25,
	}, func(key string) string {
		if key == "GITLAB_TOKEN" {
			return "gitlab-token-value"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("mapGitLabTarget() error = %v, want nil", err)
	}
	if got, want := mapped.Token, "gitlab-token-value"; got != want {
		t.Fatalf("Token = %q, want %q", got, want)
	}
	if got, want := mapped.ProjectPath, "example-group/example-project"; got != want {
		t.Fatalf("ProjectPath = %q, want %q", got, want)
	}
}
