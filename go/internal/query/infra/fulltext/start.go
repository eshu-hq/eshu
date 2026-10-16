// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Start builds the process Reader from the ESHU_INFRA_SEARCH_KEY_READ value
// and, on Neo4j with a graph runner, starts the readiness refresher, which
// probes now and every DefaultRefreshInterval until ctx is done. The
// refresher runs with the flag off too, so operators can read the readiness
// gauges before turning the flag on. An invalid flag value is an error.
func Start(
	ctx context.Context,
	mode string,
	neo4j bool,
	runner Runner,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (*Reader, error) {
	enabled, err := ParseReadMode(mode)
	if err != nil {
		return nil, err
	}
	reader := &Reader{Enabled: enabled, Logger: logger, Instruments: instruments}
	if neo4j && runner != nil {
		reader.Readiness = NewReadiness(runner, logger, instruments)
		go reader.Readiness.Watch(ctx, DefaultRefreshInterval)
	}
	return reader, nil
}
