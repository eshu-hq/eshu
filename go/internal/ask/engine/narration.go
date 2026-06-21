package engine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answernarration"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// narrationSystemSentinel is a distinctive string embedded in the narration
// system prompt. Tests use it to discriminate narration calls from main-loop
// calls without coupling to the full prompt text.
const narrationSystemSentinel = "ask-eshu-narration-v1"

// narrationSystemPrompt is the bounded base system instruction for the
// narration completion. It instructs the model to produce a compact JSON object
// whose sentences each cite evidence handles from the source packet. The model
// MUST NOT invent citations; every handle ID it uses must come from the packet.
//
// This base prompt is sufficient for a fully-supported, complete packet.
// buildNarrationSystemPrompt appends partial-signal instructions when the packet
// carries a partial, truncated, or limitation signal, because the narration
// validator (answernarration.Validate) rejects any narration of a partial
// packet that does not surface that signal. Without the appended instructions a
// weaker provider has no way to know it must emit a partial-signal sentence, so
// the validator deterministically rejects otherwise-valid evidence-backed
// narration (issue #3356).
const narrationSystemPrompt = `You are the ` + narrationSystemSentinel + ` narrator.
Your task is to narrate the answer packet below as a short list of human-readable sentences.

Rules:
- Output ONLY valid JSON matching exactly: {"sentences":[{"text":"...","kind":"factual","provenance":[{"kind":"citation","id":"<citation_ref>"}]}]}
- Every factual sentence MUST cite the citation_ref provided in the packet. Use kind "factual" and provenance kind "citation" with the exact id.
- Do NOT invent citation IDs. Use only the citation_ref from the packet.
- Maximum 5 sentences. Each sentence must be under 500 characters.
- Do NOT output any text outside the JSON object.
- Do NOT include markdown fences or explanation.`

// narrationPartialSignalInstructions is appended to the base narration prompt
// when the source packet carries a partial signal. It tells the model exactly
// how to satisfy the validator's partial-signal rule: emit one extra sentence
// whose provenance points at a real packet limitation. The model must copy a
// limitation string verbatim, because the validator matches the provenance id
// against the packet's Limitations / UnsupportedReasons text.
const narrationPartialSignalInstructions = `

This packet is PARTIAL or TRUNCATED. You MUST also include exactly one sentence
that surfaces this so the answer does not look complete:
- Add one sentence with kind "limitation" and provenance kind "limitation".
- Set that provenance id to one of the packet "limitations" or
  "unsupported_reasons" strings, copied verbatim.
- If the packet has no limitations or unsupported_reasons strings, use kind
  "freshness" with provenance kind "freshness" and an empty id instead.
- Do NOT claim the answer is complete.`

// buildNarrationSystemPrompt returns the narration system prompt for the given
// packet. For a fully-supported, complete packet it returns the base prompt.
// When the packet carries a partial signal (Partial, Truncated, or any
// limitation / missing-evidence / unsupported-reason entries) it appends the
// partial-signal instructions so the model can produce narration the validator
// will accept.
func buildNarrationSystemPrompt(packet query.AnswerPacket) string {
	if packetHasPartialSignal(packet) {
		return narrationSystemPrompt + narrationPartialSignalInstructions
	}
	return narrationSystemPrompt
}

// packetHasPartialSignal reports whether the packet carries any signal that the
// narration validator treats as "partial": an explicit partial/truncated flag,
// or any limitation, missing-evidence, or unsupported-reason entry. It mirrors
// the validator's packetHasPartialSignal so the prompt and the validator agree
// on when partial-signal narration is required.
func packetHasPartialSignal(packet query.AnswerPacket) bool {
	return packet.Partial ||
		packet.Truncated ||
		len(packet.Limitations) > 0 ||
		len(packet.MissingEvidence) > 0 ||
		len(packet.UnsupportedReasons) > 0
}

// narrationLocalShape is the JSON shape the model is instructed to return.
// It is parsed locally and then mapped to answernarration.Sentence values.
type narrationLocalShape struct {
	Sentences []narrationLocalSentence `json:"sentences"`
}

// narrationLocalSentence is one sentence in the model's narration JSON output.
type narrationLocalSentence struct {
	Text       string                     `json:"text"`
	Kind       string                     `json:"kind"`
	Provenance []narrationLocalProvenance `json:"provenance,omitempty"`
}

// narrationLocalProvenance is one provenance reference in the model's output.
type narrationLocalProvenance struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// resolveNarrationPosture returns the current narration posture. When no
// posture source has been configured it returns DefaultAnswerNarrationStatus,
// which leaves the engine in the safe Unavailable state.
func (e *Engine) resolveNarrationPosture() status.AnswerNarrationStatus {
	if e.narrationPosture == nil {
		return status.DefaultAnswerNarrationStatus()
	}
	return e.narrationPosture()
}

// SetNarrationPosture injects an optional posture source into the Engine. When
// fn is non-nil, narrate calls fn on each Ask to determine whether governed
// narration is permitted. A nil fn leaves narration in the default Unavailable
// state, preserving deterministic packet-summary prose.
//
// SetNarrationPosture is safe to call before the Engine receives any Ask calls.
// Changing the posture source while Ask calls are in flight is not safe.
func (e *Engine) SetNarrationPosture(fn func() status.AnswerNarrationStatus) {
	e.narrationPosture = fn
}

