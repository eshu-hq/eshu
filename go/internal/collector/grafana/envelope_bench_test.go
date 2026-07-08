// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"testing"
	"time"
)

var benchmarkEnvelopeSink any

func BenchmarkNewObservedRuleEnvelope(b *testing.B) {
	ctx := EnvelopeContext{
		ScopeID:             "scope:grafana:prod",
		GenerationID:        "generation:grafana:prod:001",
		CollectorInstanceID: "grafana-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC),
		SourceURI:           "https://grafana.example.com/api/ruler/grafana/api/v1/rules",
		SourceInstanceID:    "grafana:prod",
	}
	rule := AlertRule{
		UID:                "alert-latency",
		Title:              "API latency high",
		RuleGroup:          "api.rules",
		FolderUID:          "folder-api",
		DatasourceUID:      "prometheus-prod",
		UpdatedAt:          ctx.ObservedAt,
		DeclaredMatchState: MatchStateNotCompared,
		FreshnessState:     FreshnessCurrent,
		Outcome:            OutcomeObserved,
	}

	b.ReportAllocs()
	for b.Loop() {
		envelope, err := NewObservedRuleEnvelope(ctx, rule)
		if err != nil {
			b.Fatalf("NewObservedRuleEnvelope() error = %v", err)
		}
		benchmarkEnvelopeSink = envelope
	}
}
