// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func codeValueFlowStaleCleanupRunnerFor(
	database postgres.ExecQueryer,
	taintEvidence reducer.CodeTaintStaleEvidenceRetractor,
	taintWriter reducer.CodeTaintEvidenceWriter,
	interprocEvidence reducer.CodeInterprocStaleEvidenceRetractor,
	interprocWriter reducer.CodeInterprocEvidenceWriter,
	leaseManager reducer.PartitionLeaseManager,
	cfg codeValueFlowStaleCleanupConfig,
) *reducer.CodeValueFlowStaleCleanupRunner {
	if !cfg.Enabled {
		return nil
	}
	interprocLedger := postgres.NewCodeInterprocProjectedEdgeStore(database)
	taintLedger := postgres.NewCodeTaintEvidenceProjectedNodeStore(database)
	return &reducer.CodeValueFlowStaleCleanupRunner{
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
