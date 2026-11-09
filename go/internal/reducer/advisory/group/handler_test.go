// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type fakeWinnersReader struct {
	rowsByGroup map[string][]WinnerRow
	watermark   time.Time
	readErr     error
	markErr     error
	readGroups  []string
}

func (f *fakeWinnersReader) QualifyingAdvisoryGroupWinners(_ context.Context, groupID string) ([]WinnerRow, error) {
	f.readGroups = append(f.readGroups, groupID)
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.rowsByGroup[groupID], nil
}

func (f *fakeWinnersReader) AdvisoryGroupWinnersWatermark(_ context.Context) (time.Time, error) {
	return f.watermark, f.markErr
}

type fakeWriter struct {
	groups []Group
	stats  WriteStats
	err    error
}

func (f *fakeWriter) ReconcileAdvisoryGroup(_ context.Context, group Group) (WriteStats, error) {
	f.groups = append(f.groups, group)
	return f.stats, f.err
}

var handlerTestEnqueuedAt = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func advisoryGroupIntent(entityKeys ...string) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:        "intent-advisory-group",
		ScopeID:         "scope:vuln",
		GenerationID:    "gen:vuln",
		SourceSystem:    "vulnerability_intelligence",
		Domain:          reducercontract.DomainAdvisoryGroupProjection,
		Cause:           "supply_chain_impact_winners_touch",
		AttemptCount:    1,
		EntityKeys:      entityKeys,
		RelatedScopeIDs: []string{"scope:vuln"},
		Status:          reducercontract.IntentStatusClaimed,
		EnqueuedAt:      handlerTestEnqueuedAt,
		AvailableAt:     handlerTestEnqueuedAt,
		CycleStartedAt:  handlerTestEnqueuedAt,
	}
}

func TestAdvisoryGroupHandlerRejectsWrongDomainAndMissingPorts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fresh := &fakeWinnersReader{watermark: handlerTestEnqueuedAt}
	writer := &fakeWriter{}

	wrong := advisoryGroupIntent(EntityKey("CVE-2026-0001"))
	wrong.Domain = reducercontract.DomainSupplyChainImpact
	if _, err := (Handler{Winners: fresh, Writer: writer}).Handle(ctx, wrong); err == nil {
		t.Fatal("wrong domain: want error")
	}
	if _, err := (Handler{Writer: writer}).Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-0001"))); err == nil {
		t.Fatal("nil winners reader: want error")
	}
	if _, err := (Handler{Winners: fresh}).Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-0001"))); err == nil {
		t.Fatal("nil writer: want error")
	}
	if _, err := (Handler{Winners: fresh, Writer: writer}).Handle(ctx, advisoryGroupIntent()); err == nil {
		t.Fatal("no entity keys: want error")
	}
	if _, err := (Handler{Winners: fresh, Writer: writer}).Handle(ctx, advisoryGroupIntent("repository:abc")); err == nil {
		t.Fatal("entity key without the advisory_group prefix: want error")
	}
}

func TestAdvisoryGroupHandlerDefersUntilWinnersWatermarkPassesEnqueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	stale := &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(-time.Second)}
	writer := &fakeWriter{}
	handler := Handler{Winners: stale, Writer: writer, Now: func() time.Time { return handlerTestEnqueuedAt.Add(time.Minute) }}
	_, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-0001")))
	if err == nil {
		t.Fatal("stale watermark: want a deferral error")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("stale watermark error must be retryable: %v", err)
	}
	if !reducercontract.IsExpectedWait(err) {
		t.Fatalf("stale watermark error must be an expected wait: %v", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("failure class = %v, want %q (the non-counting class)", err, WinnersWatermarkNotReadyFailureClass)
	}
	if len(writer.groups) != 0 {
		t.Fatalf("writer reconciled %d groups behind a stale watermark", len(writer.groups))
	}

	// A reopened row's cycle start is the bound, not the original enqueue:
	// a watermark past the first enqueue but before the reopen still waits.
	reopened := advisoryGroupIntent(EntityKey("CVE-2026-0001"))
	reopened.CycleStartedAt = handlerTestEnqueuedAt.Add(10 * time.Minute)
	between := &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(5 * time.Minute)}
	handler = Handler{Winners: between, Writer: writer, Now: func() time.Time { return handlerTestEnqueuedAt.Add(11 * time.Minute) }}
	if _, err := handler.Handle(ctx, reopened); !reducercontract.IsExpectedWait(err) {
		t.Fatalf("watermark before cycle start: want expected wait, got %v", err)
	}
}

