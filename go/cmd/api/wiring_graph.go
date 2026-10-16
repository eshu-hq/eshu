// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// openGraphReader builds the API query layer's graph reader from an open
// driver, decorating the read seam with differential capture when a session
// is open, and runs the startup backfills on graph-enabled profiles.
// Undriven readers stay undecorated so lightweight profiles keep their
// graph-free responses. It is split out of wiring.go to keep that file
// under the 500-line cap.
func openGraphReader(
	ctx context.Context,
	rawDB *sql.DB,
	driver neo4jdriver.DriverWithContext,
	neo4jDB string,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	captureSession *capture.Session,
) (query.GraphQuery, error) {
	// Construct the universal graph-read policy only after instruments exist.
	// The startup owner-ledger backfill uses this same bounded reader, so a slow
	// graph cannot hold process startup beyond the per-read budget.
	neo4jReader := query.NewNeo4jReader(
		driver,
		neo4jDB,
		query.WithNeo4jReaderObservability(logger, instruments),
	)
	// Capture the read seam when a session is open. Undriven readers stay
	// undecorated so lightweight profiles keep their graph-free responses.
	graphReader := captureSession.ReaderIfConfigured(neo4jReader)
	// #5563 upgrade gate: seed pre-ledger CloudResource graph rows before the
	// indexed owner-ledger list path is mounted, then start the #6793 infra read
	// model backfill in the background. Graph-disabled profiles skip both.
	if driver != nil {
		if err := query.RunStartupBackfills(ctx, rawDB, graphReader, logger, instruments); err != nil {
			return nil, fmt.Errorf("backfill cloud resource owner ledger: %w", err)
		}
	}
	return graphReader, nil
}

// openQueryGraph opens the Neo4j/NornicDB driver the API query layer reads
// from, honoring the local-lightweight profile and the ESHU_DISABLE_NEO4J
// escape hatch (both return a nil driver and the default database name so the
// caller can run graph-free). It is split out of wiring.go to keep that file
// under the 500-line cap.
func openQueryGraph(
	ctx context.Context,
	getenv func(string) string,
	queryProfile query.QueryProfile,
	logger *slog.Logger,
) (neo4jdriver.DriverWithContext, string, error) {
	neo4jDB := envOrDefault(getenv, "DEFAULT_DATABASE", "nornic")
	if queryProfile == query.ProfileLocalLightweight || strings.EqualFold(envOrDefault(getenv, "ESHU_DISABLE_NEO4J", ""), "true") {
		return nil, neo4jDB, nil
	}

	driver, cfg, err := internalruntime.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, "", err
	}
	if logger != nil {
		logger.Info("neo4j connected", telemetry.EventAttr("runtime.neo4j.connected"), slog.String("neo4j_uri", cfg.URI))
	}
	return driver, cfg.DatabaseName, nil
}

// startInfraSearchKeyRead wires the #7245 infra search full-text read onto
// the infra handler: it parses ESHU_INFRA_SEARCH_KEY_READ (an invalid value
// fails startup) and, on a driven Neo4j graph, starts the readiness refresher
// for the life of ctx. NornicDB and graph-free profiles get a Reader that
// always serves the legacy statement.
func startInfraSearchKeyRead(
	ctx context.Context,
	getenv func(string) string,
	infra *query.InfraHandler,
	graphBackend query.GraphBackend,
	graphDriven bool,
	graphReader query.GraphQuery,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) error {
	if infra == nil {
		return nil
	}
	reader, err := fulltext.Start(ctx, getenv(fulltext.ReadEnv),
		graphBackend == query.GraphBackendNeo4j && graphDriven, graphReader, logger, instruments)
	if err != nil {
		return fmt.Errorf("configure infra search key read: %w", err)
	}
	infra.SearchKey = reader
	return nil
}
