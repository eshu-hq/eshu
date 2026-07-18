// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// packetRankKey is the deterministic sort key used to select the primary answer
// packet. Fields are compared in priority order: evidence tier first, then
// question relevance, then truth strength, then completeness. A higher key is a
// better primary answer.
type packetRankKey struct {
	tier      int // 3 supported+complete+summary, 2 supported, 1 partial+summary, 0 else
	relevance int // count of question tokens present in the packet's answer text
	truth     int // truth-class strength, deterministic highest
	complete  int // 1 when not partial and not truncated, else 0
}

// greaterThan reports whether k ranks strictly above other. Comparison is
// lexicographic over (tier, relevance, truth, complete); equal keys are not
// greater, so the first (lowest-index) packet wins ties and selection stays
// stable and dispatch-ordered.
func (k packetRankKey) greaterThan(other packetRankKey) bool {
	switch {
	case k.tier != other.tier:
		return k.tier > other.tier
	case k.relevance != other.relevance:
		return k.relevance > other.relevance
	case k.truth != other.truth:
		return k.truth > other.truth
	default:
		return k.complete > other.complete
	}
}

// selectPrimaryPacketIndex ranks packets and returns the index of the most
// relevant answer packet plus a low-cardinality selection reason for telemetry.
// It replaces first-supported-packet selection with a deterministic ranking by
// evidence tier, question relevance, truth strength, and completeness so an
// entity-scoped question publishes the packet that actually answers it rather
// than the first supported packet in dispatch order.
//
// It returns (-1, "") when packets is empty. The reason is "relevance" when the
// ranked winner differs from the first supported packet, "first_supported" when
// they coincide, and "only_packet" for a single-packet answer.
func selectPrimaryPacketIndex(question string, packets []query.AnswerPacket) (int, string) {
	if len(packets) == 0 {
		return -1, ""
	}
	if len(packets) == 1 {
		return 0, "only_packet"
	}
	qTokens := meaningfulTokens(question)
	best := 0
	bestKey := packetRankKeyFor(qTokens, packets[0])
	for i := 1; i < len(packets); i++ {
		key := packetRankKeyFor(qTokens, packets[i])
		if key.greaterThan(bestKey) {
			best, bestKey = i, key
		}
	}
	if best == firstSupportedIndex(packets) {
		return best, "first_supported"
	}
	return best, "relevance"
}

// packetRankKeyFor builds the rank key for a packet against the question tokens.
func packetRankKeyFor(qTokens map[string]struct{}, p query.AnswerPacket) packetRankKey {
	return packetRankKey{
		tier:      packetTier(p),
		relevance: relevanceScore(qTokens, p),
		truth:     truthRank(p.TruthClass),
		complete:  boolToInt(!p.Partial && !p.Truncated),
	}
}

// packetTier buckets a packet by how publishable its evidence is.
func packetTier(p query.AnswerPacket) int {
	switch {
	case p.Supported && !p.Partial && p.Summary != "":
		return 3
	case p.Supported:
		return 2
	case p.Summary != "":
		return 1
	default:
		return 0
	}
}

// truthRank orders truth classes from strongest to weakest so a deterministic
// graph answer outranks a fallback or content hint at equal relevance.
func truthRank(class query.AnswerTruthClass) int {
	switch class {
	case query.AnswerTruthDeterministic:
		return 5
	case query.AnswerTruthSemanticObservation:
		return 4
	case query.AnswerTruthDerived:
		return 3
	case query.AnswerTruthCodeHint:
		return 2
	case query.AnswerTruthFallback:
		return 1
	default:
		return 0
	}
}

// relevanceScore counts the distinct meaningful question tokens present in the
// packet's answer text (Summary, PrimaryTool, and PromptFamily). It is a bounded
// lexical-overlap heuristic, deterministic and provider-free, used only to break
// ties toward the packet that names the question's entity. The packet's own
// Question field is deliberately excluded: every packet in an answer carries the
// same normalized question, so including it would saturate the score and erase
// the relevance signal.
func relevanceScore(qTokens map[string]struct{}, p query.AnswerPacket) int {
	if len(qTokens) == 0 {
		return 0
	}
	packetTokens := meaningfulTokens(p.Summary + " " + p.PrimaryTool + " " + p.PromptFamily)
	score := 0
	for tok := range qTokens {
		if _, ok := packetTokens[tok]; ok {
			score++
		}
	}
	return score
}

// firstSupportedIndex returns the index of the first supported packet, or 0 when
// none is supported. It mirrors the historical first-supported fallback so the
// selection reason can report when the ranked winner coincides with it.
func firstSupportedIndex(packets []query.AnswerPacket) int {
	for i, p := range packets {
		if p.Supported {
			return i
		}
	}
	return 0
}

// selectedPacketSummary returns the deterministic prose for an answer: the
// Summary of the selected primary packet when it is a supported summary-bearing
// packet, else the historical best-packet fallback. It honours an explicitly set
// PrimaryPacketIndex (for example the deterministic count route) and otherwise
// uses the relevance ranking.
func selectedPacketSummary(ans *Answer) string {
	idx := selectedPacketIndex(ans)
	if idx >= 0 && idx < len(ans.Packets) {
		p := ans.Packets[idx]
		if p.Supported && p.Summary != "" {
			return p.Summary
		}
	}
	return bestPacketSummary(ans.Packets)
}

// selectedPacketIndex returns the index of the packet backing the published
// answer: the explicit PrimaryPacketIndex when set and in range, otherwise the
// relevance-ranked winner. It returns -1 when there are no packets.
func selectedPacketIndex(ans *Answer) int {
	if ans.PrimaryPacketIndex != nil {
		i := *ans.PrimaryPacketIndex
		if i >= 0 && i < len(ans.Packets) {
			return i
		}
	}
	idx, _ := selectPrimaryPacketIndex(ans.Question, ans.Packets)
	return idx
}

// meaningfulTokens splits text into a set of lowercased, whole-word tokens,
// excluding a small stop-word set. A token is kept when it is at least three
// characters OR it carries a digit, so a short version or count identifier
// ("v2", "5") that discriminates the right packet is not dropped. This mirrors
// the digit escape in answerguardrail.contentTokens. It is the shared tokenizer
// for the relevance heuristic.
func meaningfulTokens(text string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		tok := strings.Trim(raw, "_")
		if len(tok) < 3 && !tokenHasDigit(tok) {
			continue
		}
		if _, stop := relevanceStopWords[tok]; stop {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

// tokenHasDigit reports whether tok contains at least one digit rune.
func tokenHasDigit(tok string) bool {
	for _, r := range tok {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// relevanceStopWords are common question words that carry no entity signal. The
// set is deliberately small: over-filtering risks dropping a genuine entity
// token, so only unambiguous filler is listed.
var relevanceStopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "are": {}, "was": {}, "were": {},
	"give": {}, "get": {}, "show": {}, "tell": {}, "what": {}, "which": {},
	"who": {}, "how": {}, "why": {}, "when": {}, "where": {}, "does": {},
	"has": {}, "have": {}, "with": {}, "from": {}, "that": {}, "this": {},
	"about": {}, "into": {}, "over": {}, "your": {}, "any": {}, "all": {},
}

// boolToInt returns 1 for true and 0 for false.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
