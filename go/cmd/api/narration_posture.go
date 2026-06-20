package main

import (
	"github.com/eshu-hq/eshu/go/internal/askwiring"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// buildNarrationPosture constructs a func that resolves the current governed
// answer-narration posture from runtime configuration. The returned func is
// safe to call concurrently and reads only from the closed-over values, so it
// can be shared between the engine and the status endpoint.
//
// adapterReady must reflect whether the ask provider adapter was ACTUALLY
// successfully constructed (i.e. provider.NewAdapter succeeded). A profile
// that is present in JSON but whose credential env var is unset will fail
// adapter construction; in that case adapterReady must be false so the status
// endpoint reports ProviderUnavailable rather than Available.
//
// Delegates to [askwiring.BuildNarrationPosture] for the shared gate logic.
func buildNarrationPosture(
	getenv func(string) string,
	adapterReady bool,
) func() status.AnswerNarrationStatus {
	return askwiring.BuildNarrationPosture(getenv, adapterReady)
}
