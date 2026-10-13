// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRepoSyncConfigGitDeadlineDefaults(t *testing.T) {
	config, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	if config.GitDeadlines.Resolve != 60*time.Second {
		t.Fatalf("Resolve = %s, want 60s", config.GitDeadlines.Resolve)
	}
	if config.GitDeadlines.Fetch != 90*time.Second {
		t.Fatalf("FetchTimeout = %s, want 90s", config.GitDeadlines.Fetch)
	}
	if config.GitDeadlines.Clone != 600*time.Second {
		t.Fatalf("CloneTimeout = %s, want 600s", config.GitDeadlines.Clone)
	}
}

func TestLoadRepoSyncConfigGitDeadlineEnvOverride(t *testing.T) {
	config, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
		"ESHU_GIT_RESOLVE_TIMEOUT_SECONDS": "30",
		"ESHU_GIT_FETCH_TIMEOUT_SECONDS":   " 45 ",
		"ESHU_GIT_CLONE_TIMEOUT_SECONDS":   "1200",
	}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	if config.GitDeadlines.Resolve != 30*time.Second {
		t.Fatalf("Resolve = %s, want 30s", config.GitDeadlines.Resolve)
	}
	if config.GitDeadlines.Fetch != 45*time.Second {
		t.Fatalf("FetchTimeout = %s, want 45s", config.GitDeadlines.Fetch)
	}
	if config.GitDeadlines.Clone != 1200*time.Second {
		t.Fatalf("CloneTimeout = %s, want 1200s", config.GitDeadlines.Clone)
	}
}

// TestLoadRepoSyncConfigRejectsBadGitDeadline pins the loud-failure contract:
// a typo must stop startup naming the variable and the value, never fall back
// to a default and never disable the deadline (a zero deadline would bring
// back the 300 s fleet pause of #8052).
func TestLoadRepoSyncConfigRejectsBadGitDeadline(t *testing.T) {
	for _, name := range []string{"ESHU_GIT_RESOLVE_TIMEOUT_SECONDS", "ESHU_GIT_FETCH_TIMEOUT_SECONDS", "ESHU_GIT_CLONE_TIMEOUT_SECONDS"} {
		for _, value := range []string{"-1", "0", "abc", "1.5", "90s"} {
			t.Run(name+"="+value, func(t *testing.T) {
				_, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{name: value}))
				if err == nil {
					t.Fatalf("LoadRepoSyncConfig() error = nil, want an error for %s=%q", name, value)
				}
				if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), value) {
					t.Fatalf("error = %q, want it to name %s and %q", err, name, value)
				}
			})
		}
	}
}
