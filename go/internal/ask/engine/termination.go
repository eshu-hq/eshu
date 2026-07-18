// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import "github.com/eshu-hq/eshu/go/internal/query"

// Termination reasons recorded on Answer.TerminationReason for operator
// telemetry. They are low-cardinality and safe to emit as structured-log or
// metric labels; they never encode question text or tool arguments.
const (
	// terminationFinalTurn is the normal exit: the model produced a completion
	// with no further tool calls.
	terminationFinalTurn = "final_turn"
	// terminationEvidenceSufficient is the sufficiency stop: the loop already
	// held answer evidence for the requested facets and the latest turn added no
	// new distinct supported evidence, so continuing would only spin.
	terminationEvidenceSufficient = "evidence_sufficient"
	// terminationDeterministicRoute is the deterministic short-circuit taken by a
	// routed answer (for example the exact indexed-repository count).
	terminationDeterministicRoute = "deterministic_route"
	// terminationMaxIterations is the bound exit: the loop reached MaxIterations
	// without a final model turn or a sufficiency stop.
	terminationMaxIterations = "max_iterations"
)

// evidenceProgress tracks, across loop turns, the distinct primary tools that
// have produced a supported, summary-bearing answer packet. It is the signal the
// evidence-sufficiency stop uses to distinguish a loop that is still gathering
// new evidence from one that is spinning on redundant or oversized calls.
//
// The zero value is ready to use.
type evidenceProgress struct {
	// tools is the set of primary tools that have contributed at least one
	// supported, summary-bearing packet. A repeated call to the same tool is not
	// new progress.
	tools map[string]struct{}
}

// observe records the supported, summary-bearing packets in packets and reports
// whether this turn added a new distinct evidence tool (progress) and whether any
// answer evidence is now held at all (haveEvidence).
//
// A packet counts as answer evidence only when it is Supported, not Partial, and
// carries a non-empty Summary — the same bar bestPacketSummary uses to publish
// deterministic prose. Repeated calls to an already-seen primary tool, oversized
// continuation packets, and refused-call packets never register as progress, so a
// model that keeps re-issuing the same broad call cannot defeat the stop.
func (p *evidenceProgress) observe(packets []query.AnswerPacket) (progress, haveEvidence bool) {
	if p.tools == nil {
		p.tools = make(map[string]struct{})
	}
	for _, pkt := range packets {
		if !packetIsAnswerEvidence(pkt) {
			continue
		}
		tool := pkt.PrimaryTool
		if _, seen := p.tools[tool]; !seen {
			p.tools[tool] = struct{}{}
			progress = true
		}
	}
	return progress, len(p.tools) > 0
}

// packetIsAnswerEvidence reports whether pkt is a supported, complete,
// summary-bearing answer packet — the bar for both deterministic prose and the
// sufficiency stop's evidence signal.
func packetIsAnswerEvidence(pkt query.AnswerPacket) bool {
	return pkt.Supported && !pkt.Partial && pkt.Summary != ""
}
