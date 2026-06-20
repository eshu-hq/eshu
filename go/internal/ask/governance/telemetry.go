package governance

// AskOutcome is a bounded low-cardinality label for the result of an Ask Eshu
// session. It is safe to use as a metric or counter tag.
//
// # Cardinality contract
//
// AskOutcome MUST remain low-cardinality. It MUST NOT encode question text,
// provider response bodies, tenant identifiers, user IDs, or any other
// high-cardinality value. Adding a new constant requires a review of the
// operator dashboard query contract.
type AskOutcome string

const (
	// AskAnswered means the session produced a complete AnswerPacket with at
	// least one supported fact.
	AskAnswered AskOutcome = "answered"
	// AskPartial means the session produced a partial answer (iteration or
	// tool-call limit was reached, or one or more packets are partial).
	AskPartial AskOutcome = "partial"
	// AskNarrated means the session produced a narration that passed the
	// answernarration validator. This is a sub-state of AskAnswered.
	AskNarrated AskOutcome = "narrated"
	// AskDeterministic means the session produced a deterministic packet
	// summary without a narration (posture was not Available or narration
	// was rejected).
	AskDeterministic AskOutcome = "deterministic"
	// AskDenied means the session was denied by a governance gate before
	// any tool calls were issued.
	AskDenied AskOutcome = "denied"
	// AskError means the session failed with a transport or dispatch error
	// that prevented an answer from being assembled.
	AskError AskOutcome = "error"
)

// Valid reports whether o is a recognised AskOutcome constant. Unknown values
// MUST NOT be emitted as metric labels; callers should treat Valid() == false
// as a programming error.
func (o AskOutcome) Valid() bool {
	switch o {
	case AskAnswered, AskPartial, AskNarrated, AskDeterministic, AskDenied, AskError:
		return true
	default:
		return false
	}
}

// AskStage is a bounded low-cardinality label for the phase of an Ask Eshu
// session. It is safe to use as a span or histogram label.
//
// # Cardinality contract
//
// AskStage MUST remain low-cardinality. It MUST NOT encode tool names,
// question fragments, provider model IDs, or tenant identifiers.
type AskStage string

const (
	// AskStagePlan is the initial phase where the engine receives the
	// question and prepares the first completion request.
	AskStagePlan AskStage = "plan"
	// AskStageTool is the phase where the engine dispatches one or more
	// tool calls and collects ResponseEnvelopes.
	AskStageTool AskStage = "tool"
	// AskStageNarrate is the optional phase where the engine issues a
	// bounded narration completion and validates the output.
	AskStageNarrate AskStage = "narrate"
	// AskStageRender is the final phase where the engine assembles the
	// Answer struct returned to the caller.
	AskStageRender AskStage = "render"
)

// Valid reports whether s is a recognised AskStage constant.
func (s AskStage) Valid() bool {
	switch s {
	case AskStagePlan, AskStageTool, AskStageNarrate, AskStageRender:
		return true
	default:
		return false
	}
}
