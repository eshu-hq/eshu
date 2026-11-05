// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// failingOSVDeltaFakes fails any replan call: the flag-off test arms the
// service with these, so any touch fails the tick loudly.
type failingOSVDeltaChangedReader struct{}

func (failingOSVDeltaChangedReader) ListVulnerabilityChangedNames(context.Context) ([]vulnerability.OSVDeltaChangedRecord, error) {
	return nil, errOSVDeltaTouchedWhenOff
}

type errOSVDeltaTouched struct{ name string }

func (e errOSVDeltaTouched) Error() string {
	return "osv delta " + e.name + " touched with the flag off"
}

var errOSVDeltaTouchedWhenOff = errOSVDeltaTouched{"reader"}

type failingOSVDeltaStoredReader struct{}

func (failingOSVDeltaStoredReader) ListStoredAdvisoryPackageNames(context.Context, []string) (map[string][]vulnerability.OSVDeltaStoredPackage, error) {
	return nil, errOSVDeltaTouched{"stored reader"}
}

type failingOSVDeltaResolver struct{}

func (failingOSVDeltaResolver) ResolveOwnedPackageNames(context.Context, []vulnerability.OSVDeltaLookup) ([]vulnerability.OSVDeltaResolvedTarget, error) {
	return nil, errOSVDeltaTouched{"resolver"}
}

type failingOSVDeltaReplanStore struct{}

func (failingOSVDeltaReplanStore) AdmitChangedTargets(context.Context, string, []vulnerability.OSVDeltaValidatedChange) (int, int, error) {
	return 0, 0, errOSVDeltaTouched{"admit"}
}

func (failingOSVDeltaReplanStore) ReadPendingReplan(context.Context, string) ([]vulnerability.OSVDeltaQueuedRow, error) {
	return nil, errOSVDeltaTouched{"pending read"}
}

func (failingOSVDeltaReplanStore) CommitReplanDrain(context.Context, string, []int64, time.Time, time.Time) (int64, error) {
	return 0, errOSVDeltaTouched{"commit"}
}

func (failingOSVDeltaReplanStore) ReadCommitWatermark(context.Context, string) (vulnerability.OSVDeltaWatermark, error) {
	return vulnerability.OSVDeltaWatermark{}, errOSVDeltaTouched{"watermark read"}
}

func (failingOSVDeltaReplanStore) RecordPollSucceeded(context.Context, string, time.Time) error {
	return errOSVDeltaTouched{"poll record"}
}

// TestServiceOSVDeltaReplanFlagOffTouchesNoReplanStore pins the flag-off
// default: without osv_delta_replan the tick never touches a replan reader
// or store, so the rotation path is byte-identical with the flag off.
func TestServiceOSVDeltaReplanFlagOffTouchesNoReplanStore(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(observedAt)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":100}}`
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
		Store:                            &fakeStore{},
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         &fakeOwnedPackageTargetReader{},
		OSVDeltaChangedNameReader:        failingOSVDeltaChangedReader{},
		OSVDeltaStoredNameReader:         failingOSVDeltaStoredReader{},
		OSVDeltaOwnedNameResolver:        failingOSVDeltaResolver{},
		OSVDeltaReplanStore:              failingOSVDeltaReplanStore{},
		Metrics:                          &fakeMetrics{},
	}
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
}

// memoryOSVDeltaReplanStore is an in-memory OSVDeltaReplanStore with the
// real table's keep-newest admit semantics, so consecutive ticks observe
// each other like the real queue.
type memoryOSVDeltaReplanStore struct {
	rows      map[int64]vulnerability.OSVDeltaQueuedRow
	byTuple   map[string]int64
	nextID    int64
	watermark vulnerability.OSVDeltaWatermark
	commits   [][]int64
	polls     int
}

