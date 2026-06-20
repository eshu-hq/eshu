package engine

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// scriptedAdapter replays a pre-configured sequence of Completion values, one
// per Complete call. It records all messages seen per call for replay assertions.
type scriptedAdapter struct {
	turns    []provider.Completion
	turnErr  error // returned on the first call if non-nil
	errOnIdx int   // which call index triggers the error (-1 means none)
	calls    int
	received [][]provider.Message // messages per call
}

func (a *scriptedAdapter) Complete(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	idx := a.calls
	a.calls++
	// Copy the message slice for later assertion.
	copied := make([]provider.Message, len(msgs))
	copy(copied, msgs)
	a.received = append(a.received, copied)

	if a.turnErr != nil && idx == a.errOnIdx {
		return provider.Completion{}, a.turnErr
	}
	if idx < len(a.turns) {
		return a.turns[idx], nil
	}
	// Default to an empty final turn so the loop can finish.
	return provider.Completion{Text: "default final"}, nil
}

func (a *scriptedAdapter) ModelID() string { return "scripted-model" }

// recordingRunner records calls and returns a scripted result or error.
type recordingRunner struct {
	env    *query.ResponseEnvelope
	value  any // plain-JSON value when env is nil
	runErr error
	calls  []runCall
}

type runCall struct {
	name string
	args map[string]any
}

func (r *recordingRunner) Run(_ context.Context, name string, args map[string]any) (RunResult, error) {
	r.calls = append(r.calls, runCall{name: name, args: args})
	if r.runErr != nil {
		return RunResult{}, r.runErr
	}
	return RunResult{Envelope: r.env, Value: r.value}, nil
}

// supportedEnvelope returns a minimal *query.ResponseEnvelope that
// NewAnswerPacket treats as Supported == true.
func supportedEnvelope() *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}
}
