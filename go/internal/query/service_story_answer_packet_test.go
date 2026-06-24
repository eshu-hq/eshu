// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestServiceStoryAnswerDataAddsAnswerPacketCompanion(t *testing.T) {
	t.Parallel()

	truth := BuildTruthEnvelope(
		ProfileProduction,
		"platform_impact.context_overview",
		TruthBasisHybrid,
		"resolved from service dossier and platform evidence",
	)
	data := map[string]any{
		"story":         "service-edge-api owns the edge API path.",
		"result_limits": map[string]any{"truncated": false},
	}

	out := serviceStoryAnswerData("service-edge-api", data, truth)
	packet, ok := out["answer_packet"].(AnswerPacket)
	if !ok {
		t.Fatalf("answer_packet = %#v, want AnswerPacket", out["answer_packet"])
	}
	if got, want := packet.PromptFamily, "service.story"; got != want {
		t.Fatalf("answer_packet.prompt_family = %#v, want %#v", got, want)
	}
	if got, want := packet.Supported, true; got != want {
		t.Fatalf("answer_packet.supported = %#v, want %#v", got, want)
	}
	if got, want := packet.PrimaryTool, "get_service_story"; got != want {
		t.Fatalf("answer_packet.primary_tool = %#v, want %#v", got, want)
	}
	if got, want := out["story"], data["story"]; got != want {
		t.Fatalf("story = %#v, want original story", got)
	}
}