func (m *memoryOSVDeltaReplanStore) AdmitChangedTargets(_ context.Context, _ string, changes []vulnerability.OSVDeltaValidatedChange) (int, int, error) {
	if m.rows == nil {
		m.rows = make(map[int64]vulnerability.OSVDeltaQueuedRow)
		m.byTuple = make(map[string]int64)
	}
	admitted := 0
	for _, change := range changes {
		key := change.AdvisoryID + "\x00" + change.Ecosystem + "\x00" + change.PackageName
		if id, ok := m.byTuple[key]; ok {
			if !change.FeedTimestamp.After(m.rows[id].FeedTimestamp) {
				continue
			}
			row := m.rows[id]
			row.Withdrawn = change.Withdrawn
			row.FeedTimestamp = change.FeedTimestamp
			m.rows[id] = row
			admitted++
			continue
		}
		m.nextID++
		m.rows[m.nextID] = vulnerability.OSVDeltaQueuedRow{
			RowID: m.nextID, AdvisoryID: change.AdvisoryID, Ecosystem: change.Ecosystem,
			PackageName: change.PackageName, Withdrawn: change.Withdrawn, FeedTimestamp: change.FeedTimestamp,
		}
		m.byTuple[key] = m.nextID
		admitted++
	}
	return admitted, len(changes) - admitted, nil
}

func (m *memoryOSVDeltaReplanStore) ReadPendingReplan(_ context.Context, _ string) ([]vulnerability.OSVDeltaQueuedRow, error) {
	var out []vulnerability.OSVDeltaQueuedRow
	for _, row := range m.rows {
		out = append(out, row)
	}
	return out, nil
}

func (m *memoryOSVDeltaReplanStore) CommitReplanDrain(_ context.Context, _ string, ids []int64, watermark time.Time, observedAt time.Time) (int64, error) {
	var marked int64
	for _, id := range ids {
		if _, ok := m.rows[id]; !ok {
			continue
		}
		delete(m.rows, id)
		m.commits = append(m.commits, []int64{id})
		marked++
	}
	if !m.watermark.CommittedSet || watermark.After(m.watermark.Committed) {
		m.watermark.Committed, m.watermark.CommittedSet = watermark, true
	}
	m.watermark.LastPoll, m.watermark.LastPollSet = observedAt, true
	return marked, nil
}

func (m *memoryOSVDeltaReplanStore) ReadCommitWatermark(_ context.Context, _ string) (vulnerability.OSVDeltaWatermark, error) {
	return m.watermark, nil
}

func (m *memoryOSVDeltaReplanStore) RecordPollSucceeded(_ context.Context, _ string, observedAt time.Time) error {
	m.polls++
	if !m.watermark.LastPollSet || observedAt.After(m.watermark.LastPoll) {
		m.watermark.LastPoll, m.watermark.LastPollSet = observedAt, true
	}
	return nil
}

type scriptOSVDeltaChangedReader struct {
	records []vulnerability.OSVDeltaChangedRecord
	calls   int
}

func (f *scriptOSVDeltaChangedReader) ListVulnerabilityChangedNames(context.Context) ([]vulnerability.OSVDeltaChangedRecord, error) {
	f.calls++
	return f.records, nil
}

type scriptOSVDeltaStoredReader struct {
	byAdvisory map[string][]vulnerability.OSVDeltaStoredPackage
	calls      int
}

func (f *scriptOSVDeltaStoredReader) ListStoredAdvisoryPackageNames(_ context.Context, ids []string) (map[string][]vulnerability.OSVDeltaStoredPackage, error) {
	f.calls++
	out := make(map[string][]vulnerability.OSVDeltaStoredPackage)
	for _, id := range ids {
		out[id] = append(out[id], f.byAdvisory[id]...)
	}
	return out, nil
}

type scriptOSVDeltaResolver struct {
	targets map[vulnerability.OSVDeltaLookup][]vulnerability.OSVDeltaResolvedTarget
	calls   int
	batches [][]vulnerability.OSVDeltaLookup
}

