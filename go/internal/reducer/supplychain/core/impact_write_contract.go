// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"time"
)

// SupplyChainImpactWrite carries findings for durable publication.
type SupplyChainImpactWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Findings     []SupplyChainImpactFinding
	// ScopeFactKinds are the fact kinds of the intent's own generation: what
	// this pass's generation carried, never what cross-scope expansion
	// resolved elsewhere. The writer diffs suppressions only when they are
	// present, so KEV, EPSS and advisory scopes issue no new statement.
	ScopeFactKinds []string
	// SuppressionDiffTimeout overrides the suppression diff statement bound
	// inside the write transaction. Non-positive keeps the default. Tests
	// shorten it to prove the bound; production leaves it unset.
	SuppressionDiffTimeout time.Duration
	// ScanPublications pins each complete derived target result to its
	// protected scan attempt. The writer publishes these witnesses and all
	// findings in one transaction; an omitted witness rejects derived rows.
	ScanPublications []ScanImpactPublicationWitness
	// StaticAdvisoryEvidence is the protected public receipt mapping for static
	// classifier inputs; StaticNonAdvisoryEvidenceFactIDs explicitly classify
	// all remaining static inputs as independent of advisory authority.
	StaticAdvisoryEvidence           []ProtectedStaticImpactPublicAdvisoryEvidence
	StaticNonAdvisoryEvidenceFactIDs []string
	// ProtectedCompleteSet asserts that the trusted loader covered every
	// authorized target and static input for this pass, including an empty
	// target set. It requires the protected transaction port even when no
	// derived finding remains, so prior protected rows can retire atomically.
	ProtectedCompleteSet bool
	// PartialEvidence marks a pass whose evidence load stopped short for a
	// cause that can change a finding's identity: the active-evidence round cap
	// or the per-intent evidence budget (supplyChainImpactTruncation.partial).
	// Its finding set is not the complete truth for the (scope, generation), so
	// the writer upserts it but retracts nothing: hiding a finding because a
	// bounded load did not reach it would trade a stale row for a missing one
	// (#6831). Per-key caps are paged to completion and a bounded suppression
	// tail does not set it, so a scope larger than one page still converges
	// (#7154).
	PartialEvidence bool
	// PartialCauses is the identity-affecting cause set that made this pass
	// partial (supplyChainImpactTruncation.causes), empty for a complete
	// pass. The writer records it on the durable per-scope pass state
	// (#10389) so the status degraded reason can name the cause; it never
	// reaches a metric label.
	PartialCauses []string
	// FencingToken is the database-issued ordering value this pass was stamped
	// with at evidence-read time (#7142). The writer admits the write only when
	// no fresher pass was admitted for the (scope, generation), stamps every
	// upserted and retracted row with it, and rejects a zero token.
	FencingToken int64
	// Recovery asks the writer to also report every CVE the scope's finding
	// rows name (RecoveryCVEIDs). It is set on a retry, when an earlier attempt
	// may have committed and died before its fan-out, so the rerun's own
	// rewrite shows no change and cannot say what the fan-out owes.
	Recovery bool
	// Owed, when set, is run inside the write transaction once the pass's changed
	// CVEs are known and are not empty, so the marker "this commit owes a
	// fan-out" commits with the rows or not at all. A failure rolls the write back.
	Owed FanOutOwedRecorder
	// ScopedCVEIDs restricts one re-derivation to the stamped change that
	// caused it (#9779). When set, the writer upserts findings for these CVEs
	// only and retracts inside the set only; every row outside it is left
	// byte-identical. Empty runs the full pass: upsert everything derived and
	// retract everything else. Handle sets it only for a one-hop pass whose
	// stamp names a bounded set; the sentinel, a stamp without a set, and an
	// owed retry all run full.
	ScopedCVEIDs []string
}

// SupplyChainImpactWriteResult summarizes durable impact publication.
type SupplyChainImpactWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	// FactsRetracted counts prior finding rows of the same (scope, generation)
	// this pass tombstoned because it no longer derives them (#6831).
	FactsRetracted  int
	EvidenceSummary string
	// ChangedCVEIDs are the CVE ids, sorted and de-duplicated, of every finding
	// row this pass wrote that was new, revived, or different in content from the
	// stored row, plus every row it retracted (#9695), plus the CVEs the
	// pass's changed suppressions name (#10019). The holder fan-out asks
	// for the holders of exactly these CVEs.
	ChangedCVEIDs []string
	// SuppressionCVEIDs are the CVEs the pass's changed suppressions name
	// (#10019), the subset of ChangedCVEIDs the suppression diff contributed.
	// A one-hop re-derivation fans out nothing of its own rewrite, but it
	// still fans these out: a stamped first pass of an operator generation
	// would otherwise admit without fanning out.
	SuppressionCVEIDs []string
	// suppressionDiff is the pass's suppression diff report for the
	// handler's signals: action, baseline, counts, and duration.
	suppressionDiff suppressionDiffReport
	// RecoveryCVEIDs are the CVE ids of every active and tombstoned finding row
	// the scope holds at this generation. Set only when the write asked for
	// Recovery.
	RecoveryCVEIDs []string
	// DerivedCVEIDs are the CVE ids of every finding row this pass wrote,
	// plus every row it retracted (#9740). The fan-out compares them with the
	// stamp that flagged the pass: disjoint means the flagged change cannot
	// affect what the pass wrote.
	DerivedCVEIDs []string
	// EvidenceComplete reports the pass loaded its whole evidence set. A
	// truncated load (#7154) may have missed the flagged change, so the
	// fan-out never clears a replay flag on its derived set: the pass
	// reruns instead.
	EvidenceComplete bool
	// ScopedCVEIDs echoes the write's scope: the stamped CVEs this
	// re-derivation covered, or empty for a full pass (#9779).
	ScopedCVEIDs []string
}

// SupplyChainImpactWriter persists reducer-owned impact findings.
type SupplyChainImpactWriter interface {
	WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error)
}
