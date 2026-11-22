// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SourcePacer is the projector's pre-write source consult (#10882 F8): it
// answers whether a source is latched slowed so the projector can release a
// claimed item before any canonical or content write instead of repeating
// the write on every paced cycle and parking a worker in an admission wait.
// It reads the same shared latch and bounded per-source probe the admission
// writer's source leg does, and fails open the same way: an unreadable signal
// is counted and logged and reports not paced.
type SourcePacer struct {
	writer reducerAdmissionWriter
}

// ProjectorSourcePacer is the consult surface the projector service takes
// (projector.SourcePacer has the identical method set). NewSourcePacer returns
// it as an interface so a disabled leg is an untyped nil, never a typed nil
// pointer that would pass the projector's nil check.
type ProjectorSourcePacer interface {
	SourcePaced(ctx context.Context, sourceSystem string) (paced bool, retryAfter time.Duration)
}

// NewSourcePacer builds the pacer from the ESHU_SOURCE_BACKPRESSURE_* env. It
// returns nil with no error when the per-source leg is not configured, so a
// disabled leg adds no consult on the projector path. A partial threshold set
// is a startup error, like the admission writer.
func NewSourcePacer(
	database db.Queryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (ProjectorSourcePacer, error) {
	sourceConfig, err := loadSourceBackpressureConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !sourceConfig.Enabled() {
		return nil, nil
	}
	execQueryer, ok := database.(db.ExecQueryer)
	if !ok {
		return nil, errors.New("source pacer requires an exec-capable database handle for the shared latch")
	}
	store := postgres.NewWorkflowControlStore(execQueryer)
	store.Instruments = instruments
	return &SourcePacer{writer: reducerAdmissionWriter{
		sourceChecker: store,
		sourceConfig:  sourceConfig,
		instruments:   instruments,
		logger:        logger,
	}}, nil
}

// SourcePaced reports whether sourceSystem is slowed and the grant delay the
// verdict carries. An empty source, a consult failure, and a cancelled context
// all report not paced.
func (p *SourcePacer) SourcePaced(ctx context.Context, sourceSystem string) (bool, time.Duration) {
	sourceSystem = strings.TrimSpace(sourceSystem)
	if p == nil || sourceSystem == "" {
		return false, 0
	}
	decision, err := p.writer.sourceDecision(ctx, sourceSystem)
	if err != nil {
		return false, 0
	}
	if !decision.Slowed {
		return false, 0
	}
	return true, max(decision.RetryAfter, 0)
}
