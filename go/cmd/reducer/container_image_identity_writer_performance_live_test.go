// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_head || perf5854_main

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const containerImageIdentityPerfWriterIterations = 3

func TestContainerImageIdentityWriterPerformanceLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_5854_PERF_DSN to run the #5854 writer benchmark")
	}
	variant := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_VARIANT"))
	if variant == "" {
		t.Fatal("set ESHU_5854_PERF_VARIANT to main or head")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	db := openContainerImageIdentityPerfSchema(t, ctx, dsn, variant, "writer_only")
	seedContainerImageIdentityPerfFixture(t, ctx, db, 1, 0)

	counts := &containerImageIdentityPerfStatementCounts{}
	countingDB := containerImageIdentityPerfCountingDB{
		delegate: postgres.SQLDB{DB: db},
		counts:   counts,
	}
	writer := containerImageIdentityPerfWriter(countingDB)
	if writer == nil {
		t.Fatal("build production container-image-identity writer = nil")
	}
	write := containerImageIdentityPerfWriterWrite()
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("warm production writer: %v", err)
	}
	assertContainerImageIdentityPerfAccuracy(
		t,
		ctx,
		db,
		containerImageIdentityPerfWorstCaseRefs,
		containerImageIdentityPerfHeadVariant,
	)
	prepareContainerImageIdentityPerfStats(t, ctx, db)
	counts.reset()
	walBefore := currentContainerImageIdentityPerfWAL(t, ctx, db)

	latencies := make([]time.Duration, 0, containerImageIdentityPerfWriterIterations)
	started := time.Now()
	for iteration := range containerImageIdentityPerfWriterIterations {
		write.EvidenceAsOf = write.EvidenceAsOf.Add(time.Duration(iteration+1) * time.Microsecond)
		runStarted := time.Now()
		if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
			t.Fatalf("measured production writer: %v", err)
		}
		latencies = append(latencies, time.Since(runStarted))
	}
	total := time.Since(started)
	walAfter := currentContainerImageIdentityPerfWAL(t, ctx, db)
	walBytes := containerImageIdentityPerfWALDiff(t, ctx, db, walAfter, walBefore)
	stats := readContainerImageIdentityPerfTableStats(t, ctx, db)
	accuracy := assertContainerImageIdentityPerfAccuracy(
		t,
		ctx,
		db,
		containerImageIdentityPerfWorstCaseRefs,
		containerImageIdentityPerfHeadVariant,
	)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	statementCounts := counts.snapshot()
	iterations := float64(containerImageIdentityPerfWriterIterations)

	result := containerImageIdentityPerfResult{
		Variant:          variant,
		Case:             "writer_only_worst_cardinality",
		References:       containerImageIdentityPerfWorstCaseRefs,
		Iterations:       containerImageIdentityPerfWriterIterations,
		MedianMillis:     durationMillis(latencies[len(latencies)/2]),
		P95Millis:        durationMillis(latencies[len(latencies)-1]),
		ThroughputPerSec: iterations / total.Seconds(),
		QueriesPerOp:     float64(statementCounts.queries) / iterations,
		ExecsPerOp:       float64(statementCounts.execs) / iterations,
		BeginsPerOp:      float64(statementCounts.begins) / iterations,
		CommitsPerOp:     float64(statementCounts.commits) / iterations,
		WALBytesPerOp:    float64(walBytes) / iterations,
		DeadTuples:       stats.dead,
		UpdatedTuples:    stats.updated,
		DeletedTuples:    stats.deleted,
		VisibleRows:      accuracy.visibleRows,
		OutcomeKeyedRows: accuracy.outcomeKeyedRows,
		LogicalChecksum:  accuracy.checksum,
		QueryBreakdown:   statementCounts.queryBreakdown,
		ExecBreakdown:    statementCounts.execBreakdown,
		QueryMillis:      statementCounts.queryMillis,
		ExecMillis:       statementCounts.execMillis,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal writer performance result: %v", err)
	}
	t.Logf("PERF5854 %s", encoded)
}

func containerImageIdentityPerfWriterWrite() reducer.ContainerImageIdentityWrite {
	decisions := make([]reducer.ContainerImageIdentityDecision, 0, containerImageIdentityPerfWorstCaseRefs)
	for reference := 1; reference <= containerImageIdentityPerfWorstCaseRefs; reference++ {
		suffix := fmt.Sprintf("%06d", reference)
		decisions = append(decisions, reducer.ContainerImageIdentityDecision{
			ImageRef:            "registry.example.com/performance/team-api:tag-" + suffix,
			Digest:              "sha256:" + fmt.Sprintf("%064x", reference),
			RepositoryID:        containerImageIdentityPerfRepositoryID,
			SourceRepositoryIDs: []string{containerImageIdentityPerfRepositoryID},
			Outcome:             reducer.ContainerImageIdentityTagResolved,
			Reason:              "synthetic exact writer performance proof",
			CanonicalWrites:     1,
			EvidenceFactIDs:     []string{"tag-observation-5854-performance-" + suffix},
			IdentityStrength:    "tag_resolved",
		})
	}
	return reducer.ContainerImageIdentityWrite{
		IntentID:     "intent-5854-performance",
		ClaimEpoch:   1,
		ScopeID:      containerImageIdentityPerfRepoScope,
		GenerationID: containerImageIdentityPerfRepoGeneration,
		SourceSystem: "git",
		Cause:        "synthetic exact writer performance proof",
		EvidenceAsOf: time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC),
		Decisions:    decisions,
		LegacyFactIDs: []string{
			"reducer_container_image_identity:5854-synthetic-unreachable-legacy",
		},
	}
}
