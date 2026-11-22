// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func ownerConflictFixtures() []factIDOwnerConflict {
	return []factIDOwnerConflict{
		{FactID: "f1", FactKind: "k", IncomingScopeID: "b", IncomingGenerationID: "gb", StoredScopeID: "a", StoredGenerationID: "ga"},
		{FactID: "f2", FactKind: "k", IncomingScopeID: "a", IncomingGenerationID: "g2", StoredScopeID: "a", StoredGenerationID: "g1"},
		{FactID: "f3", FactKind: "k", IncomingScopeID: "b", IncomingGenerationID: "gb", StoredScopeID: "a", StoredGenerationID: "ga"},
	}
}

// TestFactIDOwnerConflictErrorSplitsReasonsAndBoundsSamples pins the typed
// error: reason scope when the stored scope differs, generation when only the
// generation differs, a bounded sample, and a message free of payload.
func TestFactIDOwnerConflictErrorSplitsReasonsAndBoundsSamples(t *testing.T) {
	t.Parallel()

	err := newFactIDOwnerConflictError("b", "gb", ownerConflictFixtures())
	if err.Count != 3 || err.ByReason["scope"] != 2 || err.ByReason["generation"] != 1 || len(err.ByReason) != 2 {
		t.Fatalf("error = %+v, want 3 conflicts split scope=2 generation=1", err)
	}
	var target *FactIDOwnerConflictError
	if !errors.As(fmt.Errorf("persist: %w", err), &target) || target != err {
		t.Fatal("errors.As does not recover the typed error through a wrap")
	}
	if !strings.Contains(err.Error(), "fact_id_owner_conflict") || !strings.Contains(err.Error(), `"f1"`) {
		t.Fatalf("Error() = %q, want the closed reason and the first fact id", err.Error())
	}

	many := make([]factIDOwnerConflict, 0, 100)
	for i := 0; i < 100; i++ {
		many = append(many, factIDOwnerConflict{FactID: fmt.Sprintf("f%d", i), IncomingScopeID: "b", StoredScopeID: "a"})
	}
	bounded := newFactIDOwnerConflictError("b", "gb", many)
	if bounded.Count != 100 || len(bounded.Samples) != factOwnerConflictSampleCap || factOwnerConflictSampleCap != 10 {
		t.Fatalf("bounded = %d conflicts, %d samples; want 100 and 10", bounded.Count, len(bounded.Samples))
	}
}

// TestRecordFactIDOwnerConflictCountsByReasonAndLogsIdsOnly pins the #11203
// operator signal: one counter increment per conflicting fact split by the
// closed reason set, and exactly one ERROR log carrying the scope and
// generation ids, the count, the samples with incoming and stored owners, and
// no payload.
func TestRecordFactIDOwnerConflictCountsByReasonAndLogsIdsOnly(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	recordFactIDOwnerConflict(context.Background(), instruments, logger, "git",
		newFactIDOwnerConflictError("b", "gb", ownerConflictFixtures()))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_fact_id_scope_conflicts_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value("reason")
				got[reason.AsString()] += point.Value
			}
		}
	}
	if got["scope"] != 2 || got["generation"] != 1 || len(got) != 2 {
		t.Fatalf("counter by reason = %v, want scope=2 generation=1", got)
	}

	line := logs.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want exactly one log line per refusal, got %q", line)
	}
	for _, want := range []string{
		`"msg":"fact_id_owner_conflict"`, `"level":"ERROR"`, `"conflict_count":3`, `"sampled":3`,
		`"fact_id":"f1"`, `"fact_kind":"k"`, `"incoming_scope_id":"b"`, `"incoming_generation_id":"gb"`,
		`"stored_scope_id":"a"`, `"stored_generation_id":"ga"`, `"failure_class":"fact_id_owner_conflict"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log missing %s: %s", want, line)
		}
	}
	if strings.Contains(line, "payload") {
		t.Errorf("log carries a payload: %s", line)
	}
	recordFactIDOwnerConflict(context.Background(), instruments, logger, "git", nil)
}

// TestUpsertFactBatchHasNoProductionCaller keeps the plain upsertFactBatch
// path honest. It has no RETURNING, so the owner guard protects an owner row
// there but cannot report a skipped foreign write (#11203). Nothing in
// production may reach it: FactStore.UpsertFacts, upsertFacts, and
// upsertFactBatch are called only from the storage/postgres files that define
// them. A new production caller must use upsertFactBatchClassified (as
// upsertStreamingFacts does) and update this allowlist.
func TestUpsertFactBatchHasNoProductionCaller(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))) // .../go
	allowed := map[string]bool{
		"internal/storage/postgres/facts.go":           true,
		"internal/storage/postgres/facts_streaming.go": true,
	}
	needles := []string{"UpsertFacts(", "upsertFacts(", "upsertFactBatch("}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		for _, needle := range needles {
			if strings.Contains(string(body), needle) && !allowed[rel] {
				t.Errorf("%s calls %s outside the files that define it; the plain upsert path cannot report an owner conflict", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if scanned < 1000 {
		t.Fatalf("scanned %d production files, want at least 1000: the scan lost the tree", scanned)
	}
}

// TestFactOwnerGuardPreStatementIsDerived guards the derivation the benchmark
// and EXPLAIN rely on: removing the clause must change the statement.
func TestFactOwnerGuardPreStatementIsDerived(t *testing.T) {
	t.Parallel()
	if ownerGuardPreSuffix() == upsertFactBatchSuffixReturningFactID {
		t.Fatal("factOwnerGuardClause is not part of upsertFactBatchSuffix; the pre-change derivation is a no-op")
	}
	if strings.Contains(ownerGuardPreSuffix(), "fact_records.scope_id = EXCLUDED.scope_id") {
		t.Fatal("the derived pre-change statement still carries the owner guard")
	}
}
