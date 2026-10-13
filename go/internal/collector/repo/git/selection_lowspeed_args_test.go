// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeRecordingGit installs a fake git that writes its argv (one per line) to
// argvFile (an "@@END@@" line ends each invocation) and its environment to envFile,
// then succeeds with no output.
func writeRecordingGit(t *testing.T, argvFile, envFile string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '" + argvFile + "'\nprintf '@@END@@\\n' >> '" + argvFile + "'\nenv > '" + envFile + "'\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

var wantLowSpeedArgs = []string{"-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=60"}

// assertLowSpeedBefore fails unless argv holds the low-speed -c pairs
// immediately before the named subcommand.
func assertLowSpeedBefore(t *testing.T, argv []string, subcommand string) {
	t.Helper()
	at := slices.Index(argv, subcommand)
	if at < 4 || !slices.Equal(argv[at-4:at], wantLowSpeedArgs) {
		t.Fatalf("argv = %v, want %v immediately before %q", argv, wantLowSpeedArgs, subcommand)
	}
}

// assertAuthHeaderEnvUntouched fails unless the environment still carries the
// single credential header entry gitCommandEnv owns, scoped to the GitHub host.
func assertAuthHeaderEnvUntouched(t *testing.T, envFile, token string) {
	t.Helper()
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.HasPrefix(k, "GIT_CONFIG") {
			env[k] = v
		}
	}
	want := map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0": "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)),
	}
	if len(env) != len(want) {
		t.Fatalf("GIT_CONFIG env = %v, want exactly %v", env, want)
	}
	for k, v := range want {
		if env[k] != v {
			t.Fatalf("%s = %q, want %q", k, env[k], v)
		}
	}
}

// TestFetchAndCloneCarryLowSpeedWithoutTouchingTheCredentialHeader pins both
// halves of the complement: fetch and clone abort a 0 byte/s transfer through
// -c argv options, and the credential header still travels alone in
// GIT_CONFIG_COUNT/KEY_0/VALUE_0, scoped to its host.
func TestFetchAndCloneCarryLowSpeedWithoutTouchingTheCredentialHeader(t *testing.T) {
	const token = "tkn"
	for _, tc := range []struct {
		name       string
		subcommand string
		run        func(config RepoSyncConfig, reposDir string)
	}{
		{"fetch", "fetch", func(config RepoSyncConfig, reposDir string) {
			repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
			_ = gitFetchBranch(context.Background(), config, repoPath, "main", token, nil, gitSyncLogEvent{}, "")
		}},
		{"clone", "clone", func(config RepoSyncConfig, reposDir string) {
			repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
			_, _ = cloneRepository(context.Background(), config, "github/org/repo-a", repoPath, token, nil, gitSyncLogEvent{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argvFile := filepath.Join(t.TempDir(), "argv")
			envFile := filepath.Join(t.TempDir(), "env")
			writeRecordingGit(t, argvFile, envFile)
			t.Setenv("GIT_CONFIG_COUNT", "")
			reposDir := t.TempDir()
			config := syncIsolationTestConfig(reposDir)
			config.GitAuthMethod = "token"
			tc.run(config, reposDir)

			raw, err := os.ReadFile(argvFile)
			if err != nil {
				t.Fatalf("fake git did not run: %v", err)
			}
			assertLowSpeedBefore(t, strings.Split(strings.TrimSpace(string(raw)), "\n"), tc.subcommand)
			assertAuthHeaderEnvUntouched(t, envFile, token)
		})
	}
}

// TestLocalGitCommandsCarryNoLowSpeed: a local command never gets the network
// options.
func TestLocalGitCommandsCarryNoLowSpeed(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv")
	writeRecordingGit(t, argvFile, filepath.Join(t.TempDir(), "env"))
	reposDir := t.TempDir()
	repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
	if _, err := gitRevParse(context.Background(), repoPath, "HEAD", syncIsolationTestConfig(reposDir), ""); err != nil {
		t.Fatalf("gitRevParse: %v", err)
	}
	raw, _ := os.ReadFile(argvFile)
	if strings.Contains(string(raw), "lowSpeed") {
		t.Fatalf("local rev-parse argv carries low-speed options: %q", raw)
	}
}

// TestPinnedRefFetchesCarryLowSpeed: gitFetchPinnedRef runs a branch fetch and
// a tag fetch; both must carry the low-speed options before the subcommand and
// leave the credential header alone.
func TestPinnedRefFetchesCarryLowSpeed(t *testing.T) {
	const token = "tkn"
	argvFile := filepath.Join(t.TempDir(), "argv")
	envFile := filepath.Join(t.TempDir(), "env")
	writeRecordingGit(t, argvFile, envFile)
	t.Setenv("GIT_CONFIG_COUNT", "")
	reposDir := t.TempDir()
	config := syncIsolationTestConfig(reposDir)
	config.GitAuthMethod = "token"
	repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
	if _, _, err := gitFetchPinnedRef(context.Background(), config, repoPath, "release", token, nil, gitSyncLogEvent{}); err != nil {
		t.Fatalf("gitFetchPinnedRef: %v", err)
	}
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("fake git did not run: %v", err)
	}
	calls := strings.Split(strings.TrimSuffix(strings.TrimSpace(string(raw)), "@@END@@"), "@@END@@")
	if len(calls) != 2 {
		t.Fatalf("fake git ran %d times, want 2 (branch fetch and tag fetch): %q", len(calls), raw)
	}
	for i, wantRefspec := range []string{"+refs/heads/release:refs/remotes/origin/release", "+refs/tags/release:refs/tags/release"} {
		argv := strings.Fields(strings.TrimSpace(calls[i]))
		assertLowSpeedBefore(t, argv, "fetch")
		if !slices.Contains(argv, wantRefspec) {
			t.Fatalf("fetch %d argv = %v, want refspec %q", i+1, argv, wantRefspec)
		}
	}
	assertAuthHeaderEnvUntouched(t, envFile, token)
}
