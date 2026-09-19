// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/iamcantargets"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/readinesswait"
)

// iamCanPerformCrossScopeTargetsFor wires the #6785 CAN_PERFORM cross-scope
// target store: candidate-scope readiness from Postgres, and exact-ARN fact
// reads pinned to each sibling scope's active generation through the shared
// fact store.
func iamCanPerformCrossScopeTargetsFor(database db.Queryer, factStore *postgres.FactStore) iamcan.CrossScopeTargetLoader {
	return iamcantargets.Store{DB: database, Facts: factStore}
}

// workloadInstanceExistenceFor wires the #6785 USES readiness gate's
// graph-backed WorkloadInstance existence lookup.
func workloadInstanceExistenceFor(graphReader query.GraphQuery) workloadinstance.ExistenceLookup {
	return workloadinstance.GraphExistenceLookup{Graph: graphReader}
}

// readinessWaitsFor wires the #6785 (scope, domain) readiness-wait ledger the
// CAN_PERFORM and USES handlers share.
func readinessWaitsFor(database db.ExecQueryer) crossscope.ReadinessWaitLedger {
	return readinesswait.Store{DB: database}
}
