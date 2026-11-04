// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr/tesseract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

// drainFunc commits one full artifact-directory drain.
type drainFunc func(context.Context) (generations int, envelopes int, err error)

// buildDrain wires the pinned tesseract backend behind the directory source
// and returns the one-shot drain. Verification runs the binary's --version
// check and every digest pin first; any mismatch is a startup error, never a
// fallback to another engine. The committer is the normal ingestion store, so
// committed envelopes flow through the standard durable path unchanged.
func buildDrain(
	database db.ExecQueryer,
	cfg Config,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (drainFunc, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("document import collector is disabled")
	}
	engine, err := buildEngine(cfg.Tesseract, logger)
	if err != nil {
		return nil, err
	}
	src, err := newDirSource(cfg.ArtifactDir, sourceOptions{
		engine:        engine,
		sourceSystem:  cfg.SourceSystem,
		scopePrefix:   cfg.ScopePrefix,
		maxInputBytes: cfg.Tesseract.MaxInputBytes,
		maxArtifacts:  cfg.MaxArtifacts,
	}, logger)
	if err != nil {
		return nil, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return func(ctx context.Context) (int, int, error) {
		return drainOnce(ctx, src, committer, logger)
	}, nil
}

// buildEngine verifies the pinned tesseract installation and returns the
// backend. Any mismatch is a startup error, never a fallback to another
// engine: there is no default engine and no fixture fallback on this path.
func buildEngine(cfg tesseract.Config, logger *slog.Logger) (ocr.Engine, error) {
	engine, err := tesseract.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("verify pinned tesseract backend: %w", err)
	}
	if logger != nil {
		logger.Info("document import engine verified",
			"engine_version", engine.EngineVersion(),
		)
	}
	return engine, nil
}

var _ collector.Source = (*dirSource)(nil)
