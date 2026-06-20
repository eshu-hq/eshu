// Package engine implements the Ask Eshu agent loop.
//
// # Contract
//
// This package is Tier 1: canonical-route orchestration only. It drives the
// LLM completion/tool-call cycle by routing each tool call through a Runner
// that dispatches to the existing Eshu query surface. The engine is
// strictly read-only: it never mutates graph state, never stores completions,
// and never leaks provider request/response bodies, system prompts, or
// credentials to callers.
//
// # Truth model
//
// Deterministic AnswerPackets produced by the underlying query surface are
// canonical. Narrated prose is an optional validated view layered on top: it
// is gated on Narrated == true in an Answer and is never presented as a
// substitute for the machine-readable packets.
//
// # Usage
//
//	eng, err := engine.New(adapter, runner, tools, engine.DefaultOptions())
//	if err != nil {
//	    // adapter or runner was nil
//	}
//	answer, err := eng.Ask(ctx, "Which services depend on pkg X?")
package engine
