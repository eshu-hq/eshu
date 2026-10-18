// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/resolve"
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

// resolveCacheForTest builds an enabled cache whose resolver answers github.com
// with a fixed TEST-NET answer set, for the curloptResolve argv contract.
func resolveCacheForTest(t *testing.T) *resolve.Cache {
	t.Helper()
	cache := resolve.New(
		resolve.Settings{Enabled: true, LookupTimeout: time.Second, MinGap: 30 * time.Second, RefreshInterval: time.Hour},
		resolve.Options{
			Lookup: func(_ context.Context, _ string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("192.0.2.4"), net.ParseIP("2001:db8::4")}, nil
			},
			Getenv: func(string) string { return "" },
		},
	)
	t.Cleanup(cache.Close)
	return cache
}

const wantCurlResolveArg = "http.curloptResolve=github.com:443:192.0.2.4,2001:db8::4"

// assertCurlResolveBefore fails unless argv holds -c <curloptResolve> ahead of
// the named subcommand (#8092).
func assertCurlResolveBefore(t *testing.T, argv []string, subcommand string) {
	t.Helper()
	at := slices.Index(argv, subcommand)
	pin := slices.Index(argv, wantCurlResolveArg)
	if at < 0 || pin < 1 || pin > at || argv[pin-1] != "-c" {
		t.Fatalf("argv = %v, want [-c %s] before %q", argv, wantCurlResolveArg, subcommand)
	}
}

// TestNetworkCommandsCarryCurlResolvePin: clone, fetch (branch and pinned),
// and ls-remote all pin the provider host through the resolve cache, a local
// command carries no pin, and an SSH remote is unaffected.
func TestNetworkCommandsCarryCurlResolvePin(t *testing.T) {
	readCalls := func(t *testing.T, argvFile string) [][]string {
		t.Helper()
		raw, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatalf("fake git did not run: %v", err)
		}
		var calls [][]string
		for _, call := range strings.Split(strings.TrimSpace(string(raw)), "@@END@@") {
			if fields := strings.Fields(call); len(fields) > 0 {
				calls = append(calls, fields)
			}
		}
		return calls
	}

	t.Run("fetch clone and ls-remote pin the host", func(t *testing.T) {
		argvFile := filepath.Join(t.TempDir(), "argv")
		writeRecordingGit(t, argvFile, filepath.Join(t.TempDir(), "env"))
		reposDir := t.TempDir()
		config := syncIsolationTestConfig(reposDir)
		config.ResolveCache = resolveCacheForTest(t)
		repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")

		_ = gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
		_, _ = cloneRepository(context.Background(), config, "github/org/repo-b", filepath.Join(reposDir, "github", "org", "repo-b"), "", nil, gitSyncLogEvent{})
		_, _ = remoteDefaultBranch(context.Background(), config, repoPath, "")
		_, _ = gitRevParse(context.Background(), repoPath, "HEAD", config, "")

		calls := readCalls(t, argvFile)
		if len(calls) != 4 {
			t.Fatalf("fake git ran %d times, want 4: %v", len(calls), calls)
		}
		assertCurlResolveBefore(t, calls[0], "fetch")
		assertCurlResolveBefore(t, calls[1], "clone")
		assertCurlResolveBefore(t, calls[2], "ls-remote")
		if slices.Contains(calls[3], wantCurlResolveArg) {
			t.Fatalf("local rev-parse argv = %v, want no pin", calls[3])
		}
	})

	t.Run("pinned ref fetches pin the host", func(t *testing.T) {
		argvFile := filepath.Join(t.TempDir(), "argv")
		writeRecordingGit(t, argvFile, filepath.Join(t.TempDir(), "env"))
		reposDir := t.TempDir()
		config := syncIsolationTestConfig(reposDir)
		config.ResolveCache = resolveCacheForTest(t)
		repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
		if _, _, err := gitFetchPinnedRef(context.Background(), config, repoPath, "release", "", nil, gitSyncLogEvent{}); err != nil {
			t.Fatalf("gitFetchPinnedRef: %v", err)
		}
		calls := readCalls(t, argvFile)
		if len(calls) != 2 {
			t.Fatalf("fake git ran %d times, want 2", len(calls))
		}
		for _, argv := range calls {
			assertCurlResolveBefore(t, argv, "fetch")
		}
	})

	t.Run("ssh remotes and an absent cache never pin", func(t *testing.T) {
		argvFile := filepath.Join(t.TempDir(), "argv")
		writeRecordingGit(t, argvFile, filepath.Join(t.TempDir(), "env"))
		reposDir := t.TempDir()
		repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")

		sshConfig := syncIsolationTestConfig(reposDir)
		sshConfig.GitAuthMethod = "ssh"
		sshConfig.ResolveCache = resolveCacheForTest(t)
		_ = gitFetchBranch(context.Background(), sshConfig, repoPath, "main", "", nil, gitSyncLogEvent{}, "")

		bareConfig := syncIsolationTestConfig(reposDir)
		_ = gitFetchBranch(context.Background(), bareConfig, repoPath, "main", "", nil, gitSyncLogEvent{}, "")

		for _, argv := range readCalls(t, argvFile) {
			if slices.Contains(argv, wantCurlResolveArg) {
				t.Fatalf("argv = %v, want no pin for ssh/no-cache", argv)
			}
		}
	})
}

