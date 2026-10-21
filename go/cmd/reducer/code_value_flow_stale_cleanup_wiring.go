// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/cleanup"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/taint"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func codeValueFlowStaleCleanupRunnerFor(
	database db.ExecQueryer,
	taintEvidence cleanup.TaintStaleEvidenceRetractor,
	taintWriter taint.EvidenceWriter,
	interprocEvidence cleanup.InterprocStaleEvidenceRetractor,
	interprocWriter taint.InterprocEvidenceWriter,
	leaseManager sharedintent.PartitionLeaseManager,
	cfg codeValueFlowStaleCleanupConfig,
) *cleanup.Runner {
	if !cfg.Enabled {
		return nil
	}
	interprocLedger := taintstore.NewCodeInterprocProjectedEdgeStore(database)
	taintLedger := taintstore.NewCodeTaintEvidenceProjectedNodeStore(database)
	return &cleanup.Runner{
		CurrentGenerations: postgres.NewCodeValueFlowCurrentGenerationStore(database),
		TaintEvidence:      taintEvidence,
		TaintWriter:        taintWriter,
		TaintLedger:        taintLedger,
		InterprocEvidence:  interprocEvidence,
		InterprocWriter:    interprocWriter,
		InterprocLedger:    interprocLedger,
		LeaseManager:       leaseManager,
		Config:             cfg.Runner,
	}
}
