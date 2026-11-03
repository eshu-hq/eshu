// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestInboundSourcesFailLoudlyOnBudget is the #9781 pass-level budget gate: when
// the store sets an inbound byte budget, the closure's anchor-scoped inbound
// load fails with ErrRelationshipFactLoadBudgetExceeded instead of holding the
// whole corpus, and the stats still report the row that crossed the budget so
// the inbound metric is recorded on the failure path.
func TestInboundSourcesFailLoudlyOnBudget(t *testing.T) {
	t.Parallel()

	catalog := parityCatalog()
	owed := parityOwed()
	owedRepos := make(map[string]backfill.RepositoryGenerationIdentity, len(owed))
	for repoID := range owed {
		scopeID := "git:" + repoID
		owedRepos[repoID] = backfill.RepositoryGenerationIdentity{
			RepoID: repoID, ScopeID: scopeID, GenerationID: parityGeneration(scopeID),
		}
	}

	store := Store{
		Backfill:      backfill.Store{DB: &parityQueryer{t: t, corpus: parityCorpus()}},
		InboundBudget: backfill.RelationshipFactLoadBudget{MaxBytes: 1},
	}
	load := make(map[backfill.ScopeGenerationPartition]struct{})
	sourceRepoIDs := make(map[string]struct{})
	stats, err := store.addInboundTargetedMaintenanceSources(
		context.Background(), catalog, owedRepos, load, sourceRepoIDs,
	)
	if !errors.Is(err, backfill.ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
	if stats.Rows == 0 || stats.Bytes == 0 {
		t.Fatalf("stats = %+v, want the crossing row reported", stats)
	}
}

// TestRecordTargetedMaintenanceInboundMetrics pins that the pass records the
// inbound rows and bytes counters once per pass, from the result fields, so an
// operator reads the load the inbound budget bounds even on a budget failure.
func TestRecordTargetedMaintenanceInboundMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("tgt9781-inbound"),
	)
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	result := TargetedMaintenanceResult{InboundRows: 42, InboundBytes: 12345}
	recordTargetedMaintenance(
		context.Background(), trace.SpanFromContext(context.Background()),
		instruments, time.Now(), nil, result, nil,
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := int64CounterValue(rm, "eshu_dp_deferred_backfill_targeted_inbound_rows_total"); got != 42 {
		t.Fatalf("inbound rows = %d, want 42", got)
	}
	if got := int64CounterValue(rm, "eshu_dp_deferred_backfill_targeted_inbound_bytes_total"); got != 12345 {
		t.Fatalf("inbound bytes = %d, want 12345", got)
	}
}

// int64CounterValue returns the accumulated value of one Int64Counter by name.
func int64CounterValue(rm metricdata.ResourceMetrics, name string) int64 {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				return 0
			}
			var total int64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	return 0
}

// evidenceRowsTouchingRepo projects evidence onto the rows whose source or
// target is repoID, in the same stable form touchingOwed uses.
func evidenceRowsTouchingRepo(evidence []relationships.EvidenceFact, repoID string) []string {
	rows := make([]string, 0, len(evidence))
	for _, fact := range evidence {
		if fact.SourceRepoID != repoID && fact.TargetRepoID != repoID {
			continue
		}
		rows = append(rows, strings.Join([]string{
			string(fact.EvidenceKind), string(fact.RelationshipType),
			fact.SourceRepoID, fact.TargetRepoID, fact.TargetEntityID,
			detailString(fact, "path"), detailString(fact, "matched_value"),
		}, "|"))
	}
	sort.Strings(rows)
	return rows
}

// TestAnchorScopedLoadParityPerRepository is the #9781 evidence-parity diff,
// per owed repository: DiscoverEvidence over the full corpus and over the
// narrowed anchor-scoped load must emit identical evidence for every owed
// repository, so the threshold is ZERO lost rows per repository. It logs the
// per-repository table (full rows, narrowed rows, lost rows) for the handoff.
func TestAnchorScopedLoadParityPerRepository(t *testing.T) {
	t.Parallel()

	catalog := parityCatalog()
	owed := parityOwed()
	full := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(parityEnvelopes(t, parityCorpus()), catalog),
	)

	queryer := &parityQueryer{t: t, corpus: parityCorpus()}
	narrowed, _, err := backfill.LoadAnchorScopedRelationshipFactsBudget(
		context.Background(), queryer, backfill.RepositoryScopedCatalog(catalog, owed), catalog,
		backfill.RelationshipFactLoadBudget{},
	)
	if err != nil {
		t.Fatalf("LoadAnchorScopedRelationshipFactsBudget: %v", err)
	}
	narrowedEvidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(narrowed, catalog),
	)

	t.Logf("repository | full rows | narrowed rows | lost rows")
	totalLost := 0
	for _, repoID := range sortedKeys(owed) {
		fullRows := evidenceRowsTouchingRepo(full, repoID)
		narrowedRows := evidenceRowsTouchingRepo(narrowedEvidence, repoID)
		lost := missingRows(fullRows, narrowedRows)
		totalLost += len(lost)
		t.Logf("%s | %d | %d | %d", repoID, len(fullRows), len(narrowedRows), len(lost))
		if len(lost) != 0 {
			t.Errorf("repo %s lost %d evidence rows: %v", repoID, len(lost), lost)
		}
	}
	if totalLost != 0 {
		t.Fatalf("narrowed load lost %d evidence rows across owed repositories", totalLost)
	}
	t.Logf("total owed repositories=%d full_evidence_rows=%d narrowed_evidence_rows=%d lost=%d",
		len(owed), len(full), len(narrowedEvidence), totalLost)
}
