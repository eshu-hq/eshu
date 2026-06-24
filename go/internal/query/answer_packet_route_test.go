// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func requireAnswerPacketCompanion(t *testing.T, data map[string]any, promptFamily string) map[string]any {
	t.Helper()

	packet, ok := data["answer_packet"].(map[string]any)
	if !ok {
		t.Fatalf("answer_packet = %#v, want object", data["answer_packet"])
	}
	if got, want := packet["prompt_family"], promptFamily; got != want {
		t.Fatalf("answer_packet.prompt_family = %#v, want %#v", got, want)
	}
	if got, want := packet["supported"], true; got != want {
		t.Fatalf("answer_packet.supported = %#v, want %#v", got, want)
	}
	if packet["truth_class"] == "" {
		t.Fatalf("answer_packet.truth_class = %#v, want non-empty", packet["truth_class"])
	}
	return packet
}
