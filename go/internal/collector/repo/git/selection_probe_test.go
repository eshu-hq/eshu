// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Classification fixtures for the upstream probe (#8454). Each seeds a scope
// the listing no longer names against a stub GitHub API whose answer
// violates the naive "missing means gone" reading, then proves the probe
// classifies it exactly.
package git

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

// stubUpstreamGitHub serves one fixed answer per probe path. Every path
// below mirrors a QA-observed fate: transferred to the archive org,
// renamed in place, renamed away, present-but-unlisted, gone, flaky, and a
// row with no stored id taking the /repos fallback.
func stubUpstreamGitHub(t *testing.T, token string) *httptest.Server {
	t.Helper()
	answers := map[string]struct {
		status   int
		fullName string
	}{
		"/repositories/101":    {http.StatusOK, "archive/old"},
		"/repositories/202":    {http.StatusOK, "acme/renamed-service-v2"},
		"/repositories/203":    {http.StatusOK, "acme/gone-elsewhere"},
		"/repositories/204":    {http.StatusOK, "acme/ghost"},
		"/repositories/205":    {http.StatusNotFound, ""},
		"/repositories/206":    {http.StatusInternalServerError, ""},
		"/repos/acme/slugless": {http.StatusOK, "archive/slugless"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		answer, ok := answers[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		if answer.fullName != "" {
			fmt.Fprintf(w, `{"id": 1, "full_name": %q}`, answer.fullName)
		}
	}))
}

func probeTargetFor(slug string, id int64) membership.ProbeTarget {
	return membership.ProbeTarget{
		ScopeID: "git-repository-scope:" + slug, Slug: slug,
		GitHubRepoID: id, SelectorOwner: "acme",
	}
}

func TestUpstreamProbeClassifiesStubAnswers(t *testing.T) {
	t.Parallel()

	const token = "listing-credential"
	server := stubUpstreamGitHub(t, token)
	defer server.Close()
	probe := githubUpstreamProbe(nil, server.URL, token)
	ctx := context.Background()

	for _, tc := range []struct {
		name           string
		target         membership.ProbeTarget
		prior          membership.UpstreamClassification
		priorSuccessor string
		want           membership.UpstreamClassification
		wantSuccessor  string
		wantUpdate     bool
	}{
		{name: "transferred", target: probeTargetFor("acme/old", 101), want: membership.UpstreamTransferred, wantSuccessor: "archive/old", wantUpdate: true},
		{name: "renamed with selected successor", target: probeTargetFor("acme/renamed-service", 202), want: membership.UpstreamRenamed, wantSuccessor: "acme/renamed-service-v2", wantUpdate: true},
		{name: "renamed out", target: probeTargetFor("acme/gone", 203), want: membership.UpstreamRenamed, wantSuccessor: "acme/gone-elsewhere", wantUpdate: true},
		{name: "still present disagreement", target: probeTargetFor("acme/ghost", 204), want: membership.UpstreamStillPresent, wantUpdate: true},
		{name: "not found", target: probeTargetFor("acme/vanished", 205), want: membership.UpstreamNotFoundOrInaccessible, wantUpdate: true},
		{name: "server error keeps prior", target: probeTargetFor("acme/flaky", 206), prior: membership.UpstreamTransferred, priorSuccessor: "archive/flaky", want: membership.UpstreamTransferred, wantSuccessor: "archive/flaky"},
		{name: "fallback when id is zero", target: probeTargetFor("acme/slugless", 0), want: membership.UpstreamTransferred, wantSuccessor: "archive/slugless", wantUpdate: true},
	} {
		// Not parallel: the shared stub server closes when this parent
		// returns.
		t.Run(tc.name, func(t *testing.T) {
			tc.target.PriorClassification = tc.prior
			tc.target.PriorSuccessor = tc.priorSuccessor
			answer, err := probe(ctx, tc.target)
			if err != nil && tc.wantUpdate {
				t.Fatalf("probe(%s) error = %v, want a decisive answer", tc.name, err)
			}
			if err == nil {
				// A decisive stub answer must not also carry an error: the
				// observer counts errors as kept_prior.
				got, successor, update := membership.Classify(tc.target, answer)
				if got != tc.want || successor != tc.wantSuccessor || update != tc.wantUpdate {
					t.Fatalf("Classify() = (%q, %q, %v), want (%q, %q, %v)",
						got, successor, update, tc.want, tc.wantSuccessor, tc.wantUpdate)
				}
				return
			}
			got, successor, update := membership.Classify(tc.target, membership.ProbeResult{})
			if got != tc.want || successor != tc.wantSuccessor || update != tc.wantUpdate {
				t.Fatalf("Classify(keep-prior) = (%q, %q, %v), want (%q, %q, %v)",
					got, successor, update, tc.want, tc.wantSuccessor, tc.wantUpdate)
			}
		})
	}
}

func TestUpstreamProbeWithoutCredentialIsInconclusive(t *testing.T) {
	t.Parallel()

	const token = "listing-credential"
	server := stubUpstreamGitHub(t, token)
	defer server.Close()
	// Wrong credential: the stub answers 401, which is inconclusive, never a
	// verdict. A probe without the listing credential proves nothing.
	probe := githubUpstreamProbe(nil, server.URL, "wrong")
	answer, err := probe(context.Background(), probeTargetFor("acme/old", 101))
	if err == nil {
		t.Fatalf("probe with wrong credential = %+v, nil error; want an inconclusive error", answer)
	}
	if answer != (membership.ProbeResult{}) {
		t.Fatalf("probe with wrong credential = %+v, want the zero result", answer)
	}
}

func TestUpstreamProbeForConfigNeedsToken(t *testing.T) {
	t.Parallel()

	if upstreamProbeForConfig(RepoSyncConfig{}, "") != nil {
		t.Fatal("upstreamProbeForConfig(blank token) != nil: no credential means no probe")
	}
	if upstreamProbeForConfig(RepoSyncConfig{}, "token") == nil {
		t.Fatal("upstreamProbeForConfig(token) == nil: a credentialed githubOrg request probes")
	}
}

func TestProbeAuthorizationHeaderCarriesListingCredential(t *testing.T) {
	t.Parallel()

	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": 9, "full_name": "acme/seen"}`)
	}))
	defer server.Close()
	probe := githubUpstreamProbe(nil, server.URL, "secret-token")
	if _, err := probe(context.Background(), probeTargetFor("acme/seen", 9)); err != nil {
		t.Fatalf("probe() error = %v", err)
	}
	if got != "Bearer secret-token" {
		t.Fatalf("Authorization = %q, want the listing credential", strings.TrimSpace(got))
	}
}
