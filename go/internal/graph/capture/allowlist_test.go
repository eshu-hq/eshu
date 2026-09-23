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

// tieOrderStatement is the entry-44 shape: ORDER BY over tied keys with
// no truncation, so only delivery order (never the row multiset) can
// differ across backends.
const tieOrderStatement = "MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File) WHERE f.language IS NOT NULL RETURN f.language AS language, count(f) AS file_count ORDER BY file_count DESC"

// TestParseTieOrderReadsAcceptsOrderByWithoutLimit is the GREEN case for
// the tie-order disposition: an ORDER BY read with no truncation
// registers without error. RED: tie_order_reads does not exist, so the
// section is ignored and nothing registers.
func TestParseTieOrderReadsAcceptsOrderByWithoutLimit(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  reason: ORDER BY over tied keys, backend-undefined delivery order
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	tie := backendconformance.DifferentialDifference{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: tieOrderStatement},
		Kind:        backendconformance.DivergenceResults,
		Detail:      "row digest differs",
	}
	remaining, excluded := allow.ExcludeTieOrder([]backendconformance.DifferentialDifference{tie})
	if len(remaining) != 0 {
		t.Fatalf("ExcludeTieOrder() remaining = %v, want the tie-order results differ excluded", remaining)
	}
	if len(excluded) != 1 {
		t.Fatalf("ExcludeTieOrder() excluded = %v, want the tie divergence reported as excluded", excluded)
	}
}

// TestParseTieOrderReadsRejectsLimit pins the guard's fail-closed
// boundary: ORDER BY with LIMIT can return different rows (not just a
// different order) when keys tie, so that statement must stay required
// and never register as tie-order.
func TestParseTieOrderReadsRejectsLimit(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "MATCH (n:Repository) RETURN n ORDER BY n.name LIMIT $limit"
  reason: tied keys with truncation
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want LIMIT rejection")
	}
}

// TestParseTieOrderReadsRejectsSkip pins the other truncation boundary:
// SKIP drops rows, so a skipped read is row-truth, not order-only.
func TestParseTieOrderReadsRejectsSkip(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "MATCH (n:Repository) RETURN n ORDER BY n.name SKIP $offset"
  reason: tied keys with truncation
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want SKIP rejection")
	}
}

// TestParseTieOrderReadsRejectsMissingOrderBy pins that the disposition
// is only for ordering nondeterminism: without ORDER BY the comparator
// already sorts rows, so there is nothing tie-shaped to register.
func TestParseTieOrderReadsRejectsMissingOrderBy(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "MATCH (n:Repository) RETURN n"
  reason: no ordering at all
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want missing-ORDER-BY rejection")
	}
}

// TestParseTieOrderReadsRejectsMixedCaseLimit pins the guard against
// case evasion: Cypher keywords are case-insensitive, so a lowercase
// limit truncates exactly like an uppercase one and must stay required.
// RED: the guard scans case-sensitively, so this registers.
func TestParseTieOrderReadsRejectsMixedCaseLimit(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "MATCH (n:Repository) RETURN n ORDER BY n.name limit $limit"
  reason: tied keys with lowercase truncation
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want mixed-case-LIMIT rejection")
	}
}

// TestParseTieOrderReadsAcceptsLowercaseOrderBy pins the other
// direction: an all-lowercase order by still orders, and the comparator
// treats it as ordered (backendconformance.HasOrderBy is
// case-insensitive), so the guard must accept it too. RED: the guard
// scans case-sensitively, so this is rejected.
func TestParseTieOrderReadsAcceptsLowercaseOrderBy(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`tie_order_reads:
- statement: "match (n:Repository) return n order by n.name"
  reason: lowercase ordering
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v, want lowercase order by accepted", err)
	}
	tie := backendconformance.DifferentialDifference{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: "match (n:Repository) return n order by n.name"},
		Kind:        backendconformance.DivergenceResults,
		Detail:      "row digest differs",
	}
	if remaining, _ := allow.ExcludeTieOrder([]backendconformance.DifferentialDifference{tie}); len(remaining) != 0 {
		t.Fatalf("ExcludeTieOrder() remaining = %v, want the lowercase tie-order differ excluded", remaining)
	}
}

// TestParseTieOrderReadsIgnoresLimitInLiteral pins that the truncation
// scan skips string literals: the word LIMIT inside a quoted value is
// prose, not a clause, and must not block registration.
func TestParseTieOrderReadsIgnoresLimitInLiteral(t *testing.T) {
	if _, err := ParseAllowlist([]byte(`tie_order_reads:
- statement: "MATCH (n:Repository) WHERE n.note = 'LIMIT reached' RETURN n ORDER BY n.name"
  reason: limit word in a literal
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)); err != nil {
		t.Fatalf("ParseAllowlist() error = %v, want LIMIT-in-literal ignored", err)
	}
}

