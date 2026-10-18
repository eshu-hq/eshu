// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/orphan"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// scriptSweepReader scripts the two read shapes one sweep cycle issues:
// the S1 candidate read (one aged disconnected Repository node, then empty
// pages) and the S2 connectivity read (disconnected).
type scriptSweepReader struct {
	agedUnix int64
	s1Calls  int
}

func (r *scriptSweepReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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
	invalidated [][]string
}

func (f *scriptSweepFingerprints) InvalidateDeployableUnitReopenFingerprintsForRepos(_ context.Context, repos []string) (int64, error) {
	f.invalidated = append(f.invalidated, append([]string(nil), repos...))
	return int64(len(repos)), nil
}

// TestGraphOrphanSweeperInvalidatesSweptReposOnly proves the #8316
// sweep-cycle push coupling and nothing more: the Repository node the cycle
// deletes is push-invalidated immediately under the same sweep lease. Gap
// reconciliation lives in the bootstrap-index maintenance pass, so the
// sweep performs no gap invalidation even though the graph left admitted
// anchors edgeless.
func TestGraphOrphanSweeperInvalidatesSweptReposOnly(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	reader := &scriptSweepReader{agedUnix: now.Add(-2 * time.Hour).Unix()}
	exec := &scriptSweepExecutor{}
	store := sourcecypher.NewOrphanSweepStore(exec, reader)
	store.Now = func() time.Time { return now }
	fingerprints := &scriptSweepFingerprints{}
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
	if len(fingerprints.invalidated) != 1 {
		t.Fatalf("invalidate calls = %d, want exactly 1 (swept push only)", len(fingerprints.invalidated))
	}
	if !reflect.DeepEqual(fingerprints.invalidated[0], []string{"repo-gone"}) {
		t.Fatalf("push invalidated = %#v, want [repo-gone]", fingerprints.invalidated[0])
	}
	if result.InvalidatedFingerprints != 1 {
		t.Fatalf("InvalidatedFingerprints = %d, want 1 (push only)", result.InvalidatedFingerprints)
	}
	if result.DeployableUnitGapRepos != 0 {
		t.Fatalf("DeployableUnitGapRepos = %d, want 0: the sweep performs no gap reconciliation", result.DeployableUnitGapRepos)
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

// TestDeployableUnitGapSeamExcludesAdmittedAnchorListing is the #8371
// regression test: the sweep-cycle Postgres seam must expose no
// admitted-anchor listing, so production code cannot issue a sweep-cycle
// statement against admission_decisions. The fault-injection kill cell holds
// an exclusive lock on that table and its census counts every ungranted
// waiter with exact equality, so a second waiter never aligns. Gap
// reconciliation lives in the bootstrap-index maintenance pass, which
// completes before the kill cell takes its lock.
func TestDeployableUnitGapSeamExcludesAdmittedAnchorListing(t *testing.T) {
	t.Parallel()

	seam := reflect.TypeOf((*deployableUnitGapPostgres)(nil)).Elem()
	if seam.NumMethod() != 1 {
		t.Fatalf("deployableUnitGapPostgres methods = %d, want exactly 1 (no admitted-anchor listing)", seam.NumMethod())
	}
	if name := seam.Method(0).Name; name != "InvalidateDeployableUnitReopenFingerprintsForRepos" {
		t.Fatalf("deployableUnitGapPostgres method = %q, want only the fingerprint invalidation", name)
	}
}
