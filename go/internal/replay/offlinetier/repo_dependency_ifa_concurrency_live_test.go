// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"crypto/sha1" // #nosec G505 -- fixture mirrors the production non-cryptographic stable ID.
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const repoDependencyIfaOduName = "odu:repo-dependency-concurrency"

func TestRepoDependencyIfaConcurrencyLive(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	odu := mustRepoDependencyIfaOdu(t)
	baseRows := repoDependencyIfaRows(t, odu)
	baseExpectedEdges := repoDependencyIfaExpectedEdges(baseRows)
	artifactIDs := repoDependencyIfaArtifactIDs(t, baseRows)
	acquireRepoDependencyIfaExclusiveBackend(ctx, t, exec, artifactIDs)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupRepoDependencyConcurrencyScope(cleanupCtx, t, exec, artifactIDs)
		assertRepoDependencyIfaCleanup(cleanupCtx, t, exec, artifactIDs)
	})

	var baseline []string
	for _, workers := range []int{1, 2, 4} {
		cleanupRepoDependencyConcurrencyScope(ctx, t, exec, artifactIDs)
		assertRepoDependencyIfaCleanup(ctx, t, exec, artifactIDs)
		seedRepoDependencyIfaRepositories(ctx, t, exec, odu)

		store := newRepoDependencyIfaStore(baseRows)
		writer := &repoDependencyOverlapWriter{
			inner: cypher.NewEdgeWriter(&cypher.RetryingExecutor{Inner: exec}, 0),
			delay: 15 * time.Millisecond,
		}
		replayer := &repoDependencyIfaReplayer{}
		baseStarted := time.Now()
		runRepoDependencyIfaRunner(ctx, t, store, writer, replayer, workers)
		baseElapsed := time.Since(baseStarted)
		if writer.maxConcurrent() < workers {
			t.Fatalf(
				"workers=%d max concurrent graph writes=%d, want >=%d",
				workers,
				writer.maxConcurrent(),
				workers,
			)
		}
		if got, want := replayer.requestCount(), 8; got != want {
			t.Fatalf("workers=%d workload replay requests=%d, want %d", workers, got, want)
		}
		if got, want := replayer.sharedTargetRequestCount(), 4; got != want {
			t.Fatalf("workers=%d shared-target workload replay requests=%d, want %d", workers, got, want)
		}

		baseSnapshot := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
		assertRepoDependencyIfaSnapshot(t, baseSnapshot, baseExpectedEdges)

		phaseTwoRows := repoDependencyIfaRetractRows(t, odu, "repository:source-08")
		store.upsert(phaseTwoRows)
		store.accept(phaseTwoRows)
		runRepoDependencyIfaRunner(ctx, t, store, writer, replayer, workers)
		phaseTwoSnapshot := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
		phaseTwoExpectedEdges := repoDependencyIfaEdgesWithoutSource(
			baseExpectedEdges,
			"repository:source-08",
		)
		assertRepoDependencyIfaSnapshot(t, phaseTwoSnapshot, phaseTwoExpectedEdges)

		// Durable intent upsert preserves completed_at. Replaying the old base
		// generation must not reopen or resurrect source-08's stale edge.
		store.upsert(baseRows)
		if got := store.pendingCount(); got != 0 {
			t.Fatalf("workers=%d duplicate old-generation replay reopened %d intents", workers, got)
		}
		afterDuplicate := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
		missing, extra := bidirectionalStringDiff(phaseTwoSnapshot.canonical, afterDuplicate.canonical)
		if len(missing) != 0 || len(extra) != 0 {
			t.Fatalf("workers=%d duplicate replay graph diff=%d/%d missing=%v extra=%v", workers, len(missing), len(extra), missing, extra)
		}

		if workers == 1 {
			baseline = afterDuplicate.canonical
		} else {
			missing, extra = bidirectionalStringDiff(baseline, afterDuplicate.canonical)
			if len(missing) != 0 || len(extra) != 0 {
				t.Fatalf("workers=%d serial-to-concurrent graph diff=%d/%d missing=%v extra=%v", workers, len(missing), len(extra), missing, extra)
			}
		}
		t.Logf(
			"workers=%d base_elapsed=%s max_concurrent_writes=%d base_rows=%d final_rows=%d serial_diff=0/0",
			workers,
			baseElapsed,
			writer.maxConcurrent(),
			len(baseSnapshot.canonical),
			len(afterDuplicate.canonical),
		)
	}
}

