// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func buildSSHCommand(config RepoSyncConfig) string {
	privateKeyPath := strings.TrimSpace(config.SSHPrivateKeyPath)
	if privateKeyPath == "" {
		privateKeyPath = "/var/run/secrets/eshu-ssh/id_rsa"
	}
	knownHostsPath := strings.TrimSpace(config.SSHKnownHostsPath)
	if knownHostsPath == "" {
		knownHostsPath = "/var/run/secrets/eshu-ssh/known_hosts"
	}
	strictHosts := "no"
	knownHostsOpt := ""
	if _, err := os.Stat(knownHostsPath); err == nil {
		strictHosts = "yes"
		knownHostsOpt = fmt.Sprintf("-o UserKnownHostsFile=%s", knownHostsPath)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"ssh -i %s %s -o StrictHostKeyChecking=%s",
		privateKeyPath,
		knownHostsOpt,
		strictHosts,
	))
}

// gitCommandEnv returns the environment for a managed git command: the
// process environment, auth settings for config's method, and LC_ALL=C.
// Callers match git stderr by its English text (gitMissingRemoteRef,
// recoverStaleGitShallowLock), so messages must never be translated.
//
// repoPath is the managed checkout the command runs against (or clones
// into). Token auth derives the remote's provider from it, so the credential
// header is scoped to the host the remote URL was built for and to no other.
func gitCommandEnv(config RepoSyncConfig, token string, repoPath string) []string {
	env := append(os.Environ(), "LC_ALL=C")
	authMethod := strings.ToLower(strings.TrimSpace(config.GitAuthMethod))
	switch authMethod {
	case "token", "githubapp":
		if strings.TrimSpace(token) == "" {
			return env
		}
		// A GitHub App installation token only authenticates to GitHub, so
		// it is never offered to another provider's host.
		provider, ok := "github", true
		if authMethod == "token" {
			provider, ok = tokenAuthProvider(config, repoPath)
		}
		if !ok {
			// Not a managed checkout: no remote URL was built for this path,
			// so no host is owed the credential. Fail closed instead of
			// defaulting to github.com.
			return env
		}
		env = append(
			env,
			fmt.Sprintf("GIT_CONFIG_COUNT=%d", 1),
			"GIT_CONFIG_KEY_0=http.https://"+repoProviderHost(config, provider)+"/.extraheader",
			"GIT_CONFIG_VALUE_0="+httpBasicExtraHeader(tokenAuthUsername(provider), token),
		)
	case "ssh":
		command := buildSSHCommand(config)
		if command != "" {
			env = append(env, "GIT_SSH_COMMAND="+command)
		}
	}
	return env
}

// tokenAuthProvider returns the provider whose host a token-auth git command
// talks to, derived from the managed checkout path exactly as repoRemoteURL
// derives the remote: <ReposDir>/<provider>/<slug> names its provider, and a
// path with no provider prefix is a GitHub repository.
//
// The second result is false when the path is not a checkout the collector
// would clone, judged by the same repoCheckoutName rule the clone path uses:
// a path outside ReposDir yields no repository ID, and the reserved .eshu-
// namespace that holds ref worktrees is never cloned. Such a command gets no
// credential at all rather than a header a wrong host could receive.
func tokenAuthProvider(config RepoSyncConfig, repoPath string) (string, bool) {
	repoID := repoIDFromManagedPath(config.ReposDir, repoPath)
	if _, err := repoCheckoutName(repoID); err != nil {
		return "", false
	}
	provider, _ := repoProviderAndSlug(repoID)
	if provider == "" {
		return "github", true
	}
	return provider, true
}

// tokenAuthUsername returns the HTTP Basic username each provider expects
// alongside an access token: GitHub's x-access-token, GitLab's documented
// oauth2 (accepted for personal, group, and project access tokens), and
// Bitbucket's x-token-auth for repository access tokens.
func tokenAuthUsername(provider string) string {
	switch provider {
	case "gitlab":
		return "oauth2"
	case "bitbucket":
		return "x-token-auth"
	default:
		return "x-access-token"
	}
}

// httpBasicExtraHeader renders the http.<url>.extraheader value that sends
// username and token as HTTP Basic credentials.
func httpBasicExtraHeader(username string, token string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	return "AUTHORIZATION: basic " + encoded
}

// gitRetryHost returns the provider host a network git command at repoPath
// talks to, for the bounded host label of the retry metrics (#8091). It uses
// the same checkout-path-to-provider rule and the same host source
// (repoProviderHost) as the clone URL and the token header, so a managed
// checkout reports github.com, bitbucket.org, or the configured GitLab host,
// and any path outside the managed layout reports "unknown". It never reads
// the remote URL, so no path or credential can reach a label.
func gitRetryHost(config RepoSyncConfig, repoPath string) string {
	provider, ok := tokenAuthProvider(config, repoPath)
	if !ok {
		return "unknown"
	}
	return repoProviderHost(config, provider)
}

// gitRetryScope returns the repository identity for the retry log line and
// span event: the managed checkout's repository ID, or the path's base name
// for a checkout outside ReposDir. It is never a metric label.
func gitRetryScope(config RepoSyncConfig, repoPath string) string {
	if repoID := repoIDFromManagedPath(config.ReposDir, repoPath); repoID != "" {
		return repoID
	}
	return filepath.Base(repoPath)
}

// usesSSHRemote reports whether the collector builds SSH remote URLs, the
// one transport the resolve cache must not pin (#8092): http.curloptResolve
// is a libcurl option and an SSH remote does its own lookup.
func usesSSHRemote(config RepoSyncConfig) bool {
	return strings.EqualFold(strings.TrimSpace(config.GitAuthMethod), "ssh")
}

// resolveCacheArgs returns the curloptResolve pin for a network command
// against the managed checkout at repoPath, or nil to run it unpinned. The
// host comes from the same checkout-path rule as the retry host label and
// the clone URL, so the pin always names the host the origin remote was
// built for. Every non-SSH remote this collector builds is HTTPS, so the
// port is 443.
func resolveCacheArgs(ctx context.Context, config RepoSyncConfig, repoPath string) []string {
	if config.ResolveCache == nil || usesSSHRemote(config) {
		return nil
	}
	return config.ResolveCache.Args(ctx, gitRetryHost(config, repoPath), 443)
}

// resolveCacheArgsForURL returns the curloptResolve pin for a clone of
// remoteURL. The host and port come from the URL itself — 443 for https,
// 80 for http, or an explicit port — and any other form (the scp-like
// git@host:path an SSH remote builds) pins nothing. Only the remote URL's
// host is pinned; a redirect to another host resolves normally.
func resolveCacheArgsForURL(ctx context.Context, config RepoSyncConfig, remoteURL string) []string {
	if config.ResolveCache == nil || usesSSHRemote(config) {
		return nil
	}
	parsed, err := url.Parse(remoteURL)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	port := 0
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		port = 443
	case "http":
		port = 80
	default:
		return nil
	}
	if explicit := parsed.Port(); explicit != "" {
		parsedPort, err := strconv.Atoi(explicit)
		if err != nil || parsedPort <= 0 {
			return nil
		}
		port = parsedPort
	}
	return config.ResolveCache.Args(ctx, parsed.Hostname(), port)
}
