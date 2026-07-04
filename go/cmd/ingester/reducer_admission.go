// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reduceradmission"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ingesterReducerIntentWriter builds the ingester's reducer intent writer:
// the local-lightweight bypass (ingester-only; bootstrap-index has no
// equivalent concept) takes precedence, then the shared
// internal/reduceradmission admission gate wraps the real
// postgres.NewReducerQueue writer. See internal/reduceradmission for the
// two-gate admission policy shared with bootstrap-index (issue #4515 parity).
func ingesterReducerIntentWriter(
	database postgres.ExecQueryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.ReducerIntentWriter, error) {
	writer := reducerIntentWriterForProfile(getenv, postgres.NewReducerQueue(database, "ingester", time.Minute))
	if ingesterLocalLightweight(getenv) {
		return writer, nil
	}
	return reduceradmission.WrapIntentWriter(database, writer, getenv, instruments, logger)
}
