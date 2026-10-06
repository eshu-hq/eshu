// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openIsolatedBootstrapSchema gives one test its own schema with the full
// production bootstrap (postgres.ApplyBootstrap) applied. Every activation
// live proof drives real queue claims, so none may share a schema (#7479).
func openIsolatedBootstrapSchema(t *testing.T, dsn, prefix string) *sql.DB {
	t.Helper()
	return postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, database *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
	})
}

// dsnForDeferredPartitionMemoProof is the disposable proof DSN, or a skip.
func dsnForDeferredPartitionMemoProof(t *testing.T) string {
	t.Helper()
	return postgresproof.DeferredPartitionProofDSN(t)
}

// catalogTestScope and catalogTestGeneration copy the root package's catalog
// fixtures (ingestion_catalog_cache_test.go): a git repository scope whose
// partition key is the repository id, and a pending snapshot generation.
func catalogTestScope(scopeID, repoID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoID,
	}
}

func catalogTestGeneration(scopeID, generationID string, now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

// testFactChannel returns a closed channel holding envelopes, the fact stream
// shape CommitScopeGeneration reads (copy of the root proof_domain_test.go).
func testFactChannel(envelopes []facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, len(envelopes))
	for _, e := range envelopes {
		ch <- e
	}
	close(ch)
	return ch
}

// seedScope inserts one active scope whose active generation no fixture
// generation equals (copy of the root seedRetentionSelectionScope in
// generation_retention_selection_fixture_live_test.go).
func seedScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	activeGeneration := scopeID + "-active"
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`, []any{scopeID}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), 'active')`, []any{activeGeneration, scopeID}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, []any{activeGeneration, scopeID}},
	} {
		if _, err := database.ExecContext(ctx, step.query, step.args...); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
	}
}

// seedSupersededGeneration inserts one superseded generation (copy of the
// root seedRetentionSelectionSupersededGeneration).
func seedSupersededGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, supersededAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $3)`,
		generationID, scopeID, supersededAt,
	); err != nil {
		t.Fatalf("seed superseded generation %s: %v", generationID, err)
	}
}

// counterValue sums an int64 counter's data points, restricted to the points
// whose attribute key equals value when key is set.
func counterValue(rm metricdata.ResourceMetrics, name, key, value string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if key != "" {
					if v, ok := dp.Attributes.Value(attribute.Key(key)); !ok || v.AsString() != value {
						continue
					}
				}
				total += dp.Value
			}
		}
	}
	return total
}

// histogramCount counts a float64 histogram's samples, restricted to the
// points whose outcome attribute equals outcome when outcome is set.
func histogramCount(rm metricdata.ResourceMetrics, name, outcome string) uint64 {
	var total uint64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range histogram.DataPoints {
				if outcome != "" {
					if value, ok := dp.Attributes.Value("outcome"); !ok || value.AsString() != outcome {
						continue
					}
				}
				total += dp.Count
			}
		}
	}
	return total
}
