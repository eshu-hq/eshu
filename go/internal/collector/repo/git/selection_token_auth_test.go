// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"encoding/base64"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// extraHeaderEntries returns every http.<url>.extraheader key gitCommandEnv
// configured through GIT_CONFIG_KEY_<n>, mapped to its GIT_CONFIG_VALUE_<n>.
func extraHeaderEntries(t *testing.T, env []string) map[string]string {
	t.Helper()
	keys := map[string]string{}
	values := map[string]string{}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if index, found := strings.CutPrefix(name, "GIT_CONFIG_KEY_"); found {
			keys[index] = value
		}
		if index, found := strings.CutPrefix(name, "GIT_CONFIG_VALUE_"); found {
			values[index] = value
		}
	}
	headers := map[string]string{}
	for index, key := range keys {
		if strings.HasSuffix(key, ".extraheader") {
			headers[key] = values[index]
		}
	}
	return headers
}

// basicCredential decodes an "AUTHORIZATION: basic <b64>" header value into
// its user:password pair.
func basicCredential(t *testing.T, header string) string {
	t.Helper()
	const prefix = "authorization: basic "
	if !strings.HasPrefix(strings.ToLower(header), prefix) {
		t.Fatalf("header %q is not HTTP Basic", header)
	}
	decoded, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		t.Fatalf("decode header %q: %v", header, err)
	}
	return string(decoded)
}

// TestGitCommandEnvScopesTokenHeaderToRemoteHost is the #7763 regression:
// token auth must send its header to the host the repository is cloned from,
// with that provider's Basic username. Before the fix the header was always
// scoped to https://github.com/, so a gitlab/ or bitbucket/ clone went out
// anonymous and failed on a private repository.
func TestGitCommandEnvScopesTokenHeaderToRemoteHost(t *testing.T) {
	reposDir := t.TempDir()
	const token = "tok-secret"
	cases := []struct {
		name     string
		method   string
		repoID   string
		wantHost string
		wantUser string
	}{
		{"github prefix", "token", "github/acme/app", "github.com", "x-access-token"},
		{"bare owner/name defaults to github", "token", "acme/app", "github.com", "x-access-token"},
		{"gitlab", "token", "gitlab/acme/app", "gitlab.com", "oauth2"},
		{"gitlab nested subgroup", "token", "gitlab/acme/platform/payments/app", "gitlab.com", "oauth2"},
		{"bitbucket", "token", "bitbucket/acme/app", "bitbucket.org", "x-token-auth"},
		{"method is case-insensitive", "Token", "gitlab/acme/app", "gitlab.com", "oauth2"},
		// A GitHub App installation token only authenticates to GitHub;
		// it must never be offered to another provider's host.
		{"githubapp stays on github for github repo", "githubApp", "github/acme/app", "github.com", "x-access-token"},
		{"githubapp never sent to gitlab", "githubApp", "gitlab/acme/app", "github.com", "x-access-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := RepoSyncConfig{GitAuthMethod: tc.method, ReposDir: reposDir}
			repoPath := filepath.Join(reposDir, filepath.FromSlash(tc.repoID))

			headers := extraHeaderEntries(t, gitCommandEnv(config, token, repoPath))

			wantKey := "http.https://" + tc.wantHost + "/.extraheader"
			if len(headers) != 1 {
				t.Fatalf("got %d extraheader entries %v, want exactly %q", len(headers), headers, wantKey)
			}
			value, ok := headers[wantKey]
			if !ok {
				t.Fatalf("extraheader scoped to %v, want %q", headers, wantKey)
			}
			if got, want := basicCredential(t, value), tc.wantUser+":"+token; got != want {
				t.Fatalf("basic credential = %q, want %q", got, want)
			}
		})
	}
}

