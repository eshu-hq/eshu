// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// newCICDRunCorrelationWriter builds the ci_cd correlation writer with its
// completion change gate (#10972 PR-4): the reducer database's transaction
// beginner, so the write computes its changed verdict in one transaction, and
// the completion-owed marker fenced on the work queue's lease owner. A
// database without a beginner keeps the autocommit write and no verdict, so
// every ack emits as before.
func newCICDRunCorrelationWriter(database db.ExecQueryer) cicdrun.PostgresCICDRunCorrelationWriter {
	writer := cicdrun.PostgresCICDRunCorrelationWriter{DB: database}
	if beginner := reducerBeginner(database); beginner != nil {
		writer.Beginner = beginner
		writer.CompletionOwed = postgres.CICDCompletionOwedMarker{LeaseOwner: reducerWorkQueueLeaseOwner}
	}
	return writer
}