func (f *scriptOSVDeltaResolver) ResolveOwnedPackageNames(_ context.Context, lookups []vulnerability.OSVDeltaLookup) ([]vulnerability.OSVDeltaResolvedTarget, error) {
	f.calls++
	f.batches = append(f.batches, append([]vulnerability.OSVDeltaLookup(nil), lookups...))
	var out []vulnerability.OSVDeltaResolvedTarget
	for _, lookup := range lookups {
		out = append(out, f.targets[lookup]...)
	}
	return out, nil
}

// TestServiceOSVDeltaReplanDrainAdmitsExtraScopesAndCommits proves the whole
// flag-on loop: changed facts admit, the drain mints one _replan scope
// through the normal admission, and the commit marks exactly those rows and
// advances the watermark.
func TestServiceOSVDeltaReplanDrainAdmitsExtraScopesAndCommits(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	instance := testServiceVulnerabilityIntelligenceInstance(observedAt)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":100,"osv_delta_replan":true}}`
	changed := &scriptOSVDeltaChangedReader{records: []vulnerability.OSVDeltaChangedRecord{
		{AdvisoryID: "ADV-X", Ecosystem: "npm", PackageName: "left-pad", FeedTimestamp: "2026-11-01T12:00:00Z"},
	}}
	stored := &scriptOSVDeltaStoredReader{byAdvisory: map[string][]vulnerability.OSVDeltaStoredPackage{
		"ADV-X": {{Ecosystem: "npm", PackageName: "old-name", PackageID: "npm://registry.npmjs.org/old-name"}},
	}}
	resolver := &scriptOSVDeltaResolver{targets: map[vulnerability.OSVDeltaLookup][]vulnerability.OSVDeltaResolvedTarget{
		{Ecosystem: "npm", Name: "left-pad"}: {{Lookup: vulnerability.OSVDeltaLookup{Ecosystem: "npm", Name: "left-pad"}, Version: "1.2.0", KeyHashLow24: 7, VirtualSlot: 7}},
		{Ecosystem: "npm", Name: "old-name"}: {{Lookup: vulnerability.OSVDeltaLookup{Ecosystem: "npm", Name: "old-name"}, Version: "0.9.0", KeyHashLow24: 7, VirtualSlot: 7}},
	}}
	replanStore := &memoryOSVDeltaReplanStore{}
	enqueue := &fakeStore{}
	metrics := &fakeMetrics{}
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
		Store:                            enqueue,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         &fakeOwnedPackageTargetReader{},
		OSVDeltaChangedNameReader:        changed,
		OSVDeltaStoredNameReader:         stored,
		OSVDeltaOwnedNameResolver:        resolver,
		OSVDeltaReplanStore:              replanStore,
		Metrics:                          metrics,
	}
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	var replanScopes []string
	for _, item := range enqueue.enqueuedItems {
		if strings.Contains(item.ScopeID, "/_replan/") {
			replanScopes = append(replanScopes, item.ScopeID)
		}
	}
	if len(replanScopes) != 1 || !strings.HasPrefix(replanScopes[0], "vuln-intel://osv/npm/_replan/7/") {
		t.Fatalf("replan scopes = %v, want the one npm slot-7 _replan scope", replanScopes)
	}
	if len(replanStore.rows) != 0 {
		t.Fatalf("%d rows still pending, want the queue drained", len(replanStore.rows))
	}
	if !replanStore.watermark.CommittedSet || !replanStore.watermark.Committed.Equal(feed) {
		t.Fatalf("watermark = %+v, want committed at the drained feed time", replanStore.watermark)
	}
	if changed.calls != 1 || stored.calls != 1 || resolver.calls != 1 {
		t.Fatalf("reads = (%d, %d, %d), want one changed, one stored, one resolve call", changed.calls, stored.calls, resolver.calls)
	}
	// One resolve call carries the whole hit list (arbiter condition B):
	// the changed name plus the stored union, never one call per name.
	if len(resolver.batches) != 1 || len(resolver.batches[0]) != 2 {
		t.Fatalf("resolve batches = %v, want one batch with the union", resolver.batches)
	}
	if len(metrics.osvDeltaObserved) != 1 {
		t.Fatalf("replan observations = %d, want 1", len(metrics.osvDeltaObserved))
	}
	got := metrics.osvDeltaObserved[0]
	if got.Admitted != 1 || got.Duplicates != 0 || got.ParseFailed != 0 || got.Drained != 1 || got.Overflow != 0 {
		t.Fatalf("replan metrics = %+v, want admitted=1 drained=1, no dups/failures/overflow", got)
	}
}

