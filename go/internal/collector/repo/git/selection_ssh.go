// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"encoding/base64"
	"fmt"
	"os"
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
		provider := "github"
		if authMethod == "token" {
			provider = tokenAuthProvider(config, repoPath)
		}
		env = append(
			env,
			fmt.Sprintf("GIT_CONFIG_COUNT=%d", 1),
			"GIT_CONFIG_KEY_0=http.https://"+repoProviderHost(provider)+"/.extraheader",
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
// path with no provider prefix (or outside ReposDir) is a GitHub repository.
func tokenAuthProvider(config RepoSyncConfig, repoPath string) string {
	provider, _ := repoProviderAndSlug(repoIDFromManagedPath(config.ReposDir, repoPath))
	if provider == "" {
		return "github"
	}
	return provider
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
