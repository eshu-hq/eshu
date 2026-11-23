// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestObserveNVDWindowClaimRoutesOutcomes is the observe-level proof for
// #11287: a stamped SDK result driven through observeNVDWindowClaim plus
// the commit/refused hooks counts under the stamped transport label, the
// unknown fold holds without a stamp, and the excluded classes stay
// uncounted. It fails if the three wiring lines in source.go and
// claim_outcome.go are removed.
func TestObserveNVDWindowClaimRoutesOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	manifest := testManifest()
	manifest.Spec.EmittedFacts = append(manifest.Spec.EmittedFacts, component.FactFamily{
		Kind:             "vulnerability.source_snapshot",
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []string{"observed"},
	})
	grants := []component.ProducerGrant{{
		ProducerID:     manifest.Metadata.ID,
		Version:        manifest.Metadata.Version,
		Kind:           "vulnerability.source_snapshot",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "repo",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}}
	// ConfigHandle must match the hardcoded handle in testSDKClaim, or the
	// NextClaimed claim-identity check rejects the runner result.
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Instruments:         inst,
		Grants:              grants,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	ctx := context.Background()
	scopeID := "vuln-intel://nvd/_window/1794438600263000-1794460200263000"

	nvdItem := func(workID, generation string) workflow.WorkItem {
		item := testWorkItem()
		item.WorkItemID = workID
		item.GenerationID = generation
		item.ScopeID = scopeID
		return item
	}
	nvdFacts := func(item workflow.WorkItem, transport any) []sdkcollector.Fact {
		fact := testSDKFact(item)
		fact.Kind = "vulnerability.source_snapshot"
		fact.StableKey = "nvd-window"
		fact.SourceRef.FactKey = "nvd-window"
		payload := map[string]any{
			"nvd_window_advance_to_unix_us": 1794438600263000,
			"nvd_window_head_unix_us":       1794460200263000,
		}
		if transport != nil {
			payload["nvd_window_transport"] = transport
		}
		fact.Payload = payload
		return []sdkcollector.Fact{fact}
	}
	// driveCompleted runs one completed NVD window claim through the
	// production NextClaimed path (which stashes the observation) plus the
	// commit/refused hook, so the test covers the wiring, not just the
	// record helper.
	driveCompleted := func(item workflow.WorkItem, transport any, advanced bool, commit bool) {
		t.Helper()
		source.runner = &recordingRunner{result: completeResult(item, nvdFacts(item, transport)...)}
		if advanced {
			// The advance decision rides the collected generation; the
			// runner result path below leaves it unset (no StateWriter),
			// so advanced commits are pinned separately below.
			t.Fatalf("advanced stash requires a StateWriter the test runner cannot provide")
		}
		if _, _, err := source.NextClaimed(ctx, item); err != nil {
			t.Fatalf("NextClaimed() error = %v", err)
		}
		if commit {
			if err := source.ObserveClaimedGenerationCommitted(ctx, item); err != nil {
				t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
			}
			return
		}
		if err := source.ObserveClaimedGenerationRefused(ctx, item, collector.CommitRefusal{}); err != nil {
			t.Fatalf("ObserveClaimedGenerationRefused() error = %v", err)
		}
	}

	// Feed completed, committed.
	feedItem := nvdItem("work-nvd-feed-1", "generation-nvd-1")
	driveCompleted(feedItem, "feed", false, true)
	// API completed, refused.
	apiItem := nvdItem("work-nvd-api-1", "generation-nvd-1")
	driveCompleted(apiItem, "api", false, false)
	// No stamp reads unknown.
	unknownItem := nvdItem("work-nvd-unknown-1", "generation-nvd-1")
	driveCompleted(unknownItem, nil, false, true)
	// Satellite-signaled retryable failure with a feed stamp.
	retryItem := nvdItem("work-nvd-retry-1", "generation-nvd-1")
	source.observeNVDWindowClaim(ctx, retryItem, testObservedAt(), collector.CollectedGeneration{},
		completeResult(retryItem, nvdFacts(retryItem, "feed")...), false,
		extensionFailure{class: FailureClassSourceStateRecord})
	// Satellite-signaled terminal failure with an API stamp.
	terminalItem := nvdItem("work-nvd-terminal-1", "generation-nvd-1")
	source.observeNVDWindowClaim(ctx, terminalItem, testObservedAt(), collector.CollectedGeneration{},
		completeResult(terminalItem, nvdFacts(terminalItem, "api")...), false,
		extensionFailure{class: "test_terminal_for_unit_test", terminal: true})
	// Excluded host classes stay uncounted even with a stamp.
	for i, class := range []string{
		FailureClassInvalidClaim, FailureClassIdentityMismatch,
		FailureClassInvalidResult, FailureClassLaunchFailure,
		FailureClassStatusRecord,
	} {
		item := nvdItem(fmt.Sprintf("work-nvd-excluded-%d", i), "generation-nvd-1")
		source.observeNVDWindowClaim(ctx, item, testObservedAt(), collector.CollectedGeneration{},
			completeResult(item, nvdFacts(item, "feed")...), false,
			extensionFailure{class: class})
	}
	// Generation mismatch drops the stash.
	staleItem := nvdItem("work-nvd-stale-1", "generation-nvd-A")
	source.runner = &recordingRunner{result: completeResult(staleItem, nvdFacts(staleItem, "feed")...)}
	if _, _, err := source.NextClaimed(ctx, staleItem); err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	movedItem := nvdItem("work-nvd-stale-1", "generation-nvd-B")
	if err := source.ObserveClaimedGenerationCommitted(ctx, movedItem); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
	}

	got := map[string]int64{}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_vulnerability_nvd_window_claims_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("claims data = %#v, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				transport, _ := point.Attributes.Value("transport")
				outcome, _ := point.Attributes.Value("outcome")
				got[transport.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{
		"feed/committed":        1,
		"api/refused":           1,
		"unknown/committed":     1,
		"feed/failed_retryable": 1,
		"api/failed_terminal":   1,
	}
	if len(got) != len(want) {
		t.Fatalf("claims series = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("claims series = %v, want %v", got, want)
		}
	}
}

// TestPendingNVDWindowClaimsEvictsOldestAtCap pins the stash bound
// (eshu-hq/eshu#11287 review): at maxPendingNVDWindowClaims entries the
// stash evicts the entry with the oldest attempt time, so the map never
// grows without bound and the evicted window is left uncounted, never
// miscounted.
func TestPendingNVDWindowClaimsEvictsOldestAtCap(t *testing.T) {
	t.Parallel()

	base := testObservedAt()
	stash := func(p *pendingNVDWindowClaims, i int, at time.Time) {
		item := testWorkItem()
		item.WorkItemID = fmt.Sprintf("work-nvd-cap-%d", i)
		item.GenerationID = "generation-cap"
		p.stash(item, nvdWindowPending{generationID: "generation-cap", attemptedAt: at})
	}

	p := newPendingNVDWindowClaims()
	for i := 0; i < maxPendingNVDWindowClaims; i++ {
		stash(p, i, base.Add(time.Duration(i)*time.Second))
	}
	if got := len(p.rows); got != maxPendingNVDWindowClaims {
		t.Fatalf("len(rows) after fill = %d, want %d", got, maxPendingNVDWindowClaims)
	}

	stash(p, maxPendingNVDWindowClaims, base.Add(time.Duration(maxPendingNVDWindowClaims)*time.Second))
	if got := len(p.rows); got != maxPendingNVDWindowClaims {
		t.Fatalf("len(rows) after overflow = %d, want cap %d", got, maxPendingNVDWindowClaims)
	}
	oldest := testWorkItem()
	oldest.WorkItemID = "work-nvd-cap-0"
	oldest.GenerationID = "generation-cap"
	if _, ok := p.take(oldest); ok {
		t.Fatalf("oldest entry survived the cap eviction")
	}
	newest := testWorkItem()
	newest.WorkItemID = fmt.Sprintf("work-nvd-cap-%d", maxPendingNVDWindowClaims)
	newest.GenerationID = "generation-cap"
	if _, ok := p.take(newest); !ok {
		t.Fatalf("newest entry missing after the cap eviction")
	}
}

// TestNVDWindowTransportReadsStampedPayload pins the satellite-to-host
// transport contract (eshu-hq/eshu#11287): the satellite stamps
// nvd_window_transport into the source snapshot payload, the host folds it
// onto the closed transport set, and an absent or non-string stamp reads as
// unknown, never as feed.
func TestNVDWindowTransportReadsStampedPayload(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "feed", payload: map[string]any{nvdWindowTransportPayloadKey: "feed"}, want: telemetry.NVDTransportFeed},
		{name: "api", payload: map[string]any{nvdWindowTransportPayloadKey: "api"}, want: telemetry.NVDTransportAPI},
		{name: "absent reads unknown", payload: map[string]any{}, want: telemetry.NVDTransportUnknown},
		{name: "non-string reads unknown", payload: map[string]any{nvdWindowTransportPayloadKey: 7}, want: telemetry.NVDTransportUnknown},
		{name: "case variant reads unknown", payload: map[string]any{nvdWindowTransportPayloadKey: "FEED"}, want: telemetry.NVDTransportUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nvdWindowTransport(tc.payload); got != tc.want {
				t.Fatalf("nvdWindowTransport(%v) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

// TestNVDWindowClaimCountsTransportsSeparately is the failing-first proof
// for #11287: a feed-served window and an API-served window count under
// different transport labels through the production host emitter.
func TestNVDWindowClaimCountsTransportsSeparately(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-nvd-window",
		Config:              map[string]any{},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Instruments:         inst,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	ctx := context.Background()
	source.recordNVDWindowClaim(ctx, telemetry.NVDTransportFeed, telemetry.NVDWindowClaimCommitted, 12.5, true)
	source.recordNVDWindowClaim(ctx, telemetry.NVDTransportAPI, telemetry.NVDWindowClaimCommitted, 7.5, false)

	byTransport := map[string]int64{}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_vulnerability_nvd_window_claims_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("claims data = %#v, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				transport, _ := point.Attributes.Value("transport")
				byTransport[transport.AsString()] += point.Value
			}
		}
	}
	if byTransport["feed"] != 1 || byTransport["api"] != 1 {
		t.Fatalf("claims by transport = %v, want exactly one feed and one api series", byTransport)
	}
}