func TestAdvisoryGroupHandlerWatermarkWaitEventuallyCountsAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Past the bounded wait the deferral stops being an expected wait and
	// counts against the retry budget, so a dead winners maintainer
	// dead-letters visibly instead of the intent waiting in silence.
	stale := &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(-time.Hour)}
	handler := Handler{
		Winners: stale,
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return handlerTestEnqueuedAt.Add(winnersWatermarkMaxWait + time.Minute) },
	}
	_, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-0001")))
	if err == nil {
		t.Fatal("stale watermark past the bound: want error")
	}
	if reducercontract.IsExpectedWait(err) {
		t.Fatalf("past the bound the wait must stop being an expected wait: %v", err)
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("past the bound the error stays retryable so the budget, not the wait, ends it: %v", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() == WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("past the bound the failure class must leave the non-counting class: %v", err)
	}
}

func TestAdvisoryGroupHandlerProjectsGroupFromWinners(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	reader := &fakeWinnersReader{
		watermark: handlerTestEnqueuedAt,
		rowsByGroup: map[string][]WinnerRow{
			"CVE-2026-0001": {
				{
					RepositoryID: "repo:a", PackageID: "pkg:npm/lib@1", Ecosystem: "npm",
					PURL: "pkg:npm/lib@1.0.0", CVEID: "CVE-2026-0001", AdvisoryID: "GHSA-aaaa",
					Members: []string{"GHSA-aaaa", "CVE-2026-0001", "DEBIAN-CVE-2026-0001", "DSA-6000-1"},
				},
				{
					RepositoryID: "repo:a", PackageID: "pkg:npm/lib@1", Ecosystem: "npm",
					PURL: "pkg:npm/lib@1.0.1", CVEID: "CVE-2026-0001", AdvisoryID: "GHSA-aaaa",
					Members: []string{"cve-2026-0002"},
				},
				{
					RepositoryID: "repo:b", PackageID: "pkg:deb/lib", Ecosystem: "deb",
					PURL: "pkg:deb/debian/lib@2", CVEID: "", AdvisoryID: "DEBIAN-CVE-2026-0001",
					Members: []string{" CVE-2026-0003 "},
				},
				{
					// Defensive: a row with no repository contributes members
					// but no edge.
					RepositoryID: "", PackageID: "pkg:npm/orphan", AdvisoryID: "GHSA-orphan",
				},
			},
		},
	}
	writer := &fakeWriter{stats: WriteStats{NodesCreated: 1, EdgesCreated: 2, PropertiesSet: 5}}
	handler := Handler{Winners: reader, Writer: writer, Now: func() time.Time { return handlerTestEnqueuedAt.Add(time.Second) }}

	result, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-0001")))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.Domain != reducercontract.DomainAdvisoryGroupProjection {
		t.Fatalf("domain = %q", result.Domain)
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want EdgesCreated+NodesCreated = 3", result.CanonicalWrites)
	}
	if len(writer.groups) != 1 {
		t.Fatalf("reconciled %d groups, want 1", len(writer.groups))
	}
	group := writer.groups[0]
	if group.ID != "CVE-2026-0001" {
		t.Fatalf("group id = %q", group.ID)
	}
	// DEBIAN-CVE-* and DSA-* are advisory class, never CVE class; a
	// lower-case cve- member canonicalizes like upstreamCVEKey.
	wantCVEs := []string{"CVE-2026-0001", "CVE-2026-0002", "CVE-2026-0003"}
	if !reflect.DeepEqual(group.CVEIDs, wantCVEs) {
		t.Fatalf("CVEIDs = %v, want %v", group.CVEIDs, wantCVEs)
	}
	wantAdvisories := []string{"DEBIAN-CVE-2026-0001", "DSA-6000-1", "GHSA-aaaa", "GHSA-orphan"}
	if !reflect.DeepEqual(group.AdvisoryIDs, wantAdvisories) {
		t.Fatalf("AdvisoryIDs = %v, want %v", group.AdvisoryIDs, wantAdvisories)
	}
	// The (repo:a, pkg:npm/lib@1) disagreement keeps the lowest PURL.
	wantEdges := []Edge{
		{RepositoryID: "repo:a", PackageID: "pkg:npm/lib@1", Ecosystem: "npm", PURL: "pkg:npm/lib@1.0.0"},
		{RepositoryID: "repo:b", PackageID: "pkg:deb/lib", Ecosystem: "deb", PURL: "pkg:deb/debian/lib@2"},
	}
	if !reflect.DeepEqual(group.Edges, wantEdges) {
		t.Fatalf("Edges = %+v, want %+v", group.Edges, wantEdges)
	}
}

