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

// sufficiencyNoProgressTurns is the number of consecutive turns that must add no
// new distinct supported evidence before the evidence-sufficiency stop fires,
// once answer evidence is already held. Requiring consecutive no-progress turns
// (rather than stopping on the first) leaves a model that is mid-narrowing — about
// to issue a distinct tool, refine a call with tighter arguments, or retry the
// bounded continuation of a runaway result — one more turn to make progress, so a
// legitimate multi-turn flow is not truncated early. A genuinely spinning loop
// still stops well before the iteration bound.
const sufficiencyNoProgressTurns = 2

// evidenceProgress tracks, across loop turns, the distinct answer packets that
// have been gathered, keyed by packet identity. It is the signal the
// evidence-sufficiency stop uses to distinguish a loop that is still gathering
// new evidence from one that is spinning on redundant or oversized calls.
//
// The zero value is ready to use.
type evidenceProgress struct {
	// seen is the set of packet identities that have contributed at least one
	// supported, summary-bearing packet. Identity is keyed by tool AND summary
	// content, so a re-issued call with the same result is not new progress but a
	// distinct result from the same tool (for example comparing several services
	// with repeated get_service_story calls) is.
	seen map[string]struct{}
}

// observe records the supported, summary-bearing packets in packets and reports
// whether this turn added a new distinct evidence packet (progress) and whether
// any answer evidence is now held at all (haveEvidence).
//
// A packet counts as answer evidence only when it is Supported, not Partial, and
// carries a non-empty Summary — the same bar bestPacketSummary uses to publish
// deterministic prose. Identity is (PrimaryTool, Summary): a repeated call that
// returns the same result, an oversized continuation packet, and a refused-call
// packet never register as progress, so a model re-issuing the same broad call
// cannot defeat the stop; but a distinct result from the same tool — a
// multi-entity comparison — is correctly counted as new evidence and does not
// trip the stop prematurely.
func (p *evidenceProgress) observe(packets []query.AnswerPacket) (progress, haveEvidence bool) {
	if p.seen == nil {
		p.seen = make(map[string]struct{})
	}
	for _, pkt := range packets {
		if !packetIsAnswerEvidence(pkt) {
			continue
		}
		id := packetIdentity(pkt)
		if _, ok := p.seen[id]; !ok {
			p.seen[id] = struct{}{}
			progress = true
		}
	}
	return progress, len(p.seen) > 0
}

// packetIdentity returns the distinct-evidence key for a packet: its primary tool
// joined with its summary content. Two calls to the same tool that return
// different summaries are distinct evidence; two calls that return the same
// summary are redundant. The NUL separator cannot appear in a tool name, so it
// unambiguously joins the two fields.
func packetIdentity(pkt query.AnswerPacket) string {
	return pkt.PrimaryTool + "\x00" + pkt.Summary
}

// packetIsAnswerEvidence reports whether pkt is a supported, complete,
// summary-bearing answer packet — the bar for both deterministic prose and the
// sufficiency stop's evidence signal.
func packetIsAnswerEvidence(pkt query.AnswerPacket) bool {
	return pkt.Supported && !pkt.Partial && pkt.Summary != ""
}
