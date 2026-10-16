// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"
)

func gitlabGroupEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func validGitLabGroupEnv() map[string]string {
	return map[string]string{
		"ESHU_REPO_SOURCE_MODE": "gitlabGroup",
		"ESHU_GITLAB_GROUP":     "acme/platform",
		"ESHU_GIT_AUTH_METHOD":  "token",
		"ESHU_GIT_TOKEN":        "glpat-test",
	}
}

// TestLoadRepoSyncConfigGitLabGroup proves the gitlabGroup mode loads the
// group with a token credential, and nothing else.
func TestLoadRepoSyncConfigGitLabGroup(t *testing.T) {
	t.Parallel()

	config, err := LoadRepoSyncConfig("test", gitlabGroupEnv(validGitLabGroupEnv()))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v", err)
	}
	if config.SourceMode != "gitlabGroup" || config.GitlabGroup != "acme/platform" {
		t.Fatalf("config = %+v, want gitlabGroup mode for acme/platform", config)
	}
	if config.GitToken != "glpat-test" {
		t.Fatalf("GitToken = %q, want the ESHU_GIT_TOKEN value", config.GitToken)
	}
}

func TestLoadRepoSyncConfigGitLabGroupRejections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"missing group", func(vars map[string]string) { delete(vars, "ESHU_GITLAB_GROUP") }, "ESHU_GITLAB_GROUP"},
		{"blank group", func(vars map[string]string) { vars["ESHU_GITLAB_GROUP"] = " " }, "ESHU_GITLAB_GROUP"},
		{"githubApp auth", func(vars map[string]string) { vars["ESHU_GIT_AUTH_METHOD"] = "githubApp" }, "ESHU_GIT_AUTH_METHOD"},
		{"ssh auth", func(vars map[string]string) { vars["ESHU_GIT_AUTH_METHOD"] = "ssh" }, "ESHU_GIT_AUTH_METHOD"},
		{"no auth", func(vars map[string]string) { vars["ESHU_GIT_AUTH_METHOD"] = "none" }, "ESHU_GIT_AUTH_METHOD"},
		{"missing token", func(vars map[string]string) { delete(vars, "ESHU_GIT_TOKEN") }, "ESHU_GIT_TOKEN"},
		{
			"github token fallback refused",
			func(vars map[string]string) {
				delete(vars, "ESHU_GIT_TOKEN")
				vars["GITHUB_TOKEN"] = "ghp-test"
			},
			"GITHUB_TOKEN",
		},
		{
			"path-form group with an outside exact rule",
			func(vars map[string]string) {
				vars["ESHU_REPOSITORY_RULES_JSON"] = `[{"exact": "gitlab/acme/other/x"}]`
			},
			"gitlab/acme/platform/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validGitLabGroupEnv()
			tc.mutate(vars)
			if _, err := LoadRepoSyncConfig("test", gitlabGroupEnv(vars)); err == nil {
				t.Fatalf("LoadRepoSyncConfig() error = nil, want a rejection naming %q", tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadRepoSyncConfig() error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestLoadRepoSyncConfigGitLabGroupNumericIDDefersRuleCheck proves a
// numeric group has no known prefix at config load, so an outside exact
// rule loads fine and the authoritative check runs at discovery.
func TestLoadRepoSyncConfigGitLabGroupNumericIDDefersRuleCheck(t *testing.T) {
	t.Parallel()

	vars := validGitLabGroupEnv()
	vars["ESHU_GITLAB_GROUP"] = "42"
	vars["ESHU_REPOSITORY_RULES_JSON"] = `[{"exact": "gitlab/acme/other/x"}]`
	config, err := LoadRepoSyncConfig("test", gitlabGroupEnv(vars))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil for a numeric group", err)
	}
	if config.GitlabGroup != "42" {
		t.Fatalf("GitlabGroup = %q, want 42", config.GitlabGroup)
	}
}