// TestServiceOSVDeltaReplanCountsStuckDrainRejects pins the stuck-queue
// stewardship: a queued row the drain intersection drops (an unknown
// ecosystem here) is counted in ParseFailed, keeps the pending depth
// honest, sets the oldest-pending age, and warns — instead of stranding the
// row invisibly at the same depth forever.
func TestServiceOSVDeltaReplanCountsStuckDrainRejects(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(3 * time.Hour) // past the 2h replan-stale bound
	instance := testServiceVulnerabilityIntelligenceInstance(observedAt)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":100,"osv_delta_replan":true}}`
	changed := &scriptOSVDeltaChangedReader{}
	stored := &scriptOSVDeltaStoredReader{}
	resolver := &scriptOSVDeltaResolver{}
	replanStore := &memoryOSVDeltaReplanStore{rows: map[int64]vulnerability.OSVDeltaQueuedRow{
		1: {RowID: 1, AdvisoryID: "ADV-U", Ecosystem: "tampered", PackageName: "x", FeedTimestamp: feed},
	}}
	enqueue := &fakeStore{}
	metrics := &fakeMetrics{}
	var logs bytes.Buffer
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
		Store:                            enqueue,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         &fakeOwnedPackageTargetReader{},
		OSVDeltaChangedNameReader:        changed,
		OSVDeltaStoredNameReader:         stored,
		OSVDeltaOwnedNameResolver:        resolver,
		OSVDeltaReplanStore:              replanStore,
		Metrics:                          metrics,
		Logger:                           slog.New(slog.NewTextHandler(&logs, nil)),
	}
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if len(metrics.osvDeltaObserved) != 1 {
		t.Fatalf("replan observations = %d, want 1", len(metrics.osvDeltaObserved))
	}
	got := metrics.osvDeltaObserved[0]
	if got.ParseFailed != 1 {
		t.Fatalf("ParseFailed = %d, want the dropped queued row counted", got.ParseFailed)
	}
	if got.Overflow != 1 || got.PendingDepth != 1 {
		t.Fatalf("replan metrics = %+v, want depth 1 for the stranded row", got)
	}
	if !got.OldestPendingKnown || got.OldestPendingAge != 3*time.Hour {
		t.Fatalf("replan metrics = %+v, want oldest-pending age 3h", got)
	}
	if out := logs.String(); !strings.Contains(out, "rejected_rows=1") {
		t.Fatalf("logs = %q, want the stuck-row stewardship warning", out)
	}
	if _, ok := replanStore.rows[1]; !ok {
		t.Fatal("row 1 left the queue: a rejected row stays pending, counted, not silently retired")
	}
}

