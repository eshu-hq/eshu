// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
)

// TestResolveGroup proves the group lookup resolves a path-form group to
// its ID and canonical full path.
func TestResolveGroup(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	h := newClientHarness(t, f.server)

	group, err := h.client.ResolveGroup(context.Background(), "Acme/Platform")
	if err != nil {
		t.Fatalf("ResolveGroup() error = %v", err)
	}
	if group.ID != 42 || group.FullPath != "acme/platform" {
		t.Fatalf("group = %+v, want ID 42 and full_path acme/platform", group)
	}
	if f.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", f.requestCount())
	}
	req := f.firstRequest()
	if !strings.Contains(req.escapedPath, "Acme%2FPlatform") {
		t.Fatalf("escaped path = %q, want the configured form escaped", req.escapedPath)
	}
	if strings.Contains(req.query, "page=") || !strings.Contains(req.query, "with_projects=false") {
		t.Fatalf("query = %q, want with_projects=false and no page", req.query)
	}
	if req.token != "glpat-test" {
		t.Fatalf("PRIVATE-TOKEN = %q, want the configured token", req.token)
	}
}

// TestResolveGroupNumericID proves a numeric group resolves without a
// known prefix, and a 404 maps to group_not_found.
func TestResolveGroupNumericID(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	h := newClientHarness(t, f.server)

	group, err := h.client.ResolveGroup(context.Background(), "42")
	if err != nil {
		t.Fatalf("ResolveGroup() error = %v", err)
	}
	if group.ID != 42 || group.FullPath != "acme/platform" {
		t.Fatalf("group = %+v, want the resolved path", group)
	}
}

// TestResolveGroupNotFound proves a 404 on the lookup is group_not_found.
func TestResolveGroupNotFound(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.script = []scriptedResponse{{status: http.StatusNotFound, body: `{"message":"404 Group Not Found"}`}}
	h := newClientHarness(t, f.server)

	if _, err := h.client.ResolveGroup(context.Background(), "acme/missing"); err == nil {
		t.Fatal("ResolveGroup() error = nil, want group_not_found")
	} else {
		var glErr *gitlabError
		if !errors.As(err, &glErr) || glErr.Class != "group_not_found" {
			t.Fatalf("error = %v, want failure_class group_not_found", err)
		}
	}
}

// TestValidateConfig proves the config-load contract: group, token auth,
// and ESHU_GIT_TOKEN, with the path-form prefix check now and the numeric
// form deferred to discovery.
func TestValidateConfig(t *testing.T) {
	t.Parallel()

	if err := gitlab.ValidateConfig("token", "acme/platform", "glpat", "", []string{"gitlab/acme/platform/a"}); err != nil {
		t.Fatalf("ValidateConfig() error = %v, want nil", err)
	}
	for _, tc := range []struct {
		name                                 string
		method, group, gitToken, githubToken string
		rules                                []string
		want                                 string
	}{
		{"missing group", "token", "", "glpat", "", nil, "ESHU_GITLAB_GROUP"},
		{"githubApp", "githubApp", "acme/platform", "glpat", "", nil, "ESHU_GIT_AUTH_METHOD"},
		{"ssh", "ssh", "acme/platform", "glpat", "", nil, "ESHU_GIT_AUTH_METHOD"},
		{"missing token", "token", "acme/platform", "", "", nil, "ESHU_GIT_TOKEN"},
		{"github token fallback", "token", "acme/platform", "", "ghp", nil, "GITHUB_TOKEN"},
		{"outside rule", "token", "acme/platform", "glpat", "", []string{"gitlab/acme/other/x"}, "gitlab/acme/platform/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := gitlab.ValidateConfig(tc.method, tc.group, tc.gitToken, tc.githubToken, tc.rules)
			if err == nil {
				t.Fatal("ValidateConfig() error = nil, want non-nil")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateConfig() error = %v, want it to name %q", err, tc.want)
			}
		})
	}
	if err := gitlab.ValidateConfig("token", "42", "", "", []string{"gitlab/acme/other/x"}); err == nil {
		t.Fatal("ValidateConfig() error = nil for a numeric group without a token, want non-nil")
	} else if !strings.Contains(err.Error(), "ESHU_GIT_TOKEN") {
		t.Fatalf("ValidateConfig() error = %v, want the token rejection before any rule check", err)
	}
	if err := gitlab.ValidateConfig("token", "42", "glpat", "", []string{"gitlab/acme/other/x"}); err != nil {
		t.Fatalf("ValidateConfig() error = %v, want nil: numeric groups defer the rule check", err)
	}
}

// TestCheckExactRules proves exact rules must live under the resolved group
// prefix, compared case-sensitively, so a rule that could never match fails
// the cycle instead of silently excluding everything.
func TestCheckExactRules(t *testing.T) {
	t.Parallel()

	if err := gitlab.CheckExactRules("acme/platform", []string{"gitlab/acme/platform/a", "gitlab/acme/platform/sub/b"}); err != nil {
		t.Fatalf("CheckExactRules() error = %v, want nil", err)
	}
	for _, tc := range []struct {
		name string
		rule string
	}{
		{"outside the group", "gitlab/acme/other/x"},
		{"lookalike prefix", "gitlab/acme/platformer/y"},
		{"wrong case", "gitlab/Acme/Platform/a"},
		{"missing provider", "acme/platform/a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := gitlab.CheckExactRules("acme/platform", []string{"gitlab/acme/platform/a", tc.rule})
			if err == nil {
				t.Fatalf("CheckExactRules(%q) error = nil, want config_invalid", tc.rule)
			}
			var glErr *gitlabError
			if !errors.As(err, &glErr) || glErr.Class != "config_invalid" {
				t.Fatalf("error = %v, want failure_class config_invalid", err)
			}
		})
	}
}
