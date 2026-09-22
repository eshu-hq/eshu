// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"strings"
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

// TestParseAllowlistRejectsEmptyTier pins fail-closed scoping: an entry must
// name the divergence kind it excuses, so a scheduling-noise excuse can never
// silently cover a result disagreement.
func TestParseAllowlistRejectsEmptyTier(t *testing.T) {
	raw := []byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  reason: NornicDB polls this more often
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want empty-tier rejection")
	}
}

// TestParseAllowlistRejectsUnknownTier pins the closed kind vocabulary: a
// typo'd tier must fail the parse rather than excuse nothing silently.
func TestParseAllowlistRejectsUnknownTier(t *testing.T) {
	raw := []byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: polls
  reason: NornicDB polls this more often
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want unknown-tier rejection")
	}
}

// TestAllowlistTierScopesExcuseToKind is the slice-3 seeded pair for kind
// scoping: a failures-tier entry excuses the failed-execution divergence but
// leaves the result disagreement on the same statement unexcused, while a
// statement-tier entry excuses both.
func TestAllowlistTierScopesExcuseToKind(t *testing.T) {
	stmt := "MATCH (n:Repository) RETURN n"
	diffs := []backendconformance.DifferentialDifference{
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: stmt}, Kind: "failures", Detail: "failed executions differ"},
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: stmt}, Kind: "results", Detail: "row digest differs"},
	}
	failures, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: failures
  reason: one backend rejects the statement under a transient lock error and the retry agrees
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	remaining, err := failures.Excuse(diffs)
	if err != nil {
		t.Fatalf("Excuse() error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].Kind != "results" {
		t.Fatalf("Excuse() remaining = %v, want only the results-kind divergence", remaining)
	}
	wildcard, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: whole-statement dialect divergence, tracked upstream
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	remaining, err = wildcard.Excuse(diffs)
	if err != nil {
		t.Fatalf("Excuse() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("Excuse() remaining = %v, want none for a statement-tier entry", remaining)
	}
}

// TestAllowlistRejectsExecutionsTier pins the #6782 permanent disposition:
// execution-count noise is advisory at the gate, so it needs no excuse and
// an executions-tier entry is a parse error rather than a silent no-op that
// would accumulate as dead weight in the spec.
func TestAllowlistRejectsExecutionsTier(t *testing.T) {
	_, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: executions
  reason: poll iteration counts vary run to run with agreeing results
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err == nil {
		t.Fatal("ParseAllowlist() error = nil, want executions-tier rejection")
	}
	if !strings.Contains(err.Error(), "executions") {
		t.Fatalf("ParseAllowlist() error = %v, want it to name the retired executions tier", err)
	}
}

// TestAllowlistRejectsRowcountTier pins the #6782 option-2 disposition:
// row-total noise with agreeing results is advisory at the gate, so it
// needs no excuse and a rowcount-tier entry is a parse error rather than
// a silent no-op that would accumulate as dead weight in the spec.
func TestAllowlistRejectsRowcountTier(t *testing.T) {
	_, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: rowcount
  reason: row totals vary run to run with agreeing results
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err == nil {
		t.Fatal("ParseAllowlist() error = nil, want rowcount-tier rejection")
	}
	if !strings.Contains(err.Error(), "rowcount") {
		t.Fatalf("ParseAllowlist() error = %v, want it to name the retired rowcount tier", err)
	}
}

// TestAllowlistStatementTierMatchesAdvisoryKind keeps a statement-tier entry
// honest under the advisory disposition: an executions-only divergence still
// counts as a match, so the entry is neither stale nor silently widened.
func TestAllowlistStatementTierMatchesAdvisoryKind(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`entries:
- statement: "MATCH (n:Repository) RETURN n"
  tier: statement
  reason: whole-statement dialect divergence, tracked upstream
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Repository) RETURN n"}, Kind: "executions", Detail: "executions differ"},
	}
	remaining, err := allow.Excuse(diffs)
	if err != nil {
		t.Fatalf("Excuse() error = %v, want the statement-tier entry to match the executions divergence", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("Excuse() remaining = %v, want none", remaining)
	}
}

// TestEmptyAllowlistExcusesNothing pins the default: an empty allowlist file
// parses clean and excuses no divergence.
// TestParseTransientReadsRejectsNonTransientStatement is the seeded RED
// case for the option-1 guard (#6782): a transient_reads entry whose
// statement is not a transient-state read must fail the parse, otherwise
// any divergence could be excluded without proving it reads transient
// state. RED: transient_reads is not parsed yet, so this parses clean.
func TestParseTransientReadsRejectsNonTransientStatement(t *testing.T) {
	raw := []byte(`transient_reads:
- statement: "MATCH (n:Repository) RETURN n"
  reason: not actually transient
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want non-transient-statement rejection")
	}
}

