// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "fmt"

// Drain evaluation lives here rather than in evaluate.go so that file stays
// under the repo's 500-line cap: #5984 added the unroutable-quarantine count
// and its check, which pushed the combined file over. The split is by subject
// -- everything about queue drain state is here, everything about graph and
// query truth stays in evaluate.go.

// DrainCounts is the observed queue state at a drain poll.
type DrainCounts struct {
	FactWorkItemsResidual int64
	// FactWorkItemsDeadLetter is the dead_letter subset of the residual. It never
	// drains on its own (the reducer treats dead_letter as terminal and does not
	// retry it), so a nonzero value is the usual reason a drain times out;
	// reporting it makes the failure diagnosable from the gate output alone.
	FactWorkItemsDeadLetter int64
	// SharedIntentsNonterminal is the total count of shared_projection_intents
	// with completed_at IS NULL, across every domain.
	SharedIntentsNonterminal int64
	// SharedIntentsRequiredNonterminal excludes the advisory domains (see
	// DrainAssertions consumers). It is the value the required drain check uses,
	// so a domain with a known not-yet-draining bug can be quarantined as advisory
	// without blocking the gate while the bug is tracked separately.
	SharedIntentsRequiredNonterminal int64
	// SharedIntentsAdvisoryNonterminal is the nonterminal count in the advisory
	// domains; reported but never blocking.
	SharedIntentsAdvisoryNonterminal int64
	// CrossScopeCompletionEventsNonterminal is the number of producer-completion
	// events still waiting to fan out into their consumer reducer domains. Every
	// persisted event is live work; completion deletes the row atomically with
	// reopening its canonical consumers.
	CrossScopeCompletionEventsNonterminal int64

	// SharedIntentsUnroutableQuarantined is the number of durable rows recording
	// a shared-projection intent no canonical edge write could route (#5984).
	//
	// These rows are TERMINAL: the intent that produced them was completed, so
	// they never drain. That is precisely why this count must not appear in
	// Drained below — putting a never-decreasing number in the poll predicate
	// turns a crisp assertion failure into a drain timeout, which reads as "the
	// pipeline needed longer" and hides the actual finding.
	SharedIntentsUnroutableQuarantined int64
	// RepoDependencyNonterminal is the repo_dependency-domain subset of
	// SharedIntentsNonterminal. Per B-13 (#3859) it is the primary signal that
	// the relationship-generation activation gate drained correctly; reported as
	// detail on the shared-intents finding.
	RepoDependencyNonterminal int64
	// PopulatedDomainsPresent is the number of distinct
	// require-populated domains that have at least one shared_projection_intents
	// row (completed or not). It proves the reducer actually emitted work for
	// those domains, which is the guard against premature drain convergence: a
	// drain poll that reads 0/0 before the reducer has started would otherwise
	// pass on an unreduced pipeline.
	PopulatedDomainsPresent int64
}

// Drained reports whether the queues are within the snapshot's drain bounds. The
// shared-intents bound applies to the required (non-advisory) nonterminal count
// so a quarantined domain does not keep the poll from converging.
func (d DrainCounts) Drained(a DrainAssertions) bool {
	return d.FactWorkItemsResidual <= a.FactWorkItems.Limit() &&
		d.SharedIntentsRequiredNonterminal <= a.SharedProjectionIntents.Limit() &&
		d.CrossScopeCompletionEventsNonterminal == 0
}

// EvaluateDrains turns observed drain counts into required findings.
// expectedPopulatedDomains is the number of domains the reducer must be proven to
// have emitted (the populated-then-drained guard); 0 disables the check.
func EvaluateDrains(d DrainCounts, a DrainAssertions, expectedPopulatedDomains int, r *Report) {
	if expectedPopulatedDomains > 0 {
		r.AddCheck("drains", "reducer_emitted_required_domains",
			d.PopulatedDomainsPresent >= int64(expectedPopulatedDomains), true,
			fmt.Sprintf("populated domains present=%d, want %d (guards against draining an unreduced pipeline)",
				d.PopulatedDomainsPresent, expectedPopulatedDomains))
	}
	factLimit := a.FactWorkItems.Limit()
	r.AddCheck("drains", "fact_work_items_residual",
		d.FactWorkItemsResidual <= factLimit, true,
		fmt.Sprintf("residual=%d (limit %d; status NOT IN succeeded,superseded; dead_letter=%d)",
			d.FactWorkItemsResidual, factLimit, d.FactWorkItemsDeadLetter))

	intentLimit := a.SharedProjectionIntents.Limit()
	r.AddCheck("drains", "shared_projection_intents_nonterminal",
		d.SharedIntentsRequiredNonterminal <= intentLimit, true,
		fmt.Sprintf("required-nonterminal=%d (limit %d; completed_at IS NULL, excl advisory domains; repo_dependency subset=%d; total=%d)",
			d.SharedIntentsRequiredNonterminal, intentLimit, d.RepoDependencyNonterminal, d.SharedIntentsNonterminal))

	r.AddCheck("drains", "cross_scope_completion_events_nonterminal",
		d.CrossScopeCompletionEventsNonterminal == 0, true,
		fmt.Sprintf("nonterminal=%d (limit 0; pending,claimed,running,retrying)",
			d.CrossScopeCompletionEventsNonterminal))

	// Hard zero, no snapshot knob. An unroutable intent means a row the
	// projector emitted could not become an edge, and the corpus is fixed
	// input: there is no legitimate count above zero for it. This is the same
	// principle as the dead-letter bound above -- a drained pipeline has no
	// dead letters -- applied to the loss that quarantine makes survivable.
	// Without it, completing quarantined intents would drain the nonterminal
	// count and the gate would go GREEN on lost edges (#5984).
	r.AddCheck("drains", "shared_projection_unroutable_quarantined",
		d.SharedIntentsUnroutableQuarantined == 0, true,
		fmt.Sprintf("quarantined=%d (limit 0; intents no canonical edge write could route)",
			d.SharedIntentsUnroutableQuarantined))

	// Advisory: nonterminal intents in quarantined domains (e.g. code_calls).
	// Reported so a known-held domain stays visible without blocking the gate.
	if d.SharedIntentsAdvisoryNonterminal > 0 {
		r.AddCheck("drains", "shared_projection_intents_advisory_nonterminal",
			false, false,
			fmt.Sprintf("advisory-domain nonterminal=%d (quarantined; tracked as a follow-up, not blocking)",
				d.SharedIntentsAdvisoryNonterminal))
	}
}
