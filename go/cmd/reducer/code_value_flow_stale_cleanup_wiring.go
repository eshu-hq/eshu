package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func codeValueFlowStaleCleanupRunnerFor(
	database postgres.ExecQueryer,
	taintEvidence reducer.CodeTaintStaleEvidenceRetractor,
	interprocEvidence reducer.CodeInterprocStaleEvidenceRetractor,
	leaseManager reducer.PartitionLeaseManager,
	cfg codeValueFlowStaleCleanupConfig,
) *reducer.CodeValueFlowStaleCleanupRunner {
	if !cfg.Enabled {
		return nil
	}
	return &reducer.CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: postgres.NewCodeValueFlowCurrentGenerationStore(database),
		TaintEvidence:      taintEvidence,
		InterprocEvidence:  interprocEvidence,
		LeaseManager:       leaseManager,
		Config:             cfg.Runner,
	}
}
