// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestSelectPrimaryPacketIndex(t *testing.T) {
	t.Parallel()

	supported := func(tool, summary string, class query.AnswerTruthClass) query.AnswerPacket {
		return query.AnswerPacket{PrimaryTool: tool, Summary: summary, Supported: true, TruthClass: class}
	}

	t.Run("relevance beats first-supported dispatch order", func(t *testing.T) {
		t.Parallel()
		packets := []query.AnswerPacket{
			supported("get_index_status", "Eshu is indexing 42 repositories", query.AnswerTruthDeterministic),
			supported("get_service_story", "payments service — 3 deployments across prod and staging", query.AnswerTruthDeterministic),
		}
		idx, reason := selectPrimaryPacketIndex("overview of the payments service", packets)
		if idx != 1 {
			t.Fatalf("selected index = %d, want 1 (the relevant payments packet)", idx)
		}
		if reason != "relevance" {
			t.Errorf("reason = %q, want relevance", reason)
		}
	})

	t.Run("supported outranks unsupported continuation", func(t *testing.T) {
		t.Parallel()
		packets := []query.AnswerPacket{
			{PrimaryTool: "get_service_story", Partial: true}, // continuation, unsupported
			supported("get_repository_summary", "repo summary payments", query.AnswerTruthDerived),
		}
		idx, _ := selectPrimaryPacketIndex("payments", packets)
		if idx != 1 {
			t.Fatalf("selected index = %d, want the supported packet 1", idx)
		}
	})

	t.Run("truth strength breaks a relevance tie", func(t *testing.T) {
		t.Parallel()
		packets := []query.AnswerPacket{
			supported("a", "payments service overview", query.AnswerTruthFallback),
			supported("b", "payments service overview", query.AnswerTruthDeterministic),
		}
		idx, _ := selectPrimaryPacketIndex("payments service", packets)
		if idx != 1 {
			t.Fatalf("selected index = %d, want the deterministic packet 1", idx)
		}
	})

	t.Run("stable first on a full tie", func(t *testing.T) {
		t.Parallel()
		packets := []query.AnswerPacket{
			supported("a", "same summary", query.AnswerTruthDeterministic),
			supported("b", "same summary", query.AnswerTruthDeterministic),
		}
		idx, reason := selectPrimaryPacketIndex("unrelated", packets)
		if idx != 0 {
			t.Fatalf("selected index = %d, want 0 (stable, dispatch order)", idx)
		}
		if reason != "first_supported" {
			t.Errorf("reason = %q, want first_supported", reason)
		}
	})

	t.Run("single packet", func(t *testing.T) {
		t.Parallel()
		idx, reason := selectPrimaryPacketIndex("q", []query.AnswerPacket{supported("a", "x", query.AnswerTruthDerived)})
		if idx != 0 || reason != "only_packet" {
			t.Fatalf("idx=%d reason=%q, want 0/only_packet", idx, reason)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if idx, _ := selectPrimaryPacketIndex("q", nil); idx != -1 {
			t.Fatalf("empty packets index = %d, want -1", idx)
		}
	})
}

func TestSelectedPacketSummaryHonoursExplicitIndex(t *testing.T) {
	t.Parallel()

	one := 1
	ans := &Answer{
		Question: "payments overview",
		Packets: []query.AnswerPacket{
			{PrimaryTool: "get_index_status", Summary: "generic", Supported: true, TruthClass: query.AnswerTruthDeterministic},
			{PrimaryTool: "get_service_story", Summary: "payments service story", Supported: true, TruthClass: query.AnswerTruthDeterministic},
		},
		PrimaryPacketIndex: &one,
	}
	if got := selectedPacketSummary(ans); got != "payments service story" {
		t.Fatalf("selectedPacketSummary = %q, want the explicitly indexed packet summary", got)
	}
}
