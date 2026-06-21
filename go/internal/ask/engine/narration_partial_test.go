package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// promptAwareNarrationAdapter returns partialAware narration only when the
// narration system prompt instructs a "limitation" sentence; otherwise it
// returns factualOnly. This models a provider that follows its instructions, so
// the e2e test fails if the engine sends the static (non-partial-aware) prompt.
type promptAwareNarrationAdapter struct {
	factualOnly  string
	partialAware string
}

func (a *promptAwareNarrationAdapter) Complete(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem && strings.Contains(m.Text, narrationSystemSentinel) {
			if strings.Contains(m.Text, "limitation") {
				return provider.Completion{Text: a.partialAware}, nil
			}
			return provider.Completion{Text: a.factualOnly}, nil
		}
	}
	return provider.Completion{Text: "final answer"}, nil
}

func (a *promptAwareNarrationAdapter) ModelID() string { return "prompt-aware-narration-model" }

// partialPacketWithLimitation builds a supported-but-partial AnswerPacket that
// carries a limitation. This mirrors the #3356 DeepSeek case: the agent loop
// exhausts its budget, the aggregate answer is marked Partial, and the primary
// packet still carries evidence plus a partial signal.
func partialPacketWithLimitation(citationRef, limitation string) query.AnswerPacket {
	return query.AnswerPacket{
		TruthClass:  query.AnswerTruthDeterministic,
		Supported:   true,
		Partial:     true,
		Summary:     "the deterministic partial summary",
		CitationRef: citationRef,
		Limitations: []string{limitation},
	}
}

// TestBuildNarrationSystemPromptPartialAware proves that when the packet carries
// a partial signal the narration system prompt instructs the model to surface a
// partial-signal sentence with limitation/unsupported_reason/freshness
// provenance. Without this, the validator's partial-signal-hidden rule rejects
// every narration the model can produce, which is the #3356 defect.
func TestBuildNarrationSystemPromptPartialAware(t *testing.T) {
	t.Parallel()

	full := buildNarrationSystemPrompt(supportedPacketWithCitation("ref1"))
	if strings.Contains(full, "limitation") {
		t.Errorf("non-partial prompt should not require a limitation sentence; got:\n%s", full)
	}

	partial := buildNarrationSystemPrompt(partialPacketWithLimitation("ref1", "reached max reasoning iterations"))
	if !strings.Contains(partial, "limitation") {
		t.Errorf("partial-packet prompt must instruct a limitation/partial-signal sentence; got:\n%s", partial)
	}
	// The sentinel must remain so test discrimination and the contract hold.
	if !strings.Contains(partial, narrationSystemSentinel) {
		t.Errorf("partial prompt lost the narration sentinel")
	}
}

// TestNarratePartialPacketAccepted is the end-to-end regression for #3356: a
// partial, evidence-backed packet narrated by a model that follows the
// partial-aware prompt (one factual+citation sentence and one limitation
// sentence) MUST pass the validator and attach prose. Before the fix the
// validator rejected it with partial_signal_hidden.
func TestNarratePartialPacketAccepted(t *testing.T) {
	t.Parallel()

	const citationRef = "ref-partial-1"
	const limitation = "reached max reasoning iterations"
	// factualOnly is what a model produces from the BASE prompt: a single
	// factual+citation sentence. It is rejected by the validator for a partial
	// packet (partial_signal_hidden).
	factualOnly := `{"sentences":[` +
		`{"text":"The repository with the most files is repo-x.","kind":"factual","provenance":[{"kind":"citation","id":"ref-partial-1"}]}` +
		`]}`
	// partialAware is what a model produces only when the prompt instructs it
	// to add a partial-signal sentence. It passes the validator.
	partialAware := `{"sentences":[` +
		`{"text":"The repository with the most files is repo-x.","kind":"factual","provenance":[{"kind":"citation","id":"ref-partial-1"}]},` +
		`{"text":"This answer is partial because the reasoning budget was reached.","kind":"limitation","provenance":[{"kind":"limitation","id":"reached max reasoning iterations"}]}` +
		`]}`

	// promptAwareAdapter couples its narration output to the prompt the engine
	// sends. It only emits the partial-signal sentence when the prompt actually
	// instructs a "limitation" sentence. This makes the test fail if the engine
	// reverts to the static prompt, tying the e2e behavior to the prompt fix.
	adapter := &promptAwareNarrationAdapter{
		factualOnly:  factualOnly,
		partialAware: partialAware,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	posture := status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	pkt := partialPacketWithLimitation(citationRef, limitation)
	ans := Answer{
		Question: "Which indexed repository has the most files?",
		Prose:    "deterministic fallback",
		Partial:  true,
		Packets:  []query.AnswerPacket{pkt},
	}
	eng.narrate(context.Background(), &ans, posture)

	if !ans.Narrated {
		t.Fatalf("Narrated = false, want true for evidence-backed partial narration; Limitations = %v", ans.Limitations)
	}
	for _, lim := range ans.Limitations {
		if strings.Contains(lim, "narration rejected by validator") {
			t.Fatalf("unexpected validator rejection for valid partial narration; Limitations = %v", ans.Limitations)
		}
	}
	if !strings.Contains(ans.Prose, "repo-x") {
		t.Errorf("Prose = %q, want narrated text", ans.Prose)
	}
}