// TestParseTieOrderReadsRejectsTier pins that tie-order exclusions take
// no tier, like transient reads: the exclusion is order-only by design,
// so a tier key would be a silent no-op inviting confusion with the
// allowlist tiers.
func TestParseTieOrderReadsRejectsTier(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  tier: results
  reason: tied keys
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want tie-order-tier rejection")
	}
}

// TestParseTieOrderReadsRejectsMissingReason keeps the accountability
// contract: a tie-order registration without a reason must fail.
func TestParseTieOrderReadsRejectsMissingReason(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want missing-reason rejection")
	}
}

// TestParseTieOrderReadsRejectsMissingUpstream keeps the tracking
// contract: a tie-order registration without an upstream issue must fail.
func TestParseTieOrderReadsRejectsMissingUpstream(t *testing.T) {
	raw := []byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  reason: tied keys
  owner: query
`)
	if _, err := ParseAllowlist(raw); err == nil {
		t.Fatal("ParseAllowlist() error = nil, want missing-upstream rejection")
	}
}

// TestExcludeTieOrderKeepsFailuresMissingAndCounts pins the exclusion's
// kind scope: only the results kind (order-only digest disagreement) is
// excluded. A backend error, a one-sided recording, or a count
// disagreement on the registered statement stays required, so the
// exclusion can never mask a real breakage.
func TestExcludeTieOrderKeepsFailuresMissingAndCounts(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  reason: tied keys
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	diffs := []backendconformance.DifferentialDifference{
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: tieOrderStatement},
			Kind:        backendconformance.DivergenceFailures,
			Detail:      "backend error",
		},
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: tieOrderStatement},
			Kind:        backendconformance.DivergenceMissing,
			Detail:      "recorded on one backend only",
		},
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: tieOrderStatement},
			Kind:        backendconformance.DivergenceRowCount,
			Detail:      "row totals differ",
		},
		{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: tieOrderStatement},
			Kind:        backendconformance.DivergenceExecutions,
			Detail:      "execution counts differ",
		},
	}
	remaining, excluded := allow.ExcludeTieOrder(diffs)
	if len(remaining) != 4 {
		t.Fatalf("ExcludeTieOrder() remaining = %v, want failures, missing, rowcount and executions kept required", remaining)
	}
	if len(excluded) != 0 {
		t.Fatalf("ExcludeTieOrder() excluded = %v, want nothing excluded", excluded)
	}
}

// TestTieOrderReadsAreNotStaleChecked pins the entry-44 fix: a
// tie-order registration that matches no divergence in the run is not
// an error, because tied delivery order agrees on most runs by design.
// Without this, the flap returns the next time both backends happen to
// deliver the tied rows in the same order.
func TestTieOrderReadsAreNotStaleChecked(t *testing.T) {
	allow, err := ParseAllowlist([]byte(`tie_order_reads:
- statement: "` + tieOrderStatement + `"
  reason: tied keys
  upstream: https://github.com/eshu-hq/eshu/issues/6782
  owner: query
`))
	if err != nil {
		t.Fatalf("ParseAllowlist() error = %v", err)
	}
	if err := allow.Validate(nil); err != nil {
		t.Fatalf("Validate(nil) error = %v, want no stale error for tie-order registrations", err)
	}
}
