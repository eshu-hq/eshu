// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func newValueFlowFixpointProjector(
	summaryLoader reducer.FunctionSummarySnapshotLoader,
	sourceLoader reducer.FunctionSourceSnapshotLoader,
	graphIDLoader reducer.FunctionGraphIDSnapshotLoader,
	componentStore reducer.ValueFlowFixpointComponentStore,
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
			FixpointComponentStore:  componentStore,
			Logger:                  logger,
		},
		Writer: writer,
	}
}
