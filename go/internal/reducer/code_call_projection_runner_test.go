package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestCodeCallProjectionRunnerProcessesRepoAtomically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "refresh-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "repo:repo-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "refresh"},
				CreatedAt:        now,
			},
			{
				IntentID:         "edge-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"repo_id":          "repo-a",
					"caller_entity_id": "caller",
					"callee_entity_id": "callee",
					"evidence_source":  codeCallEvidenceSource,
				},
				CreatedAt: now.Add(time.Second),
			},
			{
				IntentID:         "meta-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "child->meta",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"repo_id":           "repo-a",
					"source_entity_id":  "child",
					"target_entity_id":  "meta",
					"relationship_type": "USES_METACLASS",
					"evidence_source":   pythonMetaclassEvidenceSource,
				},
				CreatedAt: now.Add(2 * time.Second),
			},
			{
				IntentID:         "stale-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "stale",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-old",
				Payload:          map[string]any{"action": "refresh"},
				CreatedAt:        now.Add(-time.Second),
			},
		},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {
				{
					IntentID:         "stale-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "stale",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-old",
					Payload:          map[string]any{"action": "refresh"},
					CreatedAt:        now.Add(-time.Second),
				},
				{
					IntentID:         "refresh-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "repo:repo-a",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload:          map[string]any{"action": "refresh"},
					CreatedAt:        now,
				},
				{
					IntentID:         "edge-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "caller->callee",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload: map[string]any{
						"repo_id":          "repo-a",
						"caller_entity_id": "caller",
						"callee_entity_id": "callee",
						"evidence_source":  codeCallEvidenceSource,
					},
					CreatedAt: now.Add(time.Second),
				},
				{
					IntentID:         "meta-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "child->meta",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload: map[string]any{
						"repo_id":           "repo-a",
						"source_entity_id":  "child",
						"target_entity_id":  "meta",
						"relationship_type": "USES_METACLASS",
						"evidence_source":   pythonMetaclassEvidenceSource,
					},
					CreatedAt: now.Add(2 * time.Second),
				},
			},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: CodeCallProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if result.ProcessedIntents != 4 {
		t.Fatalf("ProcessedIntents = %d, want 4", result.ProcessedIntents)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want 2 evidence-source retracts", len(writer.retractCalls))
	}
	if got, want := writer.retractCalls[0].evidenceSource, codeCallEvidenceSource; got != want {
		t.Fatalf("retractCalls[0].evidenceSource = %q, want %q", got, want)
	}
	if got, want := writer.retractCalls[1].evidenceSource, pythonMetaclassEvidenceSource; got != want {
		t.Fatalf("retractCalls[1].evidenceSource = %q, want %q", got, want)
	}
	if len(writer.writeCalls) != 2 {
		t.Fatalf("len(writeCalls) = %d, want 2 evidence-grouped writes", len(writer.writeCalls))
	}
	if len(reader.marked) != 4 {
		t.Fatalf("len(marked) = %d, want 4", len(reader.marked))
	}
	if got, want := result.MaxIntentWaitSeconds, 1.0; got != want {
		t.Fatalf("MaxIntentWaitSeconds = %.3f, want %.3f", got, want)
	}
	if result.ProcessingDurationSeconds < 0 {
		t.Fatalf("ProcessingDurationSeconds = %.3f, want non-negative", result.ProcessingDurationSeconds)
	}
}

