// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
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

func gitCommandEnv(config RepoSyncConfig, token string) []string {
	env := os.Environ()
	authMethod := strings.ToLower(strings.TrimSpace(config.GitAuthMethod))
	switch authMethod {
	case "token", "githubapp":
		if strings.TrimSpace(token) == "" {
			return env
		}
		env = append(
			env,
			fmt.Sprintf("GIT_CONFIG_COUNT=%d", 1),
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0="+githubHTTPExtraHeader(token),
		)
	case "ssh":
		command := buildSSHCommand(config)
		if command != "" {
			env = append(env, "GIT_SSH_COMMAND="+command)
		}
	}
	return env
}