// TestServiceOSVDeltaReplanDropsMalformedFreshFact pins the error-scope
// contract (#10092 P2): one malformed fresh fact (bad withdrawn spelling
// here) counts parse_failed and warns scoped to the instance, while the
// pass still schedules: a single bad fact never aborts the tick, and its
// valid neighbors still admit and drain.
func TestServiceOSVDeltaReplanDropsMalformedFreshFact(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	instance := testServiceVulnerabilityIntelligenceInstance(observedAt)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":100,"osv_delta_replan":true}}`
	changed := &scriptOSVDeltaChangedReader{records: []vulnerability.OSVDeltaChangedRecord{
		{AdvisoryID: "ADV-M", Ecosystem: "npm", PackageName: "odd", Withdrawn: "yes", FeedTimestamp: "2026-11-01T12:00:00Z"},
		{AdvisoryID: "ADV-K", Ecosystem: "npm", PackageName: "kept", Withdrawn: "false", FeedTimestamp: "2026-11-01T12:00:00Z"},
	}}
	stored := &scriptOSVDeltaStoredReader{}
	resolver := &scriptOSVDeltaResolver{}
	replanStore := &memoryOSVDeltaReplanStore{}
	enqueue := &fakeStore{}
	metrics := &fakeMetrics{}
	var logs bytes.Buffer
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
		Store:                            enqueue,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         &fakeOwnedPackageTargetReader{},
		OSVDeltaChangedNameReader:        changed,
		OSVDeltaStoredNameReader:         stored,
		OSVDeltaOwnedNameResolver:        resolver,
		OSVDeltaReplanStore:              replanStore,
		Metrics:                          metrics,
		Logger:                           slog.New(slog.NewTextHandler(&logs, nil)),
	}
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil (one malformed fact must not abort the tick)", err)
	}
	if len(metrics.osvDeltaObserved) != 1 {
		t.Fatalf("replan observations = %d, want 1", len(metrics.osvDeltaObserved))
	}
	got := metrics.osvDeltaObserved[0]
	if got.ParseFailed != 1 {
		t.Fatalf("ParseFailed = %d, want the malformed fact counted", got.ParseFailed)
	}
	if got.Admitted != 1 {
		t.Fatalf("Admitted = %d, want the valid neighbor admitted", got.Admitted)
	}
	if len(replanStore.rows) != 0 {
		t.Fatalf("%d rows still pending, want the valid neighbor drained", len(replanStore.rows))
	}
	if out := logs.String(); !strings.Contains(out, "malformed_changed_facts=1") {
		t.Fatalf("logs = %q, want the instance-scoped malformed-fact warning", out)
	}
}

// TestCommitOSVDeltaReplanHoldsUnmappedNontrivialRows pins the commit gate:
// a covered row with no minted scope commits only when the planner marked
// it trivial (nothing owned, nothing to authorize). A non-trivial row the
// plan did not mint a scope for stays pending instead of retiring on an
// unchecked held scope.
func TestCommitOSVDeltaReplanHoldsUnmappedNontrivialRows(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	replanStore := &memoryOSVDeltaReplanStore{rows: map[int64]vulnerability.OSVDeltaQueuedRow{
		1: {RowID: 1, AdvisoryID: "GHSA-h", Ecosystem: "npm", PackageName: "held", FeedTimestamp: feed},
		2: {RowID: 2, AdvisoryID: "GHSA-t", Ecosystem: "npm", PackageName: "gone", FeedTimestamp: feed},
	}}
	service := Service{Metrics: &fakeMetrics{}}
	pass := &osvDeltaPass{
		store:      replanStore,
		instanceID: "vuln-intel-replan",
		pending: []vulnerability.OSVDeltaValidatedChange{
			{RowID: 1, AdvisoryID: "GHSA-h", Ecosystem: "npm", PackageName: "held", FeedTimestamp: feed},
			{RowID: 2, AdvisoryID: "GHSA-t", Ecosystem: "npm", PackageName: "gone", FeedTimestamp: feed},
		},
	}
	observation := vulnerability.OSVDeltaReplanObservation{
		Enabled:       true,
		Drained:       2,
		CoveredRowIDs: []int64{1, 2},
		TrivialRowIDs: []int64{2},
		Watermark:     feed,
		WatermarkSet:  true,
	}
	// Nil items with nil authorized and inserted sets is the site-A shape:
	// no _replan scope was authorized or inserted this pass, so only the
	// trivial row drains.
	if err := service.commitOSVDeltaReplan(context.Background(), pass, workflow.Run{}, nil, observation, observedAt, nil, nil); err != nil {
		t.Fatalf("commitOSVDeltaReplan() error = %v, want nil", err)
	}
	if _, ok := replanStore.rows[1]; !ok {
		t.Fatal("row 1 committed without a minted scope: a non-trivial unmapped row must stay pending")
	}
	if _, ok := replanStore.rows[2]; ok {
		t.Fatal("row 2 still pending: a trivial row commits with nothing to authorize")
	}
}