// narrate attempts to attach a validated narration to ans when the current
// posture permits it. It mutates ans in place:
//
//   - If posture.State != AnswerNarrationAvailable or there are no packets, it
//     leaves ans.Narrated = false and the existing ans.Prose unchanged.
//   - If available: it issues a bounded narration completion, parses the model
//     output into answernarration.Narration, and calls answernarration.Validate.
//     When valid, ans.Prose is replaced with the joined sentence texts and
//     ans.Narrated is set true. When invalid, the deterministic prose is kept,
//     ans.Narrated is false, and a "narration rejected by validator" limitation
//     is appended.
//
// narrate never leaks raw provider text or error bodies into ans.
func (e *Engine) narrate(ctx context.Context, ans *Answer, posture status.AnswerNarrationStatus) {
	if posture.State != status.AnswerNarrationAvailable {
		ans.Narrated = false
		return
	}
	if len(ans.Packets) == 0 {
		ans.Narrated = false
		return
	}

	primary := primaryPacket(ans.Packets)

	narrationText, ok := e.callNarrationAdapter(ctx, primary)
	if !ok {
		e.rejectNarration(ans, "adapter_error", primary, nil)
		return
	}

	narration, err := parseNarrationJSON(narrationText, primary)
	if err != nil {
		e.rejectNarration(ans, "parse_error", primary, nil)
		return
	}

	input := answernarration.Input{
		Packet:             primary,
		Response:           narration,
		CitationIDs:        nil, // no external citation IDs in this context
		MaxSentences:       5,
		MaxSentenceBytes:   500,
		MaxRefsPerSentence: 4,
	}
	verdict := answernarration.Validate(input)
	if !verdict.Valid {
		e.rejectNarration(ans, "validator", primary, verdict.Findings)
		return
	}

	ans.Prose = joinSentences(narration.Sentences)
	ans.Narrated = true
	e.log().Info("ask: narration accepted",
		"partial", packetHasPartialSignal(primary),
		"truth_class", string(primary.TruthClass))
}

// rejectNarration records a narration-gate rejection: it leaves ans.Narrated
// false, keeps the deterministic prose, appends the bounded limitation, and
// emits a structured operator log with a low-cardinality reason. The validator
// finding reason codes are audit-safe (no free-form text), so they are logged
// to let an operator distinguish a format/binding rejection from an
// adapter/parse failure at 3 AM.
func (e *Engine) rejectNarration(ans *Answer, reason string, primary query.AnswerPacket, findings []answernarration.Finding) {
	ans.Narrated = false
	ans.Limitations = appendLimitation(ans.Limitations, "narration rejected by validator")
	reasons := make([]string, 0, len(findings))
	for _, f := range findings {
		reasons = append(reasons, string(f.Reason))
	}
	e.log().Warn("ask: narration rejected",
		"reason", reason,
		"finding_reasons", reasons,
		"partial", packetHasPartialSignal(primary),
		"truth_class", string(primary.TruthClass))
}

// callNarrationAdapter issues a single bounded narration completion call with
// no tools and returns the raw text. It returns (text, true) on success and
// ("", false) if the adapter errors. It never leaks provider error bodies.
func (e *Engine) callNarrationAdapter(ctx context.Context, primary query.AnswerPacket) (string, bool) {
	packetJSON, err := json.Marshal(primary)
	if err != nil {
		return "", false
	}
	messages := []provider.Message{
		{Role: provider.RoleSystem, Text: buildNarrationSystemPrompt(primary)},
		{Role: provider.RoleUser, Text: "Narrate this answer packet:\n" + string(packetJSON)},
	}
	comp, err := e.adapter.Complete(ctx, messages, nil)
	if err != nil {
		return "", false
	}
	return comp.Text, true
}

// parseNarrationJSON unmarshals the model's compact JSON output and maps it
// to an answernarration.Narration. The TruthClass, Supported, and Partial
// fields are taken from the primary packet to prevent drift. On any parse
// failure it returns a non-nil error.
func parseNarrationJSON(text string, primary query.AnswerPacket) (answernarration.Narration, error) {
	text = strings.TrimSpace(text)
	var local narrationLocalShape
	if err := json.Unmarshal([]byte(text), &local); err != nil {
		return answernarration.Narration{}, err
	}

	sentences := make([]answernarration.Sentence, 0, len(local.Sentences))
	for _, ls := range local.Sentences {
		s := answernarration.Sentence{
			Text: ls.Text,
			Kind: answernarration.SentenceKind(ls.Kind),
		}
		for _, lp := range ls.Provenance {
			s.Provenance = append(s.Provenance, answernarration.ProvenanceRef{
				Kind: answernarration.ProvenanceKind(lp.Kind),
				ID:   lp.ID,
			})
		}
		sentences = append(sentences, s)
	}

	return answernarration.Narration{
		TruthClass: primary.TruthClass,
		Supported:  primary.Supported,
		Partial:    primary.Partial,
		Sentences:  sentences,
	}, nil
}

// primaryPacket returns the first supported packet from packets, or packets[0]
// when no packet is supported. Callers must ensure len(packets) > 0.
func primaryPacket(packets []query.AnswerPacket) query.AnswerPacket {
	for _, p := range packets {
		if p.Supported {
			return p
		}
	}
	return packets[0]
}

// joinSentences concatenates the text of all sentences, separated by a space.
func joinSentences(sentences []answernarration.Sentence) string {
	var b strings.Builder
	for i, s := range sentences {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s.Text)
	}
	return b.String()
}
