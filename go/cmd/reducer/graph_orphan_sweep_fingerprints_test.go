// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/orphan"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// scriptSweepReader scripts the three read shapes one sweep cycle issues:
// the S1 candidate read (one aged disconnected Repository node, then empty
// pages), the S2 connectivity read (disconnected), and the #8316 gap probe
// (no edges, so every admitted anchor is a gap).
type scriptSweepReader struct {
	agedUnix int64
	s1Calls  int
}

func (r *scriptSweepReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "CORRELATES_DEPLOYABLE_UNIT") {
		return nil, nil
	}
	if strings.Contains(cypher, "observed_at") {
		r.s1Calls++
		if r.s1Calls > 1 || !strings.Contains(cypher, "n:Repository") {
			return nil, nil
		}
		return []map[string]any{{"key": "repo-gone", "observed_at": r.agedUnix}}, nil
	}
	return nil, nil
}

type scriptSweepExecutor struct {
	executed []sourcecypher.Statement
}

func (e *scriptSweepExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.executed = append(e.executed, stmt)
	return nil
}

type scriptSweepFingerprints struct {
	anchors     []string
	invalidated [][]string
}

func (f *scriptSweepFingerprints) ListDeployableUnitAdmittedAnchorRepos(_ context.Context, _ int) ([]string, error) {
	return f.anchors, nil
}

func (f *scriptSweepFingerprints) InvalidateDeployableUnitReopenFingerprintsForRepos(_ context.Context, repos []string) (int64, error) {
	f.invalidated = append(f.invalidated, append([]string(nil), repos...))
	return int64(len(repos)), nil
}

// TestGraphOrphanSweeperInvalidatesSweptReposAndGapRepos proves the #8316
// sweep-cycle coupling: the Repository node the cycle deletes is
// push-invalidated immediately, then the gap reconciliation invalidates the
// admitted anchors the graph left edgeless. Both run under the same sweep
// lease, with no new worker.
func TestGraphOrphanSweeperInvalidatesSweptReposAndGapRepos(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	reader := &scriptSweepReader{agedUnix: now.Add(-2 * time.Hour).Unix()}
	exec := &scriptSweepExecutor{}
	store := sourcecypher.NewOrphanSweepStore(exec, reader)
	store.Now = func() time.Time { return now }
	fingerprints := &scriptSweepFingerprints{anchors: []string{"repo-gone", "repo-healthy"}}
	sweeper := graphOrphanSweeper{store: store, fingerprints: fingerprints}

	result, err := sweeper.SweepOrphanNodes(context.Background(), orphan.Policy{
		Labels:     []string{"Repository"},
		OrphanTTL:  time.Hour,
		BatchLimit: 10,
		CountLimit: 100,
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if got := result.Deleted["Repository"]; got != 1 {
		t.Fatalf("Deleted[Repository] = %d, want 1", got)
	}
	if len(fingerprints.invalidated) != 2 {
		t.Fatalf("invalidate calls = %d, want 2 (swept push, then gap set)", len(fingerprints.invalidated))
	}
	if !reflect.DeepEqual(fingerprints.invalidated[0], []string{"repo-gone"}) {
		t.Fatalf("push invalidated = %#v, want [repo-gone]", fingerprints.invalidated[0])
	}
	gaps := append([]string(nil), fingerprints.invalidated[1]...)
	sort.Strings(gaps)
	if !reflect.DeepEqual(gaps, []string{"repo-gone", "repo-healthy"}) {
		t.Fatalf("gap invalidated = %#v, want both admitted anchors", gaps)
	}
	if result.InvalidatedFingerprints != 3 {
		t.Fatalf("InvalidatedFingerprints = %d, want 3 (1 push + 2 gap)", result.InvalidatedFingerprints)
	}
	if result.DeployableUnitGapRepos != 2 {
		t.Fatalf("DeployableUnitGapRepos = %d, want 2", result.DeployableUnitGapRepos)
	}
}

// TestGraphOrphanSweeperWithoutFingerprintsSkipsGapWork proves the nil seam
// leaves the sweep exactly as before: no invalidation, no gap probe, no
// fingerprint counts.
func TestGraphOrphanSweeperWithoutFingerprintsSkipsGapWork(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	reader := &scriptSweepReader{agedUnix: now.Add(-2 * time.Hour).Unix()}
	exec := &scriptSweepExecutor{}
	store := sourcecypher.NewOrphanSweepStore(exec, reader)
	store.Now = func() time.Time { return now }
	sweeper := graphOrphanSweeper{store: store}

	result, err := sweeper.SweepOrphanNodes(context.Background(), orphan.Policy{
		Labels:     []string{"Repository"},
		OrphanTTL:  time.Hour,
		BatchLimit: 10,
		CountLimit: 100,
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v", err)
	}
	if got := result.Deleted["Repository"]; got != 1 {
		t.Fatalf("Deleted[Repository] = %d, want 1", got)
	}
	if result.InvalidatedFingerprints != 0 || result.DeployableUnitGapRepos != 0 {
		t.Fatalf("fingerprint counts = (%d, %d), want (0, 0) without the seam",
			result.InvalidatedFingerprints, result.DeployableUnitGapRepos)
	}
}