// TestGitCommandEnvTokenHeaderMatchesCloneURLHost ties the header scope to
// the URL the collector actually clones: for every provider the HTTPS remote
// built by repoRemoteURL and the extraheader key must name the same host, so
// git sends the credential to that remote and to no other.
func TestGitCommandEnvTokenHeaderMatchesCloneURLHost(t *testing.T) {
	reposDir := t.TempDir()
	config := RepoSyncConfig{GitAuthMethod: "token", ReposDir: reposDir}
	for _, repoID := range []string{
		"github/acme/app",
		"acme/app",
		"gitlab/acme/app",
		"gitlab/acme/platform/app",
		"bitbucket/acme/app",
	} {
		remote, err := url.Parse(repoRemoteURL(config, repoID))
		if err != nil {
			t.Fatalf("%s: parse remote URL: %v", repoID, err)
		}
		repoPath := filepath.Join(reposDir, filepath.FromSlash(repoID))
		headers := extraHeaderEntries(t, gitCommandEnv(config, "tok", repoPath))
		wantKey := "http.https://" + remote.Host + "/.extraheader"
		if _, ok := headers[wantKey]; !ok || len(headers) != 1 {
			t.Fatalf("%s: clone URL %s but extraheader entries %v, want only %q",
				repoID, remote, headers, wantKey)
		}
	}
}

// TestGitCommandEnvTokenAuthWithoutTokenSendsNoHeader keeps the existing
// contract: an empty token adds no credential header for any provider.
func TestGitCommandEnvTokenAuthWithoutTokenSendsNoHeader(t *testing.T) {
	reposDir := t.TempDir()
	for _, method := range []string{"token", "githubApp"} {
		config := RepoSyncConfig{GitAuthMethod: method, ReposDir: reposDir}
		repoPath := filepath.Join(reposDir, "gitlab", "acme", "app")
		if headers := extraHeaderEntries(t, gitCommandEnv(config, "  ", repoPath)); len(headers) != 0 {
			t.Fatalf("%s: empty token produced extraheader entries %v", method, headers)
		}
	}
}

// TestGitCommandEnvTokenAuthUnmanagedPathSendsNoHeader pins the fail-closed
// side of the host-scope contract: a path that does not resolve to a
// repository the collector would clone gets no credential header at all,
// not a header scoped to github.com. A path outside ReposDir has no remote,
// and a ref worktree lives under the reserved .eshu- namespace inside
// ReposDir and runs only local commands. Sending the token to github.com from
// either would offer a GitLab or Bitbucket credential to the wrong host if a
// network command were ever run there.
func TestGitCommandEnvTokenAuthUnmanagedPathSendsNoHeader(t *testing.T) {
	reposDir := t.TempDir()
	config := RepoSyncConfig{GitAuthMethod: "token", ReposDir: reposDir}
	cases := []struct {
		name     string
		repoPath string
	}{
		{"outside ReposDir", filepath.Join(t.TempDir(), "gitlab", "acme", "app")},
		{"ref worktree under reserved namespace", filepath.Join(reposDir, ".eshu-ref-worktrees", "gitlab", "acme", "app", "main")},
		{"ReposDir itself", reposDir},
		{"empty path", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := extraHeaderEntries(t, gitCommandEnv(config, "tok-secret", tc.repoPath))
			if len(headers) != 0 {
				t.Fatalf("unmanaged path %q produced extraheader entries %v, want none", tc.repoPath, headers)
			}
		})
	}
}

// TestGitCommandEnvGithubAppIgnoresUnmanagedPath pins that the fail-closed
// rule is token-mode only. A GitHub App installation token authenticates
// nowhere but github.com, so it is scoped there from any path.
func TestGitCommandEnvGithubAppIgnoresUnmanagedPath(t *testing.T) {
	reposDir := t.TempDir()
	config := RepoSyncConfig{GitAuthMethod: "githubApp", ReposDir: reposDir}
	outside := filepath.Join(t.TempDir(), "gitlab", "acme", "app")
	headers := extraHeaderEntries(t, gitCommandEnv(config, "tok-secret", outside))
	const wantKey = "http.https://github.com/.extraheader"
	if _, ok := headers[wantKey]; !ok || len(headers) != 1 {
		t.Fatalf("githubApp from unmanaged path produced %v, want only %q", headers, wantKey)
	}
}
