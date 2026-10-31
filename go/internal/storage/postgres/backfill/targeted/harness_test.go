// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type (
	targetedDiffPair   = testfixtures.TargetedDiffPair
	targetedDiffRow    = testfixtures.TargetedDiffRow
	targetedDiffState  = testfixtures.TargetedDiffState
	targetedDiffReport = testfixtures.TargetedDiffReport
	hookBeginner       = testfixtures.HookBeginner
)

var (
	targetedDiffBase                  = testfixtures.TargetedDiffBase
	targetedDiffArmsAt                = testfixtures.TargetedDiffArmsAt
	diffStates                        = testfixtures.DiffStates
	changedRows                       = testfixtures.ChangedRows
	assertPhaseCommittedAfterEvidence = testfixtures.AssertPhaseCommittedAfterEvidence
	sortedPartitions                  = testfixtures.SortedPartitions
	partitionSet                      = testfixtures.PartitionSet
	partitionsOf                      = testfixtures.PartitionsOf
)

func targetedDiffReopenDomains() []string {
	return append([]string{"deployment_mapping", "code_import_repo_edge"},
		postgres.CrossScopeCorrelationReopenDomains()...)
}

func newTargetedDiffPair(t *testing.T) *targetedDiffPair {
	t.Helper()
	open := func(t *testing.T, dsn, prefix string) *sql.DB {
		return openIsolatedBootstrapSchema(t, dsn, os.Getenv("ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE"), prefix)
	}
	return testfixtures.NewTargetedDiffPair(t, testfixtures.TargetedMaintenanceProofDSN(t),
		open, targetedDiffReopenDomains(),
		func(ctx context.Context, database *sql.DB, at time.Time) error {
			return targetedDiffStore(database, at).BackfillAllRelationshipEvidence(ctx, nil, nil)
		})
}

func targetedMaintenanceProofDSN(t *testing.T) string {
	return testfixtures.TargetedMaintenanceProofDSN(t)
}

func targetedDiffStore(database *sql.DB, now time.Time) postgres.IngestionStore {
	return targetedDiffStoreWithConfig(postgres.SQLDB{DB: database}, now, 0)
}

func targetedDiffStoreWithConfig(database db.ExecQueryer, now time.Time, batchSize int) postgres.IngestionStore {
	store := postgres.NewIngestionStoreWithMaintenanceConfig(database, batchSize, 1)
	store.Now = func() time.Time { return now }
	return store
}

func openIsolatedBootstrapSchema(t *testing.T, dsn, optIn, prefix string) *sql.DB {
	t.Helper()
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, func(ctx context.Context, database *sql.DB) error {
		if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
			return fmt.Errorf("apply bootstrap schema: %w", err)
		}
		return nil
	})
	return database
}

type hookedDB struct {
	postgres.SQLDB
	hook *hookBeginner
}

func (h hookedDB) Begin(ctx context.Context) (db.Transaction, error) {
	return h.hook.Begin(ctx)
}

const targetedGCPScope = testfixtures.TargetedGCPScope

var (
	seedTargetedCorpus         = testfixtures.SeedTargetedCorpus
	seedTargetedArgoCD         = testfixtures.SeedTargetedArgoCD
	seedTargetedMonoGeneration = testfixtures.SeedTargetedMonoGeneration
)
