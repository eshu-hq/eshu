// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"
)

var sqlRefreshFenceRelationshipTypes = []string{
	"EXECUTES",
	"HAS_COLUMN",
	"INDEXES",
	"MIGRATES",
	"QUERIES_TABLE",
	"READS_FROM",
	"TRIGGERS",
}

// TestProcessPartitionOnceSQLRefreshFenceRedeliveryConverges proves the refresh
// fence is generation-aware and exact retries are idempotent. Edge partitions
// deliberately run before the refresh partition. A generation-blind fence lets
// the next generation's writes complete, then its late refresh retract deletes
// them permanently. A correct fence defers them until that generation's refresh
// completes. The same-generation cell uses production-stable IDs and proves an
// exact durable upsert does not reopen completed work. Both cells retain all
// seven SQL edge families without reducing partition concurrency.
func TestProcessPartitionOnceSQLRefreshFenceRedeliveryConverges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		secondGeneration string
	}{
		{
			name:             "same generation redelivery",
			secondGeneration: "gen-1",
		},
		{
			name:             "next generation reuses source run",
			secondGeneration: "gen-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
			const (
				partitionCount = 8
				repositoryID   = "repo-sql"
			)

			initial := sqlRefreshFenceDeliveryIntents(
				repositoryID, "run-reused", "gen-1", now.Add(-2*time.Minute),
			)
			store := newFenceTrackingIntentStore(initial)
			edges := newSQLRelationshipStateWriter()
			lease := &stubLeaseManager{claimResult: true}
			readiness := readinessLookupFixed(true, true)

			drainSQLRefreshFencePartitions(
				t, now.Add(-time.Minute), partitionCount, store, edges, lease,
				acceptedGenerationFixed("gen-1", true), readiness,
			)
			assertSQLRelationshipSet(t, edges.relationshipTypes(), sqlRefreshFenceRelationshipTypes)

			redelivery := sqlRefreshFenceDeliveryIntents(
				repositoryID, "run-reused", tt.secondGeneration, now,
			)
			upsertFenceTrackingDelivery(store, redelivery)
			accepted := acceptedGenerationFixed(tt.secondGeneration, true)

			refreshPartition, err := PartitionForKey(
				repoWideRetractRefreshPartitionKey(DomainSQLRelationships, repositoryID),
				partitionCount,
			)
			if err != nil {
				t.Fatalf("PartitionForKey(refresh): %v", err)
			}

			// Force the hostile cross-partition interleaving: every edge partition
			// races ahead of the re-fired refresh, then gets one retry after it.
			for partitionID := 0; partitionID < partitionCount; partitionID++ {
				if partitionID != refreshPartition {
					processSQLRefreshFencePartition(
						t, now, partitionID, partitionCount, store, edges, lease, accepted, readiness,
					)
				}
			}
			processSQLRefreshFencePartition(
				t, now, refreshPartition, partitionCount, store, edges, lease, accepted, readiness,
			)
			for partitionID := 0; partitionID < partitionCount; partitionID++ {
				if partitionID != refreshPartition {
					processSQLRefreshFencePartition(
						t, now, partitionID, partitionCount, store, edges, lease, accepted, readiness,
					)
				}
			}

			assertSQLRelationshipSet(t, edges.relationshipTypes(), sqlRefreshFenceRelationshipTypes)
		})
	}
}

func sqlRefreshFenceDeliveryIntents(
	repositoryID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	refresh := BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainSQLRelationships,
		PartitionKey:     repoWideRetractRefreshPartitionKey(DomainSQLRelationships, repositoryID),
		ScopeID:          "scope-sql",
		AcceptanceUnitID: repositoryID,
		RepositoryID:     repositoryID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		Payload: map[string]any{
			"repo_id":     repositoryID,
			"intent_type": repoRefreshIntentType,
			"action":      repoRefreshAction,
		},
		CreatedAt: createdAt,
	})
	rows := []SharedProjectionIntentRow{refresh}
	for _, relationshipType := range sqlRefreshFenceRelationshipTypes {
		edgeIdentity := "sql-source->sql-target:" + relationshipType
		row := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainSQLRelationships,
			PartitionKey: sqlRelationshipFilePartitionKey(
				repositoryID,
				"db/schema.sql",
				edgeIdentity,
			),
			IdentityKey:      edgeIdentity,
			ScopeID:          "scope-sql",
			AcceptanceUnitID: repositoryID,
			RepositoryID:     repositoryID,
			SourceRunID:      sourceRunID,
			GenerationID:     generationID,
			Payload: map[string]any{
				"repo_id":            repositoryID,
				"relationship_type":  relationshipType,
				"action":             "upsert",
				retractViaRefreshKey: true,
			},
			CreatedAt: createdAt,
		})
		rows = append(rows, row)
	}
	return rows
}

