// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestSharedProjectionRunnerLogsPartitionProcessingError proves a shared-
// projection graph-write failure (e.g. the sql_relationships domain's
// QUERIES_TABLE/etc. MERGE) is not silently swallowed. Before this test,
// ProcessPartitionOnce's returned error was dropped by runOneCycleSequential/
// Concurrent's `if err != nil { continue }` with no log, metric, or durable
// Postgres row anywhere in the shared-projection path -- unlike fact_work_items
// domains (e.g. gcp_resource_materialization), which the reducer's WorkSink.Fail
// durably tracks via attempt_count. That silence made it impossible to prove a
// scripted graph-write fault (issue #5555's ifafaultinjection
// fail-graph-write-once-then-succeed) actually fired against a shared-
// projection domain: scripts/verify-ifa-fault-injection.sh's SQL-targeted
// fail-graph-write-once-then-succeed-sql cell needs this log line as its
// fired-fault, non-vacuity proof.
func TestSharedProjectionRunnerLogsPartitionProcessingError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("ifa fault: fail-graph-write-once-then-succeed (queue-retry) injected one failure for graph-write call #1")
	reader := &fakeSharedIntentReader{
		intents: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-sql-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "view->table",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"action":            "upsert",
					"source_entity_id":  "entity:sql_view:v1",
					"target_entity_id":  "entity:sql_table:t1",
					"repo_id":           "repo-a",
					"relationship_type": "READS_FROM",
				},
				CreatedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	leaseManager := &fakeLeaseManager{granted: true}
	edgeWriter := &fakeEdgeWriter{writeErr: wantErr}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   edgeWriter,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Logger:       logger,
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			LeaseOwner:     "test-runner",
			PollInterval:   10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	out := logBuf.String()
	if !strings.Contains(out, wantErr.Error()) {
		t.Fatalf("expected a shared-projection partition-processing error log containing %q, got log output:\n%s", wantErr.Error(), out)
	}
	if !strings.Contains(out, DomainSQLRelationships) {
		t.Fatalf("expected the error log to name domain %q, got log output:\n%s", DomainSQLRelationships, out)
	}
}
