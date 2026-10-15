// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
)

// TestListGroupProjectsRateLimitRecovers proves a 429 with Retry-After
// waits once and then succeeds.
func TestListGroupProjectsRateLimitRecovers(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.script = []scriptedResponse{
		{status: http.StatusTooManyRequests, retryAfter: "1"},
		{status: http.StatusOK, body: "[]"},
	}
	h := newClientHarness(t, f.server)

	_, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if len(h.sleeps) != 1 || h.sleeps[0] != time.Second {
		t.Fatalf("sleeps = %v, want one 1s wait", h.sleeps)
	}
	if got := h.listingOutcomeCount(t, "retried"); got != 1 {
		t.Fatalf("retried count = %d, want 1", got)
	}
}

// TestListGroupProjectsRetryAfterClamp proves an HTTP-date Retry-After is
// parsed and a huge value is clamped to 60s.
func TestListGroupProjectsRetryAfterClamp(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"3600", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)} {
		f := newListingFixture(t)
		f.script = []scriptedResponse{
			{status: http.StatusServiceUnavailable, retryAfter: raw},
			{status: http.StatusOK, body: "[]"},
		}
		h := newClientHarness(t, f.server)

		if _, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000); err != nil {
			t.Fatalf("Retry-After %q: ListGroupProjects() error = %v", raw, err)
		}
		if len(h.sleeps) != 1 || h.sleeps[0] != 60*time.Second {
			t.Fatalf("Retry-After %q: sleeps = %v, want one clamped 60s wait", raw, h.sleeps)
		}
	}
}

// TestListGroupProjectsRateLimitExhausted proves four 429s give
// rate_limited with no records.
func TestListGroupProjectsRateLimitExhausted(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.script = []scriptedResponse{
		{status: http.StatusTooManyRequests},
		{status: http.StatusTooManyRequests},
		{status: http.StatusTooManyRequests},
		{status: http.StatusTooManyRequests},
	}
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err == nil {
		t.Fatal("ListGroupProjects() error = nil, want rate_limited")
	}
	if len(listing.Projects) != 0 {
		t.Fatalf("projects = %+v, want no partial result", listing.Projects)
	}
	var glErr *gitlabError
	if !errors.As(err, &glErr) || glErr.Class != "rate_limited" {
		t.Fatalf("error = %v, want failure_class rate_limited", err)
	}
	if len(f.requests) != 4 {
		t.Fatalf("requests = %d, want 1 initial plus 3 retries", len(f.requests))
	}
	if got := h.listingOutcomeCount(t, "rate_limited"); got != 1 {
		t.Fatalf("rate_limited count = %d, want 1", got)
	}
}

// TestListGroupProjectsContextCancelDuringWait proves cancelling the
// context during a backoff wait returns ctx.Err().
func TestListGroupProjectsContextCancelDuringWait(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.script = []scriptedResponse{
		{status: http.StatusTooManyRequests, retryAfter: "60"},
		{status: http.StatusOK, body: "[]"},
	}
	h := newClientHarness(t, f.server)
	ctx, cancel := context.WithCancel(context.Background())
	h.client.Sleep = func(d time.Duration) { cancel() }

	_, err := h.client.ListGroupProjects(ctx, "acme/platform", 4000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests = %d, want 1 (no second attempt after cancel)", len(f.requests))
	}
}

// TestListGroupProjectsStatusClasses proves 401, 403, 404, 500 and
// malformed JSON map to their failure classes with no partial result.
func TestListGroupProjectsStatusClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
		class  string
	}{
		{"auth", http.StatusUnauthorized, `{"message":"401 Unauthorized"}`, "auth_failed"},
		{"forbidden", http.StatusForbidden, `{"message":"403 Forbidden"}`, "forbidden"},
		{"not found", http.StatusNotFound, `{"message":"404 Not found"}`, "group_not_found"},
		{"upstream", http.StatusInternalServerError, `{"message":"boom"}`, "upstream_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newListingFixture(t)
			f.status = tc.status
			h := newClientHarness(t, f.server)

			listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
			if err == nil {
				t.Fatal("ListGroupProjects() error = nil, want a classified error")
			}
			if len(listing.Projects) != 0 {
				t.Fatalf("projects = %+v, want no partial result", listing.Projects)
			}
			var glErr *gitlabError
			if !errors.As(err, &glErr) || glErr.Class != tc.class {
				t.Fatalf("error = %v, want failure_class %q", err, tc.class)
			}
			if len(f.requests) != 1 {
				t.Fatalf("requests = %d, want 1 (no retry on %d)", len(f.requests), tc.status)
			}
		})
	}

	t.Run("decode", func(t *testing.T) {
		t.Parallel()
		f := newListingFixture(t)
		f.pages[1] = "not json"
		h := newClientHarness(t, f.server)

		if _, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000); err == nil {
			t.Fatal("ListGroupProjects() error = nil, want decode_error")
		} else {
			var glErr *gitlabError
			if !errors.As(err, &glErr) || glErr.Class != "decode_error" {
				t.Fatalf("error = %v, want failure_class decode_error", err)
			}
		}
	})
}

// gitlabError is the test alias for the leaf's classified error, letting
// errors.As match without importing internals. It is replaced by the real
// type once client.go lands.
type gitlabError = gitlab.Error
