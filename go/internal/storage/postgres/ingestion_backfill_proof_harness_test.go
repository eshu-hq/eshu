// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

type targetedDiffPair = testfixtures.TargetedDiffPair

var (
	targetedDiffBase   = testfixtures.TargetedDiffBase
	targetedDiffArmsAt = testfixtures.TargetedDiffArmsAt
	sortedPartitions   = testfixtures.SortedPartitions
)

func targetedDiffReopenDomains() []string {
	return append([]string{"deployment_mapping", "code_import_repo_edge"}, CrossScopeCorrelationReopenDomains()...)
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

func targetedDiffStore(database *sql.DB, now time.Time) IngestionStore {
	store := NewIngestionStoreWithMaintenanceConfig(SQLDB{DB: database}, 0, 1)
	store.Now = func() time.Time { return now }
	return store
}

func workIDs(generationID string, domains ...string) []string {
	if len(domains) == 0 {
		domains = targetedDiffReopenDomains()
	}
	return testfixtures.WorkIDs(generationID, domains)
}

func targetedMaintenanceProofDSN(t *testing.T) string {
	return testfixtures.TargetedMaintenanceProofDSN(t)
}
