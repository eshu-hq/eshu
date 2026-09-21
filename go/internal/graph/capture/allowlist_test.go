// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// TestParseAllowlistRejectsMissingReason is the seeded RED case for the
// divergence allowlist: an entry without a reason must fail validation,
// otherwise a divergence could be silenced with no explanation.
func TestParseAllowlistRejectsMissingReason(t *testing.T) {
	raw := []byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  upstream: https://github.com/orneryd/NornicDB/issues/400
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want missing-reason rejection")
	}
}

// TestParseAllowlistRejectsMissingUpstream is the seeded RED case for
// untracked divergences: an entry without an upstream issue link must fail.
func TestParseAllowlistRejectsMissingUpstream(t *testing.T) {
	raw := []byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: NornicDB returns an extra row here
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want missing-upstream rejection")
	}
}

// TestParseAllowlistRejectsNonIssueUpstream pins the upstream contract: the
// link must point at a real issue, not a wiki page or a bare repo, so the
// divergence stays tracked against a fixable item.
func TestParseAllowlistRejectsNonIssueUpstream(t *testing.T) {
	raw := []byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: NornicDB returns an extra row here
  upstream: https://github.com/orneryd/NornicDB/wiki/known-limits
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want non-issue-upstream rejection")
	}
}

// TestAllowlistStaleEntryFails keeps the allowlist honest in the other
// direction: an entry that matches no divergence in the run fails, so fixed
// bugs cannot linger as permanent exemptions.
func TestAllowlistStaleEntryFails(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: historical divergence, since fixed
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Other) RETURN n"}, Detail: "row digest differs"},
	}
	if err := allow.Validate(diffs); err == nil {
		t.Fatal("Validate() error = nil, want stale-entry rejection")
	}
}

// TestAllowlistMatchedEntryPasses is the GREEN case: a divergence the
// allowlist names, with a reason and an upstream issue, is excused and the
// entry is consumed.
func TestAllowlistMatchedEntryPasses(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: NornicDB returns an extra row here
  upstream: https://github.com/orneryd/NornicDB/issues/400
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n"}, Detail: "row digest differs"},
	}
	remaining, err := allow.Excuse(diffs)
	if err != nil {
		t.Fatalf("Excuse() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("Excuse() remaining = %d, want 0", len(remaining))
	}
}

// TestEmptyAllowlistExcusesNothing pins the default: an empty allowlist file
// parses clean and excuses no divergence.
func TestEmptyAllowlistExcusesNothing(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`entries: []`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n"}, Detail: "row digest differs"},
	}
	remaining, err := allow.Excuse(diffs)
	if err != nil {
		t.Fatalf("Excuse() error = %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("Excuse() remaining = %d, want 1", len(remaining))
	}
}