// TestParseTransientReadsRejectsTier pins that transient exclusions take
// no tier: the exclusion is kind-agnostic within the observed-noise family
// by design, so a tier key would be a silent no-op inviting confusion
// with the allowlist tiers.
func TestParseTransientReadsRejectsTier(t *testing.T) {
	raw := []byte(`transient_reads:
- statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
  tier: results
  reason: orphan scan over uid IS NULL
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want transient-tier rejection")
	}
}

// TestExcludeTransientHoldsOrphanDigestDiffer pins the option-1
// disposition: a results-kind divergence on a registered transient read
// (the Module orphan page whose digest disagrees across legs) is
// excluded from the required set. RED: ExcludeTransient does not exist.
func TestExcludeTransientHoldsOrphanDigestDiffer(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`transient_reads:
- statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
  reason: orphan scan over uid IS NULL, result depends on drain point
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	orphan := backendconformance.DifferentialDifference{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"},
		Kind:        backendconformance.DivergenceResults,
		Detail:      "row digest differs",
	}
	remaining, excluded := allow.ExcludeTransient([]backendconformance.DifferentialDifference{orphan})
	if len(remaining) != 0 {
		t.Fatalf("ExcludeTransient() remaining = %v, want the orphan digest differ excluded", remaining)
	}
	if len(excluded) != 1 {
		t.Fatalf("ExcludeTransient() excluded = %v, want the orphan divergence reported as excluded", excluded)
	}
}

// TestExcludeTransientKeepsFailuresAndMissing pins the exclusion's kind
// scope: a backend error or a one-sided recording on a transient read
// stays required, so the exclusion can never mask a real breakage.
func TestExcludeTransientKeepsFailuresAndMissing(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`transient_reads:
- statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
  reason: orphan scan over uid IS NULL, result depends on drain point
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"},
			Kind:        backendconformance.DivergenceFailures,
			Detail:      "backend error",
		},
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"},
			Kind:        backendconformance.DivergenceMissing,
			Detail:      "recorded on one backend only",
		},
	}
	remaining, excluded := allow.ExcludeTransient(diffs)
	if len(remaining) != 2 {
		t.Fatalf("ExcludeTransient() remaining = %v, want failures and missing kept required", remaining)
	}
	if len(excluded) != 0 {
		t.Fatalf("ExcludeTransient() excluded = %v, want nothing excluded", excluded)
	}
}

// TestExcludeTransientIsNotStaleChecked pins the option-1 contract
// against the allowlist rule: a transient entry that matches no
// divergence in the run is not an error, because transient reads agree
// on most runs by design.
func TestExcludeTransientIsNotStaleChecked(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`transient_reads:
- statement: "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
  reason: orphan scan over uid IS NULL, result depends on drain point
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: graph
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	remaining, excluded := allow.ExcludeTransient(nil)
	if len(remaining) != 0 || len(excluded) != 0 {
		t.Fatalf("ExcludeTransient(nil) = %v, %v, want no error and empty sets", remaining, excluded)
	}
}

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