func TestAdvisoryGroupHandlerSingletonGroupCarriesBothIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	reader := &fakeWinnersReader{
		watermark: handlerTestEnqueuedAt,
		rowsByGroup: map[string][]WinnerRow{
			"GHSA-solo": {{
				RepositoryID: "repo:a", PackageID: "pkg:gem/solo", Ecosystem: "rubygems",
				PURL: "pkg:gem/solo@1", CVEID: "CVE-2026-7777", AdvisoryID: "GHSA-solo",
			}},
		},
	}
	writer := &fakeWriter{stats: WriteStats{EdgesCreated: 1}}
	handler := Handler{Winners: reader, Writer: writer}
	if _, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("GHSA-solo"))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	group := writer.groups[0]
	if !reflect.DeepEqual(group.CVEIDs, []string{"CVE-2026-7777"}) {
		t.Fatalf("CVEIDs = %v, want the row's own CVE even with no Members", group.CVEIDs)
	}
	if !reflect.DeepEqual(group.AdvisoryIDs, []string{"GHSA-solo"}) {
		t.Fatalf("AdvisoryIDs = %v, want the row's own advisory id", group.AdvisoryIDs)
	}
}

func TestAdvisoryGroupHandlerEmptyWinnersReconcilesDeletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	reader := &fakeWinnersReader{watermark: handlerTestEnqueuedAt, rowsByGroup: map[string][]WinnerRow{}}
	writer := &fakeWriter{stats: WriteStats{NodesDeleted: 1, EdgesDeleted: 3}}
	handler := Handler{Winners: reader, Writer: writer}
	result, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-gone")))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(writer.groups) != 1 {
		t.Fatalf("reconciled %d groups, want 1 (the delete)", len(writer.groups))
	}
	group := writer.groups[0]
	if group.ID != "CVE-2026-gone" || len(group.Edges) != 0 || len(group.CVEIDs) != 0 || len(group.AdvisoryIDs) != 0 {
		t.Fatalf("delete group = %+v, want id only with no members or edges", group)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for a pure deletion", result.CanonicalWrites)
	}
}

func TestAdvisoryGroupHandlerOversizedMemberUnionProjectsInFull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	members := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		members = append(members, fmt.Sprintf("GHSA-%04d", i))
	}
	reader := &fakeWinnersReader{
		watermark: handlerTestEnqueuedAt,
		rowsByGroup: map[string][]WinnerRow{
			"CVE-2026-wide": {{
				RepositoryID: "repo:a", PackageID: "pkg:npm/wide", Ecosystem: "npm",
				PURL: "pkg:npm/wide@1", CVEID: "CVE-2026-wide", Members: members,
			}},
		},
	}
	writer := &fakeWriter{}
	handler := Handler{Winners: reader, Writer: writer}
	if _, err := handler.Handle(ctx, advisoryGroupIntent(EntityKey("CVE-2026-wide"))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	group := writer.groups[0]
	if len(group.AdvisoryIDs) != 40 {
		t.Fatalf("AdvisoryIDs = %d, want all 40: an oversized union is counted, never truncated", len(group.AdvisoryIDs))
	}
	if got := group.AdvisoryIDs[0]; got != "GHSA-0000" {
		t.Fatalf("AdvisoryIDs not byte-sorted: first = %q", got)
	}
}

func TestAdvisoryGroupHandlerReadAndWriteErrorsSurface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("boom")

	reader := &fakeWinnersReader{watermark: handlerTestEnqueuedAt, markErr: boom}
	if _, err := (Handler{Winners: reader, Writer: &fakeWriter{}}).Handle(ctx, advisoryGroupIntent(EntityKey("CVE-1"))); !errors.Is(err, boom) {
		t.Fatalf("watermark error = %v, want wrapped boom", err)
	}
	reader = &fakeWinnersReader{watermark: handlerTestEnqueuedAt, readErr: boom}
	if _, err := (Handler{Winners: reader, Writer: &fakeWriter{}}).Handle(ctx, advisoryGroupIntent(EntityKey("CVE-1"))); !errors.Is(err, boom) {
		t.Fatalf("winner read error = %v, want wrapped boom", err)
	}
	reader = &fakeWinnersReader{watermark: handlerTestEnqueuedAt, rowsByGroup: map[string][]WinnerRow{}}
	if _, err := (Handler{Winners: reader, Writer: &fakeWriter{err: boom}}).Handle(ctx, advisoryGroupIntent(EntityKey("CVE-1"))); !errors.Is(err, boom) {
		t.Fatalf("reconcile error = %v, want wrapped boom", err)
	}
}

func TestAdvisoryGroupDefinitionIsValid(t *testing.T) {
	t.Parallel()
	definition := Definition()
	if definition.Domain != reducercontract.DomainAdvisoryGroupProjection {
		t.Fatalf("domain = %q", definition.Domain)
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Definition().Validate(): %v", err)
	}
	if definition.TruthContract.CanonicalKind != "advisory_group" {
		t.Fatalf("canonical kind = %q", definition.TruthContract.CanonicalKind)
	}
}