func repoDependencyIfaArtifactIDs(
	t *testing.T,
	rows []reducer.SharedProjectionIntentRow,
) []string {
	t.Helper()
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		artifacts, ok := row.Payload["evidence_artifacts"].([]map[string]any)
		if !ok || len(artifacts) != 1 {
			t.Fatalf("intent %q evidence_artifacts=%T/%d, want one typed artifact", row.IntentID, row.Payload["evidence_artifacts"], len(artifacts))
		}
		artifact := artifacts[0]
		parts := []string{
			strings.TrimSpace(fmt.Sprint(row.Payload["resolved_id"])),
			strings.TrimSpace(fmt.Sprint(artifact["evidence_kind"])),
			strings.TrimSpace(fmt.Sprint(artifact["path"])),
			strings.TrimSpace(fmt.Sprint(artifact["matched_value"])),
		}
		hash := sha1.Sum([]byte(strings.Join(parts, "\x00"))) // #nosec G401 -- stable fixture identity, not cryptography.
		ids = append(ids, "evidence-artifact:"+hex.EncodeToString(hash[:8]))
	}
	sort.Strings(ids)
	return ids
}

func repoDependencyIfaExpectedEdges(rows []reducer.SharedProjectionIntentRow) []string {
	edges := make([]string, 0, len(rows))
	for _, row := range rows {
		action := strings.TrimSpace(fmt.Sprint(row.Payload["action"]))
		if action == "retract" || action == "delete" {
			continue
		}
		source := strings.TrimSpace(fmt.Sprint(row.Payload["repo_id"]))
		target := strings.TrimSpace(fmt.Sprint(row.Payload["target_repo_id"]))
		relationshipType := strings.TrimSpace(fmt.Sprint(row.Payload["relationship_type"]))
		edges = append(edges, repoDependencyIfaEdgeKey(source, relationshipType, target))
	}
	sort.Strings(edges)
	return edges
}

func repoDependencyIfaEdgesWithoutSource(edges []string, source string) []string {
	prefix := source + "\x00"
	result := make([]string, 0, len(edges))
	for _, edge := range edges {
		if !strings.HasPrefix(edge, prefix) {
			result = append(result, edge)
		}
	}
	return result
}

func mustRepoDependencyIfaOdu(t *testing.T) ifa.Odu {
	t.Helper()
	odu, ok := ifa.CatalogByName()[repoDependencyIfaOduName]
	if !ok {
		t.Fatalf("Ifa catalog is missing %q", repoDependencyIfaOduName)
	}
	return odu
}

func repoDependencyIfaRows(t *testing.T, odu ifa.Odu) []reducer.SharedProjectionIntentRow {
	t.Helper()
	evidenceBySource := make(map[string][]relationships.EvidenceFact)
	for _, item := range ifa.DiscoveredEvidence(odu) {
		evidenceBySource[item.SourceRepoID] = append(evidenceBySource[item.SourceRepoID], item)
	}

	capture := &repoDependencyIfaIntentCapture{}
	for _, source := range sortedRepoDependencyIfaSources(evidenceBySource) {
		scopeID, generationID := repoDependencyIfaCoordinates(t, odu, source)
		evidence := evidenceBySource[source]
		evidence = append(evidence, evidence...)
		handler := reducer.CrossRepoRelationshipHandler{
			EvidenceLoader: repoDependencyIfaEvidenceLoader{evidence: evidence},
			IntentWriter:   capture,
		}
		if _, err := handler.Resolve(context.Background(), scopeID, generationID); err != nil {
			t.Fatalf("resolve Odù source %q: %v", source, err)
		}
	}
	if got, want := len(capture.rows), 8; got != want {
		t.Fatalf("Odù repo-dependency intents=%d, want %d", got, want)
	}
	return append([]reducer.SharedProjectionIntentRow(nil), capture.rows...)
}

func repoDependencyIfaRetractRows(
	t *testing.T,
	odu ifa.Odu,
	source string,
) []reducer.SharedProjectionIntentRow {
	t.Helper()
	scopeID, generationID := repoDependencyIfaCoordinates(t, odu, source)
	capture := &repoDependencyIfaIntentCapture{}
	handler := reducer.CrossRepoRelationshipHandler{
		EvidenceLoader: repoDependencyIfaEvidenceLoader{},
		IntentWriter:   capture,
	}
	if _, err := handler.Resolve(context.Background(), scopeID, generationID+":retract"); err != nil {
		t.Fatalf("resolve retract Odù source %q: %v", source, err)
	}
	if got, want := len(capture.rows), 1; got != want {
		t.Fatalf("retract intents=%d, want %d", got, want)
	}
	return append([]reducer.SharedProjectionIntentRow(nil), capture.rows...)
}

func repoDependencyIfaCoordinates(t *testing.T, odu ifa.Odu, source string) (string, string) {
	t.Helper()
	for _, fact := range odu.Facts {
		if fact.FactKind != "repository" || strings.TrimSpace(fmt.Sprint(fact.Payload["repo_id"])) != source {
			continue
		}
		return fact.ScopeID, fact.GenerationID
	}
	t.Fatalf("source repository %q has no Odù coordinates", source)
	return "", ""
}

