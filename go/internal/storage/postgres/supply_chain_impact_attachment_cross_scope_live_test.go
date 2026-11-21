// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerContentionGateSupplyChainImpactCrossScopeAttachmentReachesItsOwnPass
// is the #11105 cross-scope characterization (arbiter ruling item 3). The
// referrer scope S1 runs the attachment and writes the attachment fact under
// S1; the SBOM document's component lives in S2; the vulnerability record
// lives in a third scope. S2's impact pass runs BEFORE S1's attachment
// commits, so it cannot see the attachment. The claim under test: S1's own
// impact pass, ordered after S1's attachment, reads the S2 component and the
// vulnerability cross-scope and publishes the attachment-backed finding. No
// reopen of S2 is needed for the finding to exist.
func TestReducerContentionGateSupplyChainImpactCrossScopeAttachmentReachesItsOwnPass(t *testing.T) {
	w := newAttachWait(t)
	const (
		referrer = "scope:11105:referrer"
		document = "scope:11105:document"
		vulns    = "scope:11105:vulnerabilities"
	)
	for _, scope := range []string{referrer, document, vulns} {
		fanOutSeedScope(t, w.ctx, w.db, scope, "g1")
	}
	w.upsert(w.vulnerabilityFacts(vulns)...)
	w.upsert(w.documentFacts(document)...)
	w.upsert(w.identityFacts(referrer)...)
	w.seedWorkItem("impact-11105-s2", document, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	// S2's impact pass first: no attachment exists anywhere yet. S1's rows
	// are enqueued only after it, so the order is pinned, not left to the
	// work_item_id tiebreak.
	s2 := w.claim(w.impactQ, "S2 impact claim", true)
	if err := w.runImpact(s2); err != nil {
		t.Fatalf("S2 impact pass: %v", err)
	}
	if got := w.attachedFindingsAnyScope(); got != 0 {
		t.Fatalf("attached findings after S2's pass = %d, want 0 (no attachment yet)", got)
	}

	// S1's attachment commits after S2's pass.
	w.seedWorkItem("attach-11105-s1", referrer, reducercontract.DomainSBOMAttestationAttachment, "pending", w.clock.Add(-time.Minute))
	w.seedWorkItem("impact-11105-s1", referrer, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))
	attach := w.claim(w.attachQ, "S1 attachment claim", true)
	w.writeAttachment(referrer, attach)
	if err := w.attachQ.Ack(w.ctx, attach, reducercontract.Result{}); err != nil {
		t.Fatalf("S1 attachment Ack(): %v", err)
	}

	// S1's own impact pass, after its attachment.
	s1 := w.claim(w.impactQ, "S1 impact claim", true)
	if err := w.runImpact(s1); err != nil {
		t.Fatalf("S1 impact pass: %v", err)
	}
	if got := w.attachedFindings(referrer); got != 1 {
		t.Fatalf("attached findings in S1 after its own pass = %d (any scope %d), want 1: the cross-scope component and vulnerability did not reach S1's pass",
			got, w.attachedFindingsAnyScope())
	}
}

// attachedFindingsAnyScope counts active attachment-backed findings for the
// CVE across every scope.
func (w *attachWait) attachedFindingsAnyScope() int {
	w.t.Helper()
	var count int
	if err := w.db.QueryRowContext(w.ctx, `
SELECT count(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.payload->>'cve_id' = $2 AND f.payload->>'subject_digest' = $3`,
		facts.ReducerSupplyChainImpactFindingFactKind, attachWaitCVE, attachWaitDigest).Scan(&count); err != nil {
		w.t.Fatalf("count findings: %v", err)
	}
	return count
}
