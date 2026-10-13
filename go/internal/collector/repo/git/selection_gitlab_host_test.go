// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestRepoRemoteURLUsesCustomGitlabHost is the #7764 regression: with
// ESHU_GITLAB_HOST set, explicit-mode gitlab/ repositories must clone and
// fetch from the configured host over SSH and HTTPS instead of gitlab.com.
func TestRepoRemoteURLUsesCustomGitlabHost(t *testing.T) {
	t.Parallel()

	config := RepoSyncConfig{GitlabHost: "gitlab.acme.example"}
	tests := []struct {
		name   string
		auth   string
		repoID string
		want   string
	}{
		{
			name:   "gitlab https custom host",
			repoID: "gitlab/acme/app",
			want:   "https://gitlab.acme.example/acme/app.git",
		},
		{
			name:   "gitlab ssh custom host",
			auth:   "ssh",
			repoID: "gitlab/acme/app",
			want:   "git@gitlab.acme.example:acme/app.git",
		},
		{
			name:   "gitlab nested subgroup https custom host",
			repoID: "gitlab/acme/platform/payments/app",
			want:   "https://gitlab.acme.example/acme/platform/payments/app.git",
		},
		{
			name:   "github still uses github.com",
			repoID: "acme/app",
			want:   "https://github.com/acme/app.git",
		},
		{
			name:   "bitbucket still uses bitbucket.org",
			repoID: "bitbucket/acme/app",
			want:   "https://bitbucket.org/acme/app.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config
			cfg.GitAuthMethod = tt.auth
			if got := repoRemoteURL(cfg, tt.repoID); got != tt.want {
				t.Fatalf("repoRemoteURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRepoRemoteURLDefaultGitlabHostStable pins that an unset host behaves
// exactly like the documented gitlab.com default, over both transports, so
// existing deployments see byte-identical remote URLs after the upgrade.
func TestRepoRemoteURLDefaultGitlabHostStable(t *testing.T) {
	t.Parallel()

	for _, auth := range []string{"", "ssh", "token"} {
		unset := RepoSyncConfig{GitAuthMethod: auth}
		deflt := RepoSyncConfig{GitAuthMethod: auth, GitlabHost: "gitlab.com"}
		for _, repoID := range []string{"gitlab/acme/app", "gitlab/acme/platform/app"} {
			if got, want := repoRemoteURL(unset, repoID), repoRemoteURL(deflt, repoID); got != want {
				t.Fatalf("auth %q repo %q: unset host URL %q != default host URL %q", auth, repoID, got, want)
			}
		}
	}
	if got, want := repoRemoteURL(RepoSyncConfig{}, "gitlab/acme/app"), "https://gitlab.com/acme/app.git"; got != want {
		t.Fatalf("repoRemoteURL() = %q, want %q", got, want)
	}
	if got, want := repoRemoteURL(RepoSyncConfig{GitAuthMethod: "ssh"}, "gitlab/acme/app"), "git@gitlab.com:acme/app.git"; got != want {
		t.Fatalf("repoRemoteURL() = %q, want %q", got, want)
	}
}

// TestRepositoryIdentityStableOnDefaultGitlabHost pins the second #7764
// acceptance item: repository identity (the hashed canonical id) is unchanged
// when the host is the default, whether the config leaves it unset or names
// gitlab.com explicitly, over both transports.
func TestRepositoryIdentityStableOnDefaultGitlabHost(t *testing.T) {
	t.Parallel()

	configs := map[string]RepoSyncConfig{
		"unset host https":   {},
		"default host https": {GitlabHost: "gitlab.com"},
		"unset host ssh":     {GitAuthMethod: "ssh"},
		"default host ssh":   {GitAuthMethod: "ssh", GitlabHost: "gitlab.com"},
		"unset host token":   {GitAuthMethod: "token"},
		"default host token": {GitAuthMethod: "token", GitlabHost: "gitlab.com"},
	}
	ids := map[string]string{}
	for name, config := range configs {
		metadata, err := repositoryidentity.MetadataFor("app", t.TempDir(), repoRemoteURL(config, "gitlab/acme/app"))
		if err != nil {
			t.Fatalf("%s: MetadataFor: %v", name, err)
		}
		ids[name] = metadata.ID
	}
	// SSH and HTTPS normalise to one canonical remote, so every transport and
	// both default spellings share the unset-host identity.
	for name, id := range ids {
		if id != ids["unset host https"] {
			t.Fatalf("%s: identity %q != canonical %q", name, id, ids["unset host https"])
		}
	}
}

// TestLoadRepoSyncConfigParsesGitlabHost pins the ESHU_GITLAB_HOST contract:
// unset keeps the gitlab.com default, set values normalise, and unusable
// values fail config load closed instead of cloning from a wrong host.
func TestLoadRepoSyncConfigParsesGitlabHost(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name  string
		value string
		want  string
	}{
		{"unset defaults", "", "gitlab.com"},
		{"plain hostname", "gitlab.acme.example", "gitlab.acme.example"},
		{"surrounding whitespace is trimmed", "  gitlab.acme.example  ", "gitlab.acme.example"},
		{"uppercase is lowercased", "GitLab.Acme.Example", "gitlab.acme.example"},
		{"trailing dot is stripped", "gitlab.acme.example.", "gitlab.acme.example"},
		{"single label intranet host", "gitlab", "gitlab"},
		{"underscores accepted for intranet hosts", "gitlab_corp", "gitlab_corp"},
		{"underscore host is lowercased", "GitLab_Corp.Example", "gitlab_corp.example"},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
				"ESHU_GITLAB_HOST": tt.value,
			}))
			if err != nil {
				t.Fatalf("LoadRepoSyncConfig() error = %v", err)
			}
			if config.GitlabHost != tt.want {
				t.Fatalf("GitlabHost = %q, want %q", config.GitlabHost, tt.want)
			}
		})
	}

	invalid := map[string]string{
		"URL scheme is rejected":       "https://gitlab.acme.example",
		"path is rejected":             "gitlab.acme.example/group",
		"port is rejected":             "gitlab.acme.example:2222",
		"userinfo is rejected":         "git@gitlab.acme.example",
		"inner whitespace is rejected": "gitlab .example",
		"empty label is rejected":      "gitlab..example",
		"label leading hyphen":         "-gitlab.example",
		"label trailing hyphen":        "gitlab-.example",
	}
	for name, value := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
				"ESHU_GITLAB_HOST": value,
			})); err == nil {
				t.Fatalf("LoadRepoSyncConfig() with ESHU_GITLAB_HOST=%q succeeded, want an error naming the variable", value)
			} else if !strings.Contains(err.Error(), "ESHU_GITLAB_HOST") {
				t.Fatalf("LoadRepoSyncConfig() error %q does not name ESHU_GITLAB_HOST", err)
			}
		})
	}
}

