// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type impactSnapshotProbeDB struct {
	begins, directQueries int
	tx                    *impactSnapshotProbeTx
}

func (p *impactSnapshotProbeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	p.directQueries++
	return nil, errors.New("read escaped snapshot")
}

func (*impactSnapshotProbeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("write escaped snapshot")
}

func (p *impactSnapshotProbeDB) BeginReadOnlyRepeatableRead(context.Context) (db.Transaction, error) {
	p.begins++
	return p.tx, nil
}

type impactSnapshotProbeTx struct{ queries, commits, rollbacks int }

func (p *impactSnapshotProbeTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	p.queries++
	return nil, nil
}

func (*impactSnapshotProbeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("read-only snapshot")
}

func (p *impactSnapshotProbeTx) Commit() error   { p.commits++; return nil }
func (p *impactSnapshotProbeTx) Rollback() error { p.rollbacks++; return nil }

func TestSupplyChainImpactExpansionSharesOnePhysicalSnapshot(t *testing.T) {
	ctx := t.Context()
	probe := &impactSnapshotProbeDB{tx: &impactSnapshotProbeTx{}}
	store := NewFactStore(probe)
	err := store.WithSupplyChainImpactSnapshot(ctx, func(loader factload.FactLoader) error {
		bound, ok := loader.(*FactStore)
		if !ok {
			t.Fatalf("snapshot loader type = %T, want *FactStore", loader)
		}
		if bound.identityCache != nil {
			t.Fatal("snapshot loader retained identity cache")
		}
		if _, err := bound.database.QueryContext(ctx, "first"); err != nil {
			return err
		}
		if err := withReadOnlyRepeatableRead(ctx, bound.database, func(queryer db.Queryer) error {
			_, err := queryer.QueryContext(ctx, "nested")
			return err
		}); err != nil {
			return err
		}
		if _, err := bound.database.ExecContext(ctx, "write"); err == nil {
			t.Fatal("snapshot-bound loader accepted a write")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithSupplyChainImpactSnapshot: %v", err)
	}
	if probe.begins != 1 || probe.directQueries != 0 || probe.tx.queries != 2 || probe.tx.commits != 1 {
		t.Fatalf("begins=%d direct=%d snapshot queries=%d commits=%d, want 1/0/2/1",
			probe.begins, probe.directQueries, probe.tx.queries, probe.tx.commits)
	}
}

func TestSupplyChainImpactSnapshotRejectsLegacyOSPackageBridge(t *testing.T) {
	ctx := t.Context()
	probe := &impactSnapshotProbeDB{tx: &impactSnapshotProbeTx{}}
	err := NewFactStore(probe).WithSupplyChainImpactSnapshot(ctx, func(loader factload.FactLoader) error {
		bound := loader.(*FactStore)
		_, _, _, err := bound.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, []string{"pkg"}, 10)
		if err == nil {
			t.Fatal("legacy OS package bridge accepted inside protected static expansion")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if probe.tx.queries != 0 {
		t.Fatalf("legacy bridge issued %d queries before refusal", probe.tx.queries)
	}
}
