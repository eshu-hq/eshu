// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "math"

// Truncation causes reported by the evidence load (#7154). The set is closed
// and low cardinality on purpose: each value is a metric attribute.
const (
	// supplyChainImpactTruncationRounds means the active-evidence expansion
	// stopped at maxSupplyChainImpactActiveEvidenceLoads rounds, so evidence
	// reachable only through later rounds was never loaded.
	supplyChainImpactTruncationRounds = "active_expansion_rounds"
	// supplyChainImpactTruncationBudget means the per-intent evidence budget
	// (maxSupplyChainImpactEvidenceEnvelopesPerIntent) was spent, so later
	// expansion stages were skipped or stopped early.
	supplyChainImpactTruncationBudget = "evidence_budget"
)

// supplyChainImpactTruncation records why one pass's bounded evidence load
// stopped short. The causes are kept apart because they do not all threaten
// the finding set (#7154).
//
// A finding's identity is built from its CVE, package, version, status,
// repository and digest. rounds and budget can leave evidence unloaded that
// changes those fields, so a pass truncated for either reason cannot tell a
// superseded finding row from one it merely did not reach: it must not
// retract. The suppression tail is different. A suppression is not part of a
// finding's identity, and core evidence for a call is always loaded in full
// before the tail is counted (see maxSupplyChainImpactActiveEvidenceRowsPerCall
// in internal/storage/postgres), so a tail truncation only makes the pass
// discard its suppression candidates and fail open.
type supplyChainImpactTruncation struct {
	rounds          bool
	budget          bool
	suppressionTail bool
}

// partial reports whether the finding set may be incomplete, which is the
// writer's PartialEvidence contract: upsert only, retract nothing.
func (t supplyChainImpactTruncation) partial() bool {
	return t.rounds || t.budget
}

// any reports whether any bounded load stopped short for any cause.
func (t supplyChainImpactTruncation) any() bool {
	return t.rounds || t.budget || t.suppressionTail
}

// causes lists the identity-affecting causes for the operator counter and log.
func (t supplyChainImpactTruncation) causes() []string {
	var causes []string
	if t.rounds {
		causes = append(causes, supplyChainImpactTruncationRounds)
	}
	if t.budget {
		causes = append(causes, supplyChainImpactTruncationBudget)
	}
	return causes
}

// maxSupplyChainImpactEvidenceEnvelopesPerIntent bounds the expansion evidence
// one intent may hold in memory: the envelopes the active-evidence,
// OS-package, scanner-analysis, resolved-digest and peer-identity stages add
// beyond the intent scope's own base load. Every per-key cap those stages used
// to carry is now paged to completion (#7154), so this budget is the one
// remaining valve against an unbounded read. It counts expansion evidence
// only: the base scope load was never capped, and a large vulnerability scope
// must not be marked partial for its own size.
//
// The default is a documented permanent constraint, not a tuning guess: the
// evidence note docs/internal/evidence/7154-capped-scope-convergence.md holds
// the measured bytes per envelope and the resulting memory bound. A var, not a
// const, so tests can lower it without seeding a hundred thousand rows.
var maxSupplyChainImpactEvidenceEnvelopesPerIntent = 100_000

// supplyChainImpactEvidenceBudget tracks the expansion envelopes one pass has
// loaded against maxSupplyChainImpactEvidenceEnvelopesPerIntent. A nil budget
// is unlimited so stage-level tests can call a stage on its own.
type supplyChainImpactEvidenceBudget struct {
	limit int
	used  int
}

// newSupplyChainImpactEvidenceBudget starts a pass's budget at limit, or at
// the package default when limit is not positive.
func newSupplyChainImpactEvidenceBudget(limit int) *supplyChainImpactEvidenceBudget {
	if limit <= 0 {
		limit = maxSupplyChainImpactEvidenceEnvelopesPerIntent
	}
	return &supplyChainImpactEvidenceBudget{limit: limit}
}

// charge records n newly loaded expansion envelopes and reports whether the
// pass may keep loading. The load that crosses the limit is kept: only later
// loads are skipped, so the budget never discards evidence it already paid for.
func (b *supplyChainImpactEvidenceBudget) charge(n int) bool {
	if b == nil {
		return true
	}
	b.used += n
	return b.used <= b.limit
}

// remaining is how many more expansion envelopes the pass may load before the
// budget is spent, never negative. A nil budget is unlimited.
func (b *supplyChainImpactEvidenceBudget) remaining() int {
	if b == nil {
		return math.MaxInt
	}
	return max(b.limit-b.used, 0)
}

// exhausted reports whether the pass has spent its budget.
func (b *supplyChainImpactEvidenceBudget) exhausted() bool {
	return b != nil && b.used > b.limit
}
