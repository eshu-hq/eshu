package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// narrationAdapter is an Adapter whose narration Complete call is tracked
// separately from the main agentic loop calls.
type narrationAdapter struct {
	// mainTurns are replayed in order for the primary loop.
	mainTurns []provider.Completion
	mainCalls int
	// narrationResponse is the text the adapter returns for the narration
	// bounded call (detected when messages contain the narration system prompt).
	narrationResponse string
	narrationCalls    int
}

func (a *narrationAdapter) Complete(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	// Detect narration call: the first system message contains the narration
	// sentinel so the test can discriminate the two call types.
	for _, m := range msgs {
		if m.Role == provider.RoleSystem && strings.Contains(m.Text, narrationSystemSentinel) {
			a.narrationCalls++
			return provider.Completion{Text: a.narrationResponse}, nil
		}
	}
	// Non-narration call: replay main turns.
	idx := a.mainCalls
	a.mainCalls++
	if idx < len(a.mainTurns) {
		return a.mainTurns[idx], nil
	}
	return provider.Completion{Text: "final answer"}, nil
}

func (a *narrationAdapter) ModelID() string { return "narration-test-model" }

// supportedPacketWithCitation builds a minimal supported AnswerPacket with
// a CitationRef so that a ProvenanceCitation sentence using that CitationRef
// satisfies the validator.
func supportedPacketWithCitation(citationRef string) query.AnswerPacket {
	return query.AnswerPacket{
		TruthClass:  query.AnswerTruthDeterministic,
		Supported:   true,
		Partial:     false,
		Summary:     "the original deterministic summary",
		CitationRef: citationRef,
	}
}

// engineShapedSupportedPacket builds a supported packet exactly as the engine
// produces one in dispatchCall (loop.go) for a route that carries a truth
// envelope but no hydrated citation packet: a classified TruthClass and a
// deterministic Summary, but an EMPTY CitationRef and no EvidenceHandles. This
// is the real packet shape that reaches narrate in production, and the shape the
// prior narration tests never exercised (they always set CitationRef), which is
// why the #3550 narration regression shipped undetected.
func engineShapedSupportedPacket() query.AnswerPacket {
	return query.AnswerPacket{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Partial:    false,
		Summary:    "the original deterministic summary",
		// CitationRef intentionally empty; EvidenceHandles intentionally nil.
	}
}

// TestNarrateEngineShapedPacketTruthProvenance proves that the narration path
// accepts a model narration that backs factual sentences with "truth"
// provenance when the engine packet carries no CitationRef and no hydrated
// citation IDs. Before the #3550 fix the narration system prompt only offered
// "citation" provenance keyed to the (empty) citation_ref, so the validator
// rejected every sentence (uncited_factual_sentence + unknown_provenance) and
// ans.Narrated stayed false for every supported engine answer.
func TestNarrateEngineShapedPacketTruthProvenance(t *testing.T) {
	t.Parallel()

	// A literal-following model that emits "truth" provenance (validator-allowed
	// for any classified packet) when the prompt tells it to, and "citation"
	// provenance otherwise. The test fails if the engine still sends the
	// citation-only prompt for an empty-CitationRef packet.
	truthNarration := `{"sentences":[{"text":"The answer is backed by graph truth.","kind":"factual","provenance":[{"kind":"truth","id":""}]}]}`
	citationNarration := `{"sentences":[{"text":"The answer is backed by graph truth.","kind":"factual","provenance":[{"kind":"citation","id":""}]}]}`

	adapter := &truthAwareNarrationAdapter{
		truthAware:   truthNarration,
		citationOnly: citationNarration,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	posture := status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	pkt := engineShapedSupportedPacket()
	ans := Answer{
		Question: "test",
		Prose:    "original deterministic",
		Packets:  []query.AnswerPacket{pkt},
	}
	eng.narrate(context.Background(), &ans, posture)

	if !ans.Narrated {
		t.Errorf("Narrated = false, want true for engine-shaped packet; Limitations = %v", ans.Limitations)
	}
	want := "The answer is backed by graph truth."
	if ans.Prose != want {
		t.Errorf("Prose = %q, want %q", ans.Prose, want)
	}
	if adapter.narrationCalls != 1 {
		t.Errorf("narrationCalls = %d, want 1", adapter.narrationCalls)
	}
}

// truthAwareNarrationAdapter returns the truth-provenance narration only when
// the narration system prompt instructs "truth" provenance; otherwise it
// returns the citation-only narration. This models a provider that follows its
// instructions, so the test fails if the engine sends the citation-only prompt
// for an empty-CitationRef packet.
type truthAwareNarrationAdapter struct {
	truthAware     string
	citationOnly   string
	narrationCalls int
}

func (a *truthAwareNarrationAdapter) Complete(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem && strings.Contains(m.Text, narrationSystemSentinel) {
			a.narrationCalls++
			if strings.Contains(m.Text, `"truth"`) {
				return provider.Completion{Text: a.truthAware}, nil
			}
			return provider.Completion{Text: a.citationOnly}, nil
		}
	}
	return provider.Completion{Text: "final answer"}, nil
}