// TestGitCommandEnvScopesTokenHeaderToCustomGitlabHost pins that the #7763
// header scope follows the configured host: with a custom host the credential
// goes to that host, and the header still names the host the collector clones
// from.
func TestGitCommandEnvScopesTokenHeaderToCustomGitlabHost(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	config := RepoSyncConfig{GitAuthMethod: "token", ReposDir: reposDir, GitlabHost: "gitlab.acme.example"}
	repoPath := filepath.Join(reposDir, "gitlab", "acme", "app")

	headers := extraHeaderEntries(t, gitCommandEnv(config, "tok-secret", repoPath))
	const wantKey = "http.https://gitlab.acme.example/.extraheader"
	if _, ok := headers[wantKey]; !ok || len(headers) != 1 {
		t.Fatalf("extraheader entries %v, want only %q", headers, wantKey)
	}
	if got, want := basicCredential(t, headers[wantKey]), "oauth2:tok-secret"; got != want {
		t.Fatalf("basic credential = %q, want %q", got, want)
	}

	remote, err := url.Parse(repoRemoteURL(config, "gitlab/acme/app"))
	if err != nil {
		t.Fatalf("parse remote URL: %v", err)
	}
	if remote.Host != "gitlab.acme.example" {
		t.Fatalf("clone URL host = %q, want %q", remote.Host, "gitlab.acme.example")
	}
}
