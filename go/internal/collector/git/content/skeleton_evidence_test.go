// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "testing"

// TestRedactSkeletonKeepsEvidenceShapes pins the evidence shapes the #8813
// record lets join the allowlist once the #8833 differential showed the raw
// Terraform evidence needs them: app_repo and app_name values that are plain
// names, the path of a github.com URL with userinfo, query, and fragment
// stripped, and a repo:org/name: OIDC subject. None can carry a credential;
// anything outside the exact shape still becomes REDACTED_value.
func TestRedactSkeletonKeepsEvidenceShapes(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"app_repo org/name", `app_repo = "org/service-a"`, `app_repo = "org/service-a"`},
		{"app_name name", `  APP_NAME = "service-a"`, `  APP_NAME = "service-a"`},
		{"app_repo json", `  "app_repo": "org/service-a",`, `  "app_repo": "org/service-a",`},
		{"app_repo with a space", `app_repo = "org/svc a"`, `app_repo = REDACTED_value`},
		{"app_repo url", `app_repo = "https://u:p@x.example.com"`, `app_repo = REDACTED_password_literal`},
		{"app_name three segments", `app_name = "a/b/c"`, `app_name = REDACTED_value`},
		{"other key with a name", `owner = "service-a"`, `owner = REDACTED_value`},
		{"github https", `repo_url = "https://github.com/org/repo.git"`, `repo_url = "https://github.com/org/repo.git"`},
		{"github ssh", `repo_url = "git@github.com:org/repo.git"`, `repo_url = "https://github.com/org/repo.git"`},
		{"github userinfo stripped", `repo_url = "https://user:t0k3n@github.com/org/repo"`, `repo_url = REDACTED_password_literal`},
		{"github query stripped", `repo_url = "https://github.com/org/repo?token=abc"`, `repo_url = "https://github.com/org/repo"`},
		{"github fragment stripped", `doc = "https://github.com/org/repo/blob/main/x.tf#L1"`, `doc = "https://github.com/org/repo"`},
		{"github module source", `src = "git::https://github.com/org/mod.git//sub?ref=v1.2.0"`, `src = "https://github.com/org/mod.git"`},
		{"github org only", `org_url = "https://github.com/org"`, `org_url = REDACTED_value`},
		// The raw evidence pattern also reads org/re here, so the skeleton keeps it.
		{"github odd path char", `repo_url = "https://github.com/org/re po"`, `repo_url = REDACTED_value # https://github.com/org/re`},
		{"github lookalike host", `repo_url = "https://github.com.evil.example/org/repo"`, `repo_url = REDACTED_value`},
		{"github text around", `note = "see https://github.com/org/repo"`, `note = REDACTED_value # https://github.com/org/repo`},
		{"github url with userinfo in text", `note = "clone https://u:p@ss@github.com/org/repo now"`, `note = REDACTED_password_literal`},
		{"github list element", `  "https://github.com/org/repo",`, `  "https://github.com/org/repo",`},
		{"github list element userinfo", `  "https://u:p@github.com/org/repo"`, `  REDACTED_password_literal`},
		{"oidc subject", `subject = "repo:org/name:ref:refs/heads/main"`, `subject = "repo:org/name:ref:refs/heads/main"`},
		{"oidc wildcard", `subject = "repo:org/name:*"`, `subject = "repo:org/name:*"`},
		{"oidc odd char", `subject = "repo:org/name:a b"`, `subject = REDACTED_value`},
		// A comment that is exactly a github.com URL keeps the normalized URL.
		{"comment github url", `# https://github.com/org/repo`, `# https://github.com/org/repo`},
		{"slash comment github url userinfo", `  // https://u:p@github.com/org/repo?x=1`, `  REDACTED_password_literal`},
		{"comment text and url", `# see https://github.com/org/repo for releases`, `# https://github.com/org/repo`},
		{"comment two urls", `# a: https://github.com/a/b and git@github.com:c/d.git`, `# https://github.com/a/b https://github.com/c/d.git`},
		{"comment text only", `# rotate the key every month`, `REDACTED_value`},
		// Only a real host boundary counts, and only owner/repo survives; the
		// owner follows GitHub's username rule (39 characters at most), so a
		// 40-character token cannot pass as one.
		{"github inside another url's query", `# see https://evil.example/?u=github.com/ghp0123456789abcdefghij0123456789abcdef/x`, `REDACTED_value`},
		{"lookalike host in text", `# notgithub.com/secretowner/secretrepo`, `REDACTED_value`},
		{"subdomain host in text", `# codeload.github.com/org/repo`, `REDACTED_value`},
		{"token-length owner", `repo_url = "https://github.com/ghp0123456789abcdefghij0123456789abcdefg/repo"`, `repo_url = REDACTED_value`},
		{"managed user owner", `repo_url = "https://github.com/jdoe_acme/repo"`, `repo_url = "https://github.com/jdoe_acme/repo"`},
		{"classic token as owner", `repo_url = "https://github.com/ghp` + `_0123456789abcdefghij0123456789abcdef/repo"`, `repo_url = REDACTED_value`},
		{"deep path trimmed", `# https://github.com/org/repo/blob/main/secrets/key.txt`, `# https://github.com/org/repo`},
		// A trailing comment that is exactly a github.com URL after a kept value.
		{"version with github comment", `chart_version = "6.7.3"  # https://github.com/org/charts/tree/main/x`, `chart_version = "6.7.3" # https://github.com/org/charts`},
		{"number with github comment", `replicas = 3 # https://github.com/org/repo`, `replicas = 3 # https://github.com/org/repo`},
		{"redacted value with github comment", `region = "us-east-1" # https://github.com/org/repo`, `region = REDACTED_value # https://github.com/org/repo`},
		{"version with text comment", `chart_version = "6.7.3" # pinned`, `chart_version = "6.7.3"`},
		{"github value with text comment", `repository_url = "https://github.com/org/repo" # paste yours here`, `repository_url = "https://github.com/org/repo"`},
		{"empty value with url in comment", `runner_repo = ""  # e.g. part of https://github.com/org/repo`, `runner_repo = REDACTED_value # https://github.com/org/repo`},
		// app_repo and app_name may hold a plain URL: host and path, no userinfo,
		// query, or fragment.
		{"app_repo registry url", `app_repo = "https://123.dkr.ecr.us-east-1.amazonaws.com/v1/org/svc"`, `app_repo = "https://123.dkr.ecr.us-east-1.amazonaws.com/v1/org/svc"`},
		{"app_repo url with userinfo", `app_repo = "https://u:p@registry.example.com/svc"`, `app_repo = REDACTED_password_literal`},
		{"app_repo url with query", `app_repo = "https://registry.example.com/svc?token=x"`, `app_repo = REDACTED_value`},
		{"other key with a plain url", `endpoint = "https://registry.example.com/svc"`, `endpoint = REDACTED_value`},
		// The finding check still comes first.
		{"flagged key with a github url", `github_token = "https://github.com/org/repo"`, `github_token = REDACTED_api_token`},
	}
	for _, tc := range cases {
		got := skeletonForTest(t, tc.in)
		if got != tc.want {
			t.Errorf("%s:\n in   %q\n got  %q\n want %q", tc.name, tc.in, got, tc.want)
		}
		if again := skeletonForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point: %q -> %q", tc.name, got, again)
		}
	}
}
