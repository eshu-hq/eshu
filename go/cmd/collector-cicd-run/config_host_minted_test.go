// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
)

// TestMapTargetHostMintedSelectsHostToken pins the #9186 satellite
// selection: a github_actions target with auth_mode "host_minted" resolves
// its bearer from host_token_env (the host-maintained GitHub App
// installation token, #9184), needs no token_env, and carries no App
// private key in satellite config, image, or env.
func TestMapTargetHostMintedSelectsHostToken(t *testing.T) {
	t.Parallel()

	mapped, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		AuthMode:            ghactionsruntime.AuthModeHostMinted,
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		HostTokenEnv:        "ESHU_HOST_GITHUB_TOKEN",
		AllowedRepositories: []string{"example-org/example-repo"},
		APIBaseURL:          "https://api.github.com",
		MaxRuns:             1,
		MaxJobs:             25,
		MaxArtifacts:        25,
	}, func(key string) string {
		if key == "ESHU_HOST_GITHUB_TOKEN" {
			return "host-minted-token"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("mapTarget() error = %v, want nil", err)
	}
	if got, want := mapped.Token, "host-minted-token"; got != want {
		t.Fatalf("Token = %q, want %q", got, want)
	}
	if got, want := mapped.AuthMode, ghactionsruntime.AuthModeHostMinted; got != want {
		t.Fatalf("AuthMode = %q, want %q", got, want)
	}
	if got, want := mapped.ScopeID, "ci-cd:github-actions:example-org/example-repo"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := mapped.Repository, "example-org/example-repo"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := mapped.SourceURI, "https://github.com/example-org/example-repo"; got != want {
		t.Fatalf("SourceURI = %q, want %q", got, want)
	}
	// The mapped target must be admissible to the production runtime path,
	// which spends Token as the Bearer credential either way.
	if _, err := ghactionsruntime.NewClaimedSource(ghactionsruntime.SourceConfig{
		CollectorInstanceID: "cicd-run-primary",
		Client:              ghactionsruntime.GitHubClient{},
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
}

// TestMapTargetHostMintedRefusesTokenEnvMix mirrors the alertruntime
// dual-mode table: the two credential shapes never mix on one target.
func TestMapTargetHostMintedRefusesTokenEnvMix(t *testing.T) {
	t.Parallel()

	_, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		AuthMode:            ghactionsruntime.AuthModeHostMinted,
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		TokenEnv:            "GITHUB_TOKEN",
		HostTokenEnv:        "ESHU_HOST_GITHUB_TOKEN",
		AllowedRepositories: []string{"example-org/example-repo"},
	}, func(key string) string { return "some-token" })
	if err == nil {
		t.Fatal("mapTarget() error = nil, want cross-mode refusal")
	}
	if !strings.Contains(err.Error(), "token_env") {
		t.Fatalf("mapTarget() error = %q, want it to name token_env", err)
	}
}

// TestMapTargetTokenEnvRefusesHostTokenEnvMix is the mirror refusal:
// a static token_env target must not silently accept a host ref.
func TestMapTargetTokenEnvRefusesHostTokenEnvMix(t *testing.T) {
	t.Parallel()

	_, err := mapTarget(targetJSON{
		Provider:            "github_actions",
		ScopeID:             "ci-cd:github-actions:example-org/example-repo",
		Repository:          "example-org/example-repo",
		TokenEnv:            "GITHUB_TOKEN",
		HostTokenEnv:        "ESHU_HOST_GITHUB_TOKEN",
		AllowedRepositories: []string{"example-org/example-repo"},
	}, func(key string) string { return "some-token" })
	if err == nil {
		t.Fatal("mapTarget() error = nil, want cross-mode refusal")
	}
	if !strings.Contains(err.Error(), "host_token_env") {
		t.Fatalf("mapTarget() error = %q, want it to name host_token_env", err)
	}
}

// TestMapTargetHostMintedRequiresResolvingEnv pins fail-closed startup
// when the host has not populated the named variable: the error names the
// reference without carrying any credential value.
func TestMapTargetHostMintedRequiresResolvingEnv(t *testing.T) {
	t.Parallel()

	for name, getenv := range map[string]func(string) string{
		"missing": func(string) string { return "" },
		"blank":   func(string) string { return "   " },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mapTarget(targetJSON{
				Provider:            "github_actions",
				AuthMode:            ghactionsruntime.AuthModeHostMinted,
				ScopeID:             "ci-cd:github-actions:example-org/example-repo",
				Repository:          "example-org/example-repo",
				HostTokenEnv:        "ESHU_HOST_GITHUB_TOKEN",
				AllowedRepositories: []string{"example-org/example-repo"},
			}, getenv)
			if err == nil {
				t.Fatal("mapTarget() error = nil, want missing credential error")
			}
			if !strings.Contains(err.Error(), "ESHU_HOST_GITHUB_TOKEN") {
				t.Fatalf("mapTarget() error = %q, want it to name the env reference", err)
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

// TestMapGitLabTargetIgnoresAuthModeFields pins the #9186 non-scope:
// gitlab_ci targets keep their exact mapping with the new github_actions
// fields present, mirroring the MaxArtifacts precedent.
func TestMapGitLabTargetIgnoresAuthModeFields(t *testing.T) {
	t.Parallel()

	mapped, err := mapGitLabTarget(targetJSON{
		Provider:            "gitlab_ci",
		AuthMode:            ghactionsruntime.AuthModeHostMinted,
		ScopeID:             "ci-cd:gitlab-ci:example-group/example-project",
		Repository:          "example-group/example-project",
		TokenEnv:            "GITLAB_TOKEN",
		HostTokenEnv:        "ESHU_HOST_GITHUB_TOKEN",
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
