// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package askwiring

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/engine"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestConvertAnswerPreservesExplicitPrimaryPacketSelection(t *testing.T) {
	t.Parallel()

	primaryIndex := 1
	converted := convertAnswer(engine.Answer{
		PrimaryPacketIndex: &primaryIndex,
		Packets: []query.AnswerPacket{
			{PrimaryTool: "list_collectors"},
			{PrimaryTool: "list_indexed_repositories"},
		},
	})

	if converted.PrimaryPacketIndex == nil {
		t.Fatal("converted.PrimaryPacketIndex = nil, want explicit selection")
	}
	if got, want := *converted.PrimaryPacketIndex, primaryIndex; got != want {
		t.Fatalf("converted.PrimaryPacketIndex = %d, want %d", got, want)
	}
	if got, want := converted.Packets[0].PrimaryTool, "list_collectors"; got != want {
		t.Fatalf("first packet tool = %q, want %q; conversion must preserve packet order", got, want)
	}
}
