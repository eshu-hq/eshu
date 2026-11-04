// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9971 disposable PostgreSQL proof of per-window ownership on the real commit path: window 2 commits under its own scope without superseding window 1 (both CVEs stay visible through the shipped active_facts fence), a refused retry and a duplicate skip leave the cursor exactly where the store left it and never run the writer, and the cursor only advances through the commit-transaction compare-and-advance; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// nvdWindowScopeForTest mints the deterministic per-window scope id the
// coordinator plans from: vuln-intel://nvd/_window/<start>-<end> with UTC
// bounds as Unix microseconds. It mirrors vulnruntime.NVDWindowScopeID,
// whose round-trip test pins the grammar; the bounds here are already
// UTC-millisecond aligned.
func nvdWindowScopeForTest(start, end time.Time) string {
	return fmt.Sprintf("vuln-intel://nvd/_window/%d-%d", start.UnixMicro(), end.UnixMicro())
}

// nvdWindowOwnershipProof is one live test's store, cursor store, and clock
// for the #9971 per-window ownership cells. The tests never run in
// parallel: refusal and skip counters are process-global.
type nvdWindowOwnershipProof struct {
	t           *testing.T
	database    *sql.DB
	store       IngestionStore
	cursorStore vulnerabilitystore.NVDWindowCursorStore
	clock       time.Time
	instance    string
}

func openNVDWindowOwnershipProof(t *testing.T) *nvdWindowOwnershipProof {
	t.Helper()
	dsn := ingestionTxLockSplitProofDSN(t)
	database, _ := openIngestionTxLockSplitProofSchema(t, dsn)
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("nvd-window-ownership-proof"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	store := NewIngestionStore(SQLDB{DB: database})
	clock := time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	store.Instruments = inst
	// The claim plane is a fake pinner that approves every pin: the fence
	// session exists so the commit skips the heartbeat path (whose trigger
	// needs a public-only scanner table absent from isolated schemas), and
	// the writer still runs in the real commit transaction.
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: &claimFenceFakePinner{}, PinBackoff: time.Microsecond,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
	}
	return &nvdWindowOwnershipProof{
		t:           t,
		database:    database,
		store:       store,
		cursorStore: vulnerabilitystore.NewNVDWindowCursorStore(SQLDB{DB: database}),
		clock:       clock,
		instance:    "nvd-rolling",
	}
}

// commitWindow commits one window generation with a recording writer around
// the real cursor compare-and-advance. It returns whether the writer ran:
// a refused or skipped generation must never run it. Findings ride reducer
// findings (the winners fence input); collectorRows adds vulnerability.cve
// facts, which the successor guard sizes on (reducer rows are excluded).
func (p *nvdWindowOwnershipProof) commitWindow(
	scopeID, generationID, hint string, trusted bool, cves []string, collectorRows int, advanceTo, head int64,
) (writerCalled bool, err error) {
	p.t.Helper()
	p.clock = p.clock.Add(time.Minute)
	now := p.clock
	scopeValue := scope.IngestionScope{
		ScopeID: scopeID, SourceSystem: "vulnerability_intelligence",
		ScopeKind: scope.KindVulnerabilityIntelligence, CollectorKind: scope.CollectorVulnerabilityIntelligence,
		PartitionKey: "nvd",
	}
	generation := scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: scopeID,
		ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: hint, TrustedDeletion: trusted,
	}
	mutation := workflow.ClaimMutation{
		WorkItemID: "work-" + generationID, ClaimID: "claim-" + generationID,
		FencingToken: 7, OwnerID: "collector-nvd-rolling", ObservedAt: now, LeaseDuration: time.Minute,
		ScopeID: scopeID, SourceRunID: "run-" + generationID, GenerationID: generationID,
	}
	stream := make(chan facts.Envelope, len(cves)+collectorRows)
	for i := 0; i < collectorRows; i++ {
		cve := fmt.Sprintf("CVE-2026-%04d", i)
		stream <- facts.Envelope{
			FactID: fmt.Sprintf("%s-collector-%04d", generationID, i), ScopeID: scopeID, GenerationID: generationID,
			FactKind:      chain.VulnerabilityCVEFactKind,
			StableFactKey: "cve-collector:" + cve, ObservedAt: now,
			Payload: map[string]any{"cve_id": cve},
		}
	}
	for _, cve := range cves {
		stream <- facts.Envelope{
			FactID: fmt.Sprintf("%s-%s", generationID, cve), ScopeID: scopeID, GenerationID: generationID,
			FactKind:      facts.ReducerSupplyChainImpactFindingFactKind,
			StableFactKey: "cve:" + cve, ObservedAt: now,
			Payload: map[string]any{"cve_id": cve},
		}
	}
	close(stream)
	writer := func(ctx context.Context, exec collector.CommitExecutor) error {
		writerCalled = true
		return vulnerabilitystore.NVDWindowCursorWriter(p.instance, advanceTo, head, now)(ctx, exec)
	}
	err = p.store.CommitClaimedScopeGenerationWithState(context.Background(), mutation, scopeValue, generation, stream, nil, writer)
	return writerCalled, err
}