func TestCodeCallProjectionRunnerProcessOnceReportsReadinessBlockedWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 15, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now.Add(-5 * time.Minute),
			},
		},
		leaseGranted: true,
	}
	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf)
	runner := CodeCallProjectionRunner{
		IntentReader:    reader,
		LeaseManager:    reader,
		EdgeWriter:      &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:     acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: readinessLookupFixed(false, false),
		Config:          CodeCallProjectionRunnerConfig{BatchLimit: 10},
		Logger:          logger,
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := result.ProcessedIntents, 0; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	if got, want := result.BlockedReadiness, 1; got != want {
		t.Fatalf("BlockedReadiness = %d, want %d", got, want)
	}
	if got, want := result.MaxBlockedIntentWaitSeconds, 300.0; got != want {
		t.Fatalf("MaxBlockedIntentWaitSeconds = %.3f, want %.3f", got, want)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want empty while readiness blocked", reader.marked)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := entry["blocked_intent_wait_seconds"], 300.0; got != want {
		t.Fatalf("blocked_intent_wait_seconds = %v, want %v", got, want)
	}
}

func TestCodeCallProjectionRunnerProcessOnceHeartbeatsLeaseDuringLongWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC)
	release := make(chan struct{})
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "edge-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"repo_id":          "repo-a",
					"caller_entity_id": "caller",
					"callee_entity_id": "callee",
					"evidence_source":  codeCallEvidenceSource,
				},
				CreatedAt: now,
			},
		},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {
				{
					IntentID:         "edge-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "caller->callee",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload: map[string]any{
						"repo_id":          "repo-a",
						"caller_entity_id": "caller",
						"callee_entity_id": "callee",
						"evidence_source":  codeCallEvidenceSource,
					},
					CreatedAt: now,
				},
			},
		},
		leaseGranted: true,
		afterClaim: func(count int) {
			if count == 2 {
				close(release)
			}
		},
	}
	writer := &blockingCodeCallProjectionEdgeWriter{
		recordingCodeCallProjectionEdgeWriter: recordingCodeCallProjectionEdgeWriter{},
		release:                               release,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		ReadinessLookup: func(_ GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
			return true, true
		},
		Config: CodeCallProjectionRunnerConfig{
			LeaseTTL:   10 * time.Millisecond,
			BatchLimit: 10,
		},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if got, want := reader.claimsCount(), 2; got < want {
		t.Fatalf("lease claims = %d, want at least %d", got, want)
	}
	if got, want := len(reader.marked), 1; got != want {
		t.Fatalf("len(marked) = %d, want %d", got, want)
	}
	if got, want := len(writer.writeCalls), 1; got != want {
		t.Fatalf("len(writeCalls) = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerRunContinuesAfterCycleError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "edge-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"repo_id":          "repo-a",
					"caller_entity_id": "caller",
					"callee_entity_id": "callee",
					"evidence_source":  codeCallEvidenceSource,
				},
				CreatedAt: now,
			},
		},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {
				{
					IntentID:         "edge-1",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "caller->callee",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload: map[string]any{
						"repo_id":          "repo-a",
						"caller_entity_id": "caller",
						"callee_entity_id": "callee",
						"evidence_source":  codeCallEvidenceSource,
					},
					CreatedAt: now,
				},
			},
		},
		leaseGranted: true,
	}
	writer := &flakyCodeCallProjectionEdgeWriter{
		err:             errors.New("neo4j transient write conflict"),
		retractFailures: 1,
	}

	waits := make([]time.Duration, 0, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: CodeCallProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
		Wait: func(_ context.Context, interval time.Duration) error {
			waits = append(waits, interval)
			if len(waits) == 1 {
				return nil
			}
			cancel()
			return context.Canceled
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(reader.marked); got != 1 {
		t.Fatalf("len(marked) = %d, want 1 completed intent after retry", got)
	}
	if got := len(writer.writeCalls); got != 1 {
		t.Fatalf("len(writeCalls) = %d, want 1 successful write call", got)
	}
	if got := len(waits); got != 2 {
		t.Fatalf("len(waits) = %d, want 2 waits (post-error backoff, then idle poll)", got)
	}
	if got, want := waits[0], 10*time.Millisecond; got != want {
		t.Fatalf("waits[0] = %v, want %v", got, want)
	}
	if got, want := waits[1], 10*time.Millisecond; got != want {
		t.Fatalf("waits[1] = %v, want %v", got, want)
	}
}