func sortedRepoDependencyIfaSources(
	bySource map[string][]relationships.EvidenceFact,
) []string {
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

type repoDependencyIfaStore struct {
	mu        sync.Mutex
	rows      map[string]reducer.SharedProjectionIntentRow
	completed map[string]struct{}
	accepted  map[reducer.SharedProjectionAcceptanceKey]string
	leases    map[string]string
	onDrained func()
}

func newRepoDependencyIfaStore(rows []reducer.SharedProjectionIntentRow) *repoDependencyIfaStore {
	store := &repoDependencyIfaStore{
		rows:      make(map[string]reducer.SharedProjectionIntentRow, len(rows)),
		completed: make(map[string]struct{}, len(rows)),
		accepted:  make(map[reducer.SharedProjectionAcceptanceKey]string, len(rows)),
		leases:    make(map[string]string),
	}
	store.upsert(rows)
	store.accept(rows)
	return store
}

func (s *repoDependencyIfaStore) upsert(rows []reducer.SharedProjectionIntentRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		s.rows[row.IntentID] = row
	}
}

func (s *repoDependencyIfaStore) accept(rows []reducer.SharedProjectionIntentRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		key, ok := row.AcceptanceKey()
		if ok {
			s.accepted[key] = row.GenerationID
		}
	}
}

func (s *repoDependencyIfaStore) acceptedGeneration(
	key reducer.SharedProjectionAcceptanceKey,
) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	generationID, ok := s.accepted[key]
	return generationID, ok
}

func (s *repoDependencyIfaStore) ListPendingDomainIntents(
	_ context.Context,
	domain string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]reducer.SharedProjectionIntentRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.ProjectionDomain != domain {
			continue
		}
		if _, done := s.completed[row.IntentID]; done {
			continue
		}
		rows = append(rows, row)
	}
	return sortAndLimitRepoDependencyIfaRows(rows, limit), nil
}

func (s *repoDependencyIfaStore) ListAcceptanceUnitDomainIntents(
	_ context.Context,
	acceptanceUnitID string,
	domain string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]reducer.SharedProjectionIntentRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.ProjectionDomain == domain && row.AcceptanceUnitID == acceptanceUnitID {
			rows = append(rows, row)
		}
	}
	return sortAndLimitRepoDependencyIfaRows(rows, limit), nil
}

func sortAndLimitRepoDependencyIfaRows(
	rows []reducer.SharedProjectionIntentRow,
	limit int,
) []reducer.SharedProjectionIntentRow {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].IntentID < rows[j].IntentID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func (s *repoDependencyIfaStore) MarkIntentsCompleted(
	_ context.Context,
	intentIDs []string,
	_ time.Time,
) error {
	s.mu.Lock()
	for _, intentID := range intentIDs {
		s.completed[intentID] = struct{}{}
	}
	drained := s.pendingCountLocked() == 0
	onDrained := s.onDrained
	s.mu.Unlock()
	if drained && onDrained != nil {
		onDrained()
	}
	return nil
}

func (s *repoDependencyIfaStore) ClaimPartitionLease(
	_ context.Context,
	domain string,
	partitionID int,
	partitionCount int,
	owner string,
	_ time.Duration,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d/%d", domain, partitionID, partitionCount)
	current, held := s.leases[key]
	if held && current != owner {
		return false, nil
	}
	s.leases[key] = owner
	return true, nil
}

func (s *repoDependencyIfaStore) ReleasePartitionLease(
	_ context.Context,
	domain string,
	partitionID int,
	partitionCount int,
	owner string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d/%d", domain, partitionID, partitionCount)
	if s.leases[key] == owner {
		delete(s.leases, key)
	}
	return nil
}

func (s *repoDependencyIfaStore) pendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingCountLocked()
}

func (s *repoDependencyIfaStore) pendingCountLocked() int {
	pending := 0
	for intentID := range s.rows {
		if _, done := s.completed[intentID]; !done {
			pending++
		}
	}
	return pending
}

func (s *repoDependencyIfaStore) setOnDrained(onDrained func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDrained = onDrained
}

func runRepoDependencyIfaRunner(
	parent context.Context,
	t *testing.T,
	store *repoDependencyIfaStore,
	writer reducer.SharedProjectionEdgeWriter,
	replayer reducer.WorkloadMaterializationReplayer,
	workers int,
) {
	t.Helper()
	if store.pendingCount() == 0 {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	store.setOnDrained(cancel)
	runner := reducer.RepoDependencyProjectionRunner{
		IntentReader:                    store,
		LeaseManager:                    store,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		AcceptedGen:                     store.acceptedGeneration,
		Config: reducer.RepoDependencyProjectionRunnerConfig{
			Workers:      workers,
			PollInterval: time.Millisecond,
			LeaseTTL:     time.Second,
			BatchLimit:   100,
		},
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("workers=%d RepoDependencyProjectionRunner.Run() error = %v", workers, err)
	}
	if got := store.pendingCount(); got != 0 {
		t.Fatalf("workers=%d pending intents=%d, want 0", workers, got)
	}
}
