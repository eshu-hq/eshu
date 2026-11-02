// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenGitHubLoginAdmission pins the exact method set behind the
// versioned #8872 admission contract for GitHub login. A renamed, added,
// removed, or re-signed method fails here until that list is deliberately
// updated; bump GitHubLoginAdmissionContractVersion alongside so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently. The optional GitHubProviderLister is
// deliberately not part of v0 (see the GitHubLoginAdmission doc comment).
var frozenGitHubLoginAdmission = []string{
	"CompleteGitHubLogin func(context.Context, query.GitHubLoginCompleteRequest) (query.GitHubLoginCompleteResponse, error)",
	"StartGitHubLogin func(context.Context, query.GitHubLoginStartRequest) (query.GitHubLoginStartResponse, error)",
}

// gitHubLoginAdmissionMethodSet reflects the live GitHubLoginAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func gitHubLoginAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*GitHubLoginAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestGitHubLoginAdmissionContractIsVersioned pins the #8872
// admission-contract version for GitHub login. The version value plus the
// frozen method set below are the enforcement: a breaking change to the
// admission shape fails this test until the frozen list is deliberately
// updated and the version bumps in the same diff.
func TestGitHubLoginAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if GitHubLoginAdmissionContractVersion != "v0" {
		t.Errorf("GitHubLoginAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", GitHubLoginAdmissionContractVersion, "v0")
	}
	got := gitHubLoginAdmissionMethodSet(t)
	if len(got) != len(frozenGitHubLoginAdmission) {
		t.Fatalf("GitHubLoginAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenGitHubLoginAdmission, len(frozenGitHubLoginAdmission), GitHubLoginAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenGitHubLoginAdmission[i] {
			t.Errorf("GitHubLoginAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenGitHubLoginAdmission[i], GitHubLoginAdmissionContractVersion)
		}
	}
}
