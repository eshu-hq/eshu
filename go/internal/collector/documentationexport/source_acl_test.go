// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/exportmanifestpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestACLSummaryMapsSourceACLState confirms the bounded source_acl_state derived
// from the export manifest's evaluated ACL policy. An evaluated ACL is the only
// documentation producer that may assert allowed; a partial evaluation stays
// partial; and an unavailable evaluation observes no posture, so the field is
// omitted (absence means "no ACL claim", default-when-unknown deferred to
// security review).
func TestACLSummaryMapsSourceACLState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		policy string
		want   string
	}{
		{name: "evaluated asserts allowed", policy: exportmanifestpreflight.ACLPolicyEvaluated, want: facts.SourceACLStateAllowed},
		{name: "partial stays partial", policy: exportmanifestpreflight.ACLPolicyPartial, want: facts.SourceACLStatePartial},
		{name: "unavailable omits state", policy: exportmanifestpreflight.ACLPolicyUnavailable, want: ""},
		{name: "empty policy omits state", policy: "", want: ""},
		{name: "unknown policy omits state", policy: "something_else", want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := aclSummary(tc.policy).SourceACLState
			if got != tc.want {
				t.Fatalf("source_acl_state = %q, want %q", got, tc.want)
			}
		})
	}
}