// project drains the projector queue the way the projector would, which
// activates the committed generations so the read fence sees them.
func (p *nvdWindowOwnershipProof) project() {
	p.t.Helper()
	queue := NewProjectorQueue(SQLDB{DB: p.database}, "nvd-window-ownership-proof", time.Minute)
	for range 10 {
		work, ok, err := queue.Claim(context.Background())
		if err != nil {
			p.t.Fatalf("claim projector work: %v", err)
		}
		if !ok {
			return
		}
		if err := queue.Ack(context.Background(), work, projectorruntime.Result{}); err != nil {
			p.t.Fatalf("ack %s: %v", work.Generation.GenerationID, err)
		}
	}
	p.t.Fatal("projector queue did not drain in 10 claims")
}

func (p *nvdWindowOwnershipProof) readCursor() (cursor int64, set bool) {
	p.t.Helper()
	cursor, set, _, err := p.cursorStore.ReadNVDWindowCursor(context.Background(), p.instance)
	if err != nil {
		p.t.Fatalf("read cursor: %v", err)
	}
	return cursor, set
}

// visibleWindowCVEs reads the shipped active_facts fence verbatim (the
// winners select prefix) with a read-only tail, proving what the query
// surface sees after the commits.
func (p *nvdWindowOwnershipProof) visibleWindowCVEs() []string {
	p.t.Helper()
	rows, err := p.database.QueryContext(context.Background(),
		supplyChainImpactWinnerActiveFactsSQL+`,
		visible AS (
		    SELECT DISTINCT payload->>'cve_id' AS cve FROM active_facts WHERE NULLIF(payload->>'cve_id', '') IS NOT NULL
		)
		SELECT cve FROM visible ORDER BY 1`)
	if err != nil {
		p.t.Fatalf("read visible CVEs: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var cves []string
	for rows.Next() {
		var cve string
		if err := rows.Scan(&cve); err != nil {
			p.t.Fatalf("scan visible CVE: %v", err)
		}
		cves = append(cves, cve)
	}
	if err := rows.Err(); err != nil {
		p.t.Fatalf("iterate visible CVEs: %v", err)
	}
	return cves
}

func (p *nvdWindowOwnershipProof) generationCount(scopeID string) int {
	p.t.Helper()
	var n int
	if err := p.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM scope_generations WHERE scope_id = $1`, scopeID).Scan(&n); err != nil {
		p.t.Fatalf("count generations: %v", err)
	}
	return n
}

// TestNVDWindowOwnershipAccumulatesAcrossWindowsLive is the #9971
// criterion-1 cell on the real commit path: window 2 commits under its own
// scope without superseding window 1, so both CVEs stay visible through the
// shipped active_facts fence, and the cursor advances once per window.
func TestNVDWindowOwnershipAccumulatesAcrossWindowsLive(t *testing.T) {
	p := openNVDWindowOwnershipProof(t)
	window1Start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	window1End := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	window2End := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	window1 := nvdWindowScopeForTest(window1Start, window1End)
	window2 := nvdWindowScopeForTest(window1End, window2End)

	if called, err := p.commitWindow(window1, "nvd-gen-w1", "hint-w1", false,
		[]string{"CVE-A"}, 0, window1End.UnixMicro(), window1End.UnixMicro()); err != nil || !called {
		t.Fatalf("commit window 1 = (writer %v, err %v), want (true, nil)", called, err)
	}
	if called, err := p.commitWindow(window2, "nvd-gen-w2", "hint-w2", false,
		[]string{"CVE-B"}, 0, window2End.UnixMicro(), window2End.UnixMicro()); err != nil || !called {
		t.Fatalf("commit window 2 = (writer %v, err %v), want (true, nil)", called, err)
	}
	// The projector activates the committed generations; only then does
	// the active_facts fence see them, exactly as in production.
	p.project()
	if got, want := p.visibleWindowCVEs(), []string{"CVE-A", "CVE-B"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visible CVEs = %v, want %v: window 2 superseded window 1", got, want)
	}
	if cursor, set := p.readCursor(); !set || cursor != window2End.UnixMicro() {
		t.Fatalf("cursor = (%d, %v), want (%d, set)", cursor, set, window2End.UnixMicro())
	}
}

// TestNVDWindowRefusalAndSkipHoldCursorLive is the #9971 criterion-2 and
// criterion-3a cell on the real commit path: an untrusted under-half retry
// is refused and a byte-identical retry is skipped, and neither runs the
// writer or moves the cursor. The coordinator then re-plans the same window
// from the held cursor (pinned by the derivation test); the store half of
// that contract is the unchanged cursor row asserted here.
func TestNVDWindowRefusalAndSkipHoldCursorLive(t *testing.T) {
	p := openNVDWindowOwnershipProof(t)
	windowStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	window := nvdWindowScopeForTest(windowStart, windowEnd)
	if called, err := p.commitWindow(window, "nvd-gen-w1", "hint-w1", false,
		nil, 4, windowEnd.UnixMicro(), windowEnd.UnixMicro()); err != nil || !called {
		t.Fatalf("commit window = (writer %v, err %v), want (true, nil)", called, err)
	}
	p.project()
	if cursor, _ := p.readCursor(); cursor != windowEnd.UnixMicro() {
		t.Fatalf("cursor = %d, want window end %d", cursor, windowEnd.UnixMicro())
	}

	beforeRefusals := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor)
	if called, err := p.commitWindow(window, "nvd-gen-w1-partial", "hint-w1-partial", false,
		nil, 1, windowEnd.UnixMicro(), windowEnd.UnixMicro()); err != nil || called {
		t.Fatalf("partial retry = (writer %v, err %v), want (false, nil): a refused window must not run the writer", called, err)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor) - beforeRefusals; got != 1 {
		t.Fatalf("refusals delta = %d, want 1", got)
	}
	if p.generationCount(window) != 1 {
		t.Fatalf("generations = %d, want 1: a refused successor must not store", p.generationCount(window))
	}
	if cursor, _ := p.readCursor(); cursor != windowEnd.UnixMicro() {
		t.Fatalf("cursor after refusal = %d, want held %d", cursor, windowEnd.UnixMicro())
	}

	beforeSkips := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonUnchanged)
	if called, err := p.commitWindow(window, "nvd-gen-w1-same", "hint-w1", false,
		nil, 4, windowEnd.UnixMicro(), windowEnd.UnixMicro()); err != nil || called {
		t.Fatalf("duplicate retry = (writer %v, err %v), want (false, nil): a skipped generation must not run the writer", called, err)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonUnchanged) - beforeSkips; got != 1 {
		t.Fatalf("unchanged skips delta = %d, want 1", got)
	}
	if p.generationCount(window) != 1 {
		t.Fatalf("generations = %d, want 1: a skipped successor must not store", p.generationCount(window))
	}
	if cursor, _ := p.readCursor(); cursor != windowEnd.UnixMicro() {
		t.Fatalf("cursor after skip = %d, want held %d", cursor, windowEnd.UnixMicro())
	}
}
