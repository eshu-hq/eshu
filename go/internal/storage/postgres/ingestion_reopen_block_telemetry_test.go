// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reopenBlockSpanName is the per-domain reopen block span of the deferred
// maintenance pass (#8082).
const reopenBlockSpanName = "relationship.backfill.reopen"

// TestRunDeferredRelationshipMaintenanceEmitsOneReopenBlockSpanPerDomain pins the
// ninth block of the maintenance pass: every reopen domain call, the two
// relationship domains and each cross-scope correlation domain, ends exactly one
// relationship.backfill.reopen span carrying its domain, reopened count, and
// skipped_by_memo count. The reopen runs after relationship.backfill_deferred
// ends, so they are siblings of it under the caller's span, not its children.
func TestRunDeferredRelationshipMaintenanceEmitsOneReopenBlockSpanPerDomain(t *testing.T) {
	t.Parallel()

	recorder, tracer := recordingTracer()
	runHermeticDeferredMaintenanceTraced(t, tracer)

	findEndedSpan(t, recorder.Ended(), backfillDeferredSpanName)

	var gotDomains []string
	for _, span := range recorder.Ended() {
		if span.Name() != reopenBlockSpanName {
			continue
		}
		if span.Status().Code == codes.Error {
			t.Errorf("reopen span status = Error, want unset on success")
		}
		domain := ""
		counts := map[string]bool{}
		for _, attr := range span.Attributes() {
			switch attr.Key {
			case attribute.Key("domain"):
				domain = attr.Value.AsString()
			case attribute.Key("reopened"), attribute.Key("skipped_by_memo"):
				counts[string(attr.Key)] = true
			}
		}
		if domain == "" {
			t.Errorf("reopen span missing domain attribute")
		}
		if !counts["reopened"] || !counts["skipped_by_memo"] {
			t.Errorf("reopen span %q attributes = %v, want reopened and skipped_by_memo", domain, span.Attributes())
		}
		gotDomains = append(gotDomains, domain)
	}

	want := append([]string{"deployment_mapping", "code_import_repo_edge"}, CrossScopeCorrelationReopenDomains()...)
	sort.Strings(want)
	sort.Strings(gotDomains)
	if len(gotDomains) != len(want) {
		t.Fatalf("reopen block spans for domains %v, want one per domain %v", gotDomains, want)
	}
	for i := range want {
		if gotDomains[i] != want[i] {
			t.Fatalf("reopen block span domains = %v, want %v", gotDomains, want)
		}
	}
}

// TestRunDeferredRelationshipMaintenanceRecordsReopenBlockDurationPerDomainCall
// pins the reopen histogram contract: one observation per reopen domain call,
// every one under the single label value block=reopen (the domain split lives
// on the span and log only), and no other label.
func TestRunDeferredRelationshipMaintenanceRecordsReopenBlockDurationPerDomainCall(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	runHermeticDeferredMaintenanceObserved(t, nil, instruments)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var reopenCount uint64
	labels := map[string]bool{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != "eshu_dp_deferred_backfill_block_duration_seconds" {
				continue
			}
			hist := record.Data.(metricdata.Histogram[float64])
			for _, dp := range hist.DataPoints {
				if dp.Attributes.Len() != 1 {
					t.Fatalf("block duration data point has %d attributes, want only block", dp.Attributes.Len())
				}
				block, _ := dp.Attributes.Value("block")
				labels[block.AsString()] = true
				if block.AsString() == "reopen" {
					reopenCount += dp.Count
				}
			}
		}
	}
	want := uint64(2 + len(CrossScopeCorrelationReopenDomains()))
	if reopenCount != want {
		t.Fatalf("block=reopen observations = %d, want %d (one per reopen domain call)", reopenCount, want)
	}
	if !labels["reopen"] {
		t.Fatalf("block labels = %v, want reopen present", labels)
	}
}