// TestFetchRetryPinsRefreshedAddresses is the composed #8092 contract: a
// fetch whose first attempt fails with a resolver error asks the cache for
// a fresh lookup through the deadline hook, and the retry's rebuilt argv
// pins the refreshed set — not the set the first attempt used. The fake
// resolver answers 192.0.2.1 first and 192.0.2.2 on the refresh; a minimum
// gap of a millisecond stands in for an aged cache entry.
func TestFetchRetryPinsRefreshedAddresses(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := makeManagedRepo(t, reposDir, "github/org/repo-a")
	argvFile := filepath.Join(t.TempDir(), "argv")
	binDir, stateDir := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argvFile + `"
printf '@@END@@\n' >> "` + argvFile + `"
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*) printf "refs/remotes/origin/main\n" ;;
	*"fetch --progress"*)
		n=$(cat "` + stateDir + `/n" 2>/dev/null || echo 0)
		n=$((n+1))
		echo "$n" > "` + stateDir + `/n"
		if [ "$n" = 1 ]; then
			echo "fatal: unable to access 'https://github.com/org/repo-a.git/': Could not resolve host: github.com" >&2
			exit 128
		fi
		;;
	*"rev-parse refs/remotes/origin/main"*) printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n" ;;
	*"ls-remote"*)
		printf "ref: refs/heads/main\tHEAD\n"
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main\n"
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var lookups atomic.Int64
	cache := resolve.New(
		resolve.Settings{Enabled: true, LookupTimeout: 2 * time.Second, MinGap: time.Millisecond, RefreshInterval: time.Hour},
		resolve.Options{
			Lookup: func(_ context.Context, _ string) ([]net.IP, error) {
				if lookups.Add(1) == 1 {
					return []net.IP{net.ParseIP("192.0.2.1")}, nil
				}
				return []net.IP{net.ParseIP("192.0.2.2")}, nil
			},
			Getenv: func(string) string { return "" },
		},
	)
	t.Cleanup(cache.Close)

	instruments, _ := newSyncIsolationInstruments(t)
	logger := slog.New(slog.DiscardHandler)
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = retryTestLimits(0)
	config.ResolveCache = cache

	synced, err := syncGitRepositoriesWithLogger(context.Background(), config,
		[]string{"github/org/repo-a"}, logger, gitDeltaBaseline{Instruments: instruments})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != repoPath {
		t.Fatalf("SelectedRepoPaths = %v, want only %q (the fetch recovered on its retry)", synced.SelectedRepoPaths, repoPath)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("resolver lookups = %d, want 2 (initial plus one failure refresh)", got)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("fake git did not run: %v", err)
	}
	var fetches [][]string
	for _, call := range strings.Split(strings.TrimSpace(string(raw)), "@@END@@") {
		if fields := strings.Fields(call); slices.Contains(fields, "fetch") {
			fetches = append(fetches, fields)
		}
	}
	if len(fetches) != 2 {
		t.Fatalf("recorded %d fetch invocations, want 2 attempts: %v", len(fetches), fetches)
	}
	first := "http.curloptResolve=github.com:443:192.0.2.1"
	second := "http.curloptResolve=github.com:443:192.0.2.2"
	if !slices.Contains(fetches[0], first) {
		t.Fatalf("attempt 1 argv = %v, want pin %q", fetches[0], first)
	}
	if !slices.Contains(fetches[1], second) || slices.Contains(fetches[1], first) {
		t.Fatalf("attempt 2 argv = %v, want refreshed pin %q and not %q", fetches[1], second, first)
	}
}