func (a *truthAwareNarrationAdapter) ModelID() string { return "truth-aware-narration-model" }

// TestNarratePostureUnavailableKeepsDeterministic verifies that when the
// posture source reports Unavailable the engine skips the narration adapter
// call, leaves ans.Narrated = false, and keeps the deterministic prose.
func TestNarratePostureUnavailableKeepsDeterministic(t *testing.T) {
	t.Parallel()

	adapter := &narrationAdapter{
		mainTurns:         []provider.Completion{{Text: "deterministic prose from loop"}},
		narrationResponse: `{"sentences":[{"text":"narrated","kind":"factual","provenance":[{"kind":"citation","id":"ref1"}]}]}`,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Posture returns default (Unavailable).
	eng.SetNarrationPosture(func() status.AnswerNarrationStatus {
		return status.DefaultAnswerNarrationStatus()
	})

	ans, err := eng.Ask(context.Background(), "what is x?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if ans.Narrated {
		t.Error("Narrated = true, want false when posture is Unavailable")
	}
	if adapter.narrationCalls != 0 {
		t.Errorf("narrationCalls = %d, want 0 (no narration call when posture Unavailable)", adapter.narrationCalls)
	}
}

// TestNarrateValidNarrationAttached verifies that when the posture is
// Available and the adapter returns a narration that passes the real
// answernarration.Validate, ans.Narrated == true and ans.Prose equals the
// joined sentence texts.
func TestNarrateValidNarrationAttached(t *testing.T) {
	t.Parallel()

	const citationRef = "ref-abc123"
	// The narration JSON must satisfy answernarration.Validate:
	// - sentence kind "truth_label" does not need provenance (not factual).
	// - But we also want to be safe: use a truth_label sentence to avoid the
	//   uncited-factual rule, since ProvenanceTruth is allowed when
	//   packet.TruthClass != "".
	//
	// A SentenceFactual sentence requires allowed provenance. The easiest
	// route is ProvenanceCitation with ID == packet.CitationRef.
	narrationJSON := `{"sentences":[{"text":"The answer is backed by graph truth.","kind":"factual","provenance":[{"kind":"citation","id":"ref-abc123"}]}]}`

	adapter := &narrationAdapter{
		// Loop finishes after one turn with a final text turn (no tool calls).
		mainTurns:         []provider.Completion{{Text: "loop prose ignored after narration"}},
		narrationResponse: narrationJSON,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	eng.SetNarrationPosture(func() status.AnswerNarrationStatus {
		return status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	})

	// Drive narrate directly with a hand-crafted Answer so the packet CitationRef matches the narration JSON without a full Ask round-trip.
	posture := status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	pkt := supportedPacketWithCitation(citationRef)
	ans := Answer{
		Question: "test",
		Prose:    "original deterministic",
		Packets:  []query.AnswerPacket{pkt},
	}
	eng.narrate(context.Background(), &ans, posture)

	if !ans.Narrated {
		t.Errorf("Narrated = false, want true; Limitations = %v", ans.Limitations)
	}
	want := "The answer is backed by graph truth."
	if ans.Prose != want {
		t.Errorf("Prose = %q, want %q", ans.Prose, want)
	}
	if adapter.narrationCalls != 1 {
		t.Errorf("narrationCalls = %d, want 1", adapter.narrationCalls)
	}
}

// TestNarrateInvalidNarrationFallsBack verifies that when the adapter returns
// a narration that the real answernarration.Validate rejects (uncited factual
// sentence), ans.Narrated == false, the original prose is retained, and the
// "narration rejected by validator" limitation is present.
func TestNarrateInvalidNarrationFallsBack(t *testing.T) {
	t.Parallel()

	// A factual sentence with no provenance is always rejected.
	badNarrationJSON := `{"sentences":[{"text":"This is a fact with no citation.","kind":"factual"}]}`

	adapter := &narrationAdapter{
		mainTurns:         []provider.Completion{{Text: "original prose"}},
		narrationResponse: badNarrationJSON,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	posture := status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	pkt := supportedPacketWithCitation("ref-xyz")
	const origProse = "original deterministic prose"
	ans := Answer{
		Question: "test",
		Prose:    origProse,
		Packets:  []query.AnswerPacket{pkt},
	}
	eng.narrate(context.Background(), &ans, posture)

	if ans.Narrated {
		t.Error("Narrated = true, want false for invalid narration")
	}
	if ans.Prose != origProse {
		t.Errorf("Prose = %q, want original %q", ans.Prose, origProse)
	}
	var foundLimitation bool
	for _, lim := range ans.Limitations {
		if strings.Contains(lim, "narration rejected by validator") {
			foundLimitation = true
		}
	}
	if !foundLimitation {
		t.Errorf("Limitations %v missing 'narration rejected by validator'", ans.Limitations)
	}
}
