package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func newValueFlowFixpointProjector(
	summaryLoader reducer.FunctionSummarySnapshotLoader,
	sourceLoader reducer.FunctionSourceSnapshotLoader,
	graphIDLoader reducer.FunctionGraphIDSnapshotLoader,
	graphReader reducer.GraphQueryRunner,
	writer reducer.CodeInterprocEvidenceWriter,
	logger *slog.Logger,
) reducer.ValueFlowFixpointEvidenceProjector {
	return reducer.ValueFlowFixpointEvidenceProjector{
		Loader: reducer.ValueFlowFixpointEvidenceLoader{
			SummarySnapshotLoader:   summaryLoader,
			SourceSnapshotLoader:    sourceLoader,
			GraphIDSnapshotLoader:   graphIDLoader,
			CloudSinkSnapshotLoader: reducer.GraphValueFlowCloudSinkTargetLoader{Graph: graphReader},
			FixpointCache:           reducer.NewValueFlowFixpointCache(),
			Logger:                  logger,
		},
		Writer: writer,
	}
}
