// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
)

func newValueFlowFixpointProjector(
	summaryLoader value.FunctionSummarySnapshotLoader,
	sourceLoader value.FunctionSourceSnapshotLoader,
	graphIDLoader value.FunctionGraphIDSnapshotLoader,
	componentStore value.FixpointComponentStore,
	graphReader reducer.GraphQueryRunner,
	writer taint.InterprocEvidenceWriter,
	logger *slog.Logger,
) value.FixpointEvidenceProjector {
	return value.FixpointEvidenceProjector{
		Loader: value.FixpointEvidenceLoader{
			SummarySnapshotLoader:   summaryLoader,
			SourceSnapshotLoader:    sourceLoader,
			GraphIDSnapshotLoader:   graphIDLoader,
			CloudSinkSnapshotLoader: value.GraphCloudSinkTargetLoader{Graph: graphReader, Logger: logger},
			FixpointCache:           value.NewFixpointCache(),
			FixpointComponentStore:  componentStore,
			Logger:                  logger,
		},
		Writer: writer,
	}
}
