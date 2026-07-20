// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the shared fixtures and assertion helpers for the multi-run
// collection tests in source_multi_run_test.go, split out to keep both files
// under the repo's 500-line cap.

// recordingClient is a fakeClient variant that captures the exact
// TargetConfig FetchRuns received, so a test can observe the value
// validateTarget resolved (e.g. a defaulted MaxRuns) without re-implementing
// validateTarget's own logic.
type recordingClient struct {
	page      RunPage
	err       error
	gotTarget TargetConfig
}

func (c *recordingClient) FetchRuns(_ context.Context, target TargetConfig) (RunPage, error) {
	c.gotTarget = target
	return c.page, c.err
}

// minimalRunSnapshot builds the smallest RunSnapshot the cicdrun fixture
// normalizer accepts: a run ID plus the repository/commit anchors that keep
// GitHubActionsFixtureEnvelopes from also emitting a
// run_missing_repository_or_commit warning envelope, which would otherwise
// pollute run-count/warning assertions in the multi-run tests above.
func minimalRunSnapshot(runID string) RunSnapshot {
	return RunSnapshot{
		Run: map[string]any{
			"id":       runID,
			"head_sha": "0123456789abcdef0123456789abcdef01234567",
			"repository": map[string]any{
				"full_name": "example/repo",
			},
		},
	}
}

// cicdRunFactRunIDs collects the distinct run_id payload values across every
// facts.CICDRunFactKind envelope, so a test can assert exactly which runs a
// claim cycle emitted independently of envelope ordering.
func cicdRunFactRunIDs(envelopes []facts.Envelope) map[string]bool {
	out := map[string]bool{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		if runID, ok := envelope.Payload["run_id"].(string); ok {
			out[runID] = true
		}
	}
	return out
}

// cicdRunFactStableKeysByRunID maps each facts.CICDRunFactKind envelope's
// run_id payload value to its StableFactKey, so a test can assert the key
// stays identical across two separate claim cycles for the same run.
func cicdRunFactStableKeysByRunID(envelopes []facts.Envelope) map[string]string {
	out := map[string]string{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		if runID, ok := envelope.Payload["run_id"].(string); ok {
			out[runID] = envelope.StableFactKey
		}
	}
	return out
}

// sameWorkflowRunSnapshot builds a RunSnapshot for a distinct run of one
// shared workflow: the workflow id/name/path and repository are identical
// across calls (so the fixture normalizer emits the same
// ci.pipeline_definition FactID), and only the run id differs.
func sameWorkflowRunSnapshot(runID string) RunSnapshot {
	return RunSnapshot{
		Workflow: map[string]any{
			"id":   42,
			"name": "Publish",
			"path": ".github/workflows/publish.yml",
		},
		Run: map[string]any{
			"id":          runID,
			"workflow_id": 42,
			"head_sha":    "0123456789abcdef0123456789abcdef01234567",
			"repository": map[string]any{
				"full_name": "example/repo",
			},
		},
	}
}

// factKindCount returns how many envelopes carry the given fact kind.
func factKindCount(envelopes []facts.Envelope, factKind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			count++
		}
	}
	return count
}

// firstDuplicateFactID returns the first FactID that appears more than once in
// the slice, or "" when every FactID is unique.
func firstDuplicateFactID(envelopes []facts.Envelope) string {
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if _, ok := seen[envelope.FactID]; ok {
			return envelope.FactID
		}
		seen[envelope.FactID] = struct{}{}
	}
	return ""
}

// cicdRunFactsEmittedTotal sums every data point of
// eshu_dp_ci_cd_run_facts_emitted_total (the counter is labeled per fact
// kind, so the deduped emitted-fact total is the sum across all label sets).
func cicdRunFactsEmittedTotal(t *testing.T, rm metricdata.ResourceMetrics) int {
	t.Helper()
	total := 0
	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != "eshu_dp_ci_cd_run_facts_emitted_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric eshu_dp_ci_cd_run_facts_emitted_total has type %T, want Sum[int64]", metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				total += int(point.Value)
			}
		}
	}
	return total
}