// upsertFenceTrackingDelivery mirrors SharedIntentStore.UpsertIntents' identity
// and completion behavior: a new deterministic ID is added as pending, while an
// existing completed ID stays completed even when payload/created_at are
// refreshed by an exact retry.
func upsertFenceTrackingDelivery(store *fenceTrackingIntentStore, rows []SharedProjectionIntentRow) {
	for _, row := range rows {
		if _, exists := store.byID[row.IntentID]; !exists {
			store.pending = append(store.pending, row)
		}
		store.byID[row.IntentID] = row
	}
}

func drainSQLRefreshFencePartitions(
	t *testing.T,
	now time.Time,
	partitionCount int,
	store *fenceTrackingIntentStore,
	edges *sqlRelationshipStateWriter,
	lease *stubLeaseManager,
	accepted AcceptedGenerationLookup,
	readiness GraphProjectionReadinessLookup,
) {
	t.Helper()
	for pass := 0; pass < partitionCount+2; pass++ {
		progressed := false
		for partitionID := 0; partitionID < partitionCount; partitionID++ {
			result := processSQLRefreshFencePartition(
				t, now, partitionID, partitionCount, store, edges, lease, accepted, readiness,
			)
			progressed = progressed || result.ProcessedIntents > 0
		}
		if !progressed {
			return
		}
	}
	t.Fatal("shared projection backlog did not converge within the bounded drain")
}

func processSQLRefreshFencePartition(
	t *testing.T,
	now time.Time,
	partitionID int,
	partitionCount int,
	store *fenceTrackingIntentStore,
	edges *sqlRelationshipStateWriter,
	lease *stubLeaseManager,
	accepted AcceptedGenerationLookup,
	readiness GraphProjectionReadinessLookup,
) PartitionProcessResult {
	t.Helper()
	result, err := ProcessPartitionOnce(
		context.Background(),
		now,
		PartitionProcessorConfig{
			Domain:         DomainSQLRelationships,
			PartitionID:    partitionID,
			PartitionCount: partitionCount,
			LeaseOwner:     "sql-refresh-proof",
			LeaseTTL:       30 * time.Second,
			BatchLimit:     100,
			EvidenceSource: sqlRelationshipEvidenceSource,
		},
		lease,
		store,
		edges,
		accepted,
		nil,
		readiness,
		nil,
		nil,
		store,
		nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce(partition=%d): %v", partitionID, err)
	}
	return result
}

type sqlRelationshipStateWriter struct {
	edges map[string]struct{}
}

func newSQLRelationshipStateWriter() *sqlRelationshipStateWriter {
	return &sqlRelationshipStateWriter{edges: make(map[string]struct{})}
}

func (w *sqlRelationshipStateWriter) RetractEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	if len(rows) > 0 {
		clear(w.edges)
	}
	return nil
}

func (w *sqlRelationshipStateWriter) WriteEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	for _, row := range rows {
		if relationshipType := payloadStr(row.Payload, "relationship_type"); relationshipType != "" {
			w.edges[relationshipType] = struct{}{}
		}
	}
	return nil
}

func (w *sqlRelationshipStateWriter) relationshipTypes() []string {
	result := make([]string, 0, len(w.edges))
	for relationshipType := range w.edges {
		result = append(result, relationshipType)
	}
	sort.Strings(result)
	return result
}

func assertSQLRelationshipSet(t *testing.T, got, want []string) {
	t.Helper()
	want = append([]string(nil), want...)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("SQL relationship set = %v, want exact set %v", got, want)
	}
}
