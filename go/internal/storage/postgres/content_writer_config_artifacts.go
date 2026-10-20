// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ConfigArtifactDeriver is the port through which ContentWriter.Write derives
// the written repository's stored config-artifact rows (#8155, decision
// record section 3.1). *artifacts.Deriver is the production implementation;
// tests substitute a fake. The derive never fails the Write: a failed derive
// leaves the state version behind, so readers fall back live and the reducer
// walker repairs the repository later.
type ConfigArtifactDeriver interface {
	DeriveRepository(context.Context, artifacts.Request) artifacts.Result
}

// WithConfigArtifactDeriver returns a copy that derives the written
// repository's config-artifact rows as the last step of Write, after the
// infra read-model derive succeeds. A nil deriver (the default) skips the
// derive: the binary did not wire one because the derive is disabled or the
// read model is not installed.
func (w ContentWriter) WithConfigArtifactDeriver(deriver ConfigArtifactDeriver) ContentWriter {
	w.configArtifactDeriver = deriver
	return w
}

// WithConfigArtifactTracer returns a copy whose config-artifact derive
// records the config_artifacts.derive span. Other Write stages emit no span
// through this tracer; a nil tracer records none.
func (w ContentWriter) WithConfigArtifactTracer(tracer trace.Tracer) ContentWriter {
	w.configArtifactTracer = tracer
	return w
}

// deriveConfigArtifacts derives materialization's repository as the last step
// of Write, after every content statement of the Write committed and the
// infra derive succeeded, so the derive reads the version the fence bumped
// for this Write (protocol section 3). It never fails the Write: a derive
// error, panic or timeout is counted on derives_total, logged by the Deriver
// at ERROR, and recorded on the state row, and the content Write still
// succeeds.
func (w ContentWriter) deriveConfigArtifacts(ctx context.Context, materialization content.Materialization) {
	if w.configArtifactDeriver == nil {
		return
	}
	start := time.Now()
	deriveCtx := ctx
	var span trace.Span
	if w.configArtifactTracer != nil {
		// The derive span is the parent of the derive's Postgres statements:
		// the instrumented pool starts each as a child of the span context
		// it runs with.
		deriveCtx, span = w.configArtifactTracer.Start(ctx, telemetry.SpanConfigArtifactsDerive)
		defer span.End()
	}
	result := w.configArtifactDeriver.DeriveRepository(deriveCtx, artifacts.Request{
		RepoID:       materialization.RepoID,
		ScopeID:      materialization.ScopeID,
		GenerationID: materialization.GenerationID,
		Trigger:      artifacts.TriggerWrite,
	})
	if span != nil {
		span.SetAttributes(
			attribute.String("repo_id", materialization.RepoID),
			telemetry.AttrTrigger(string(result.Trigger)),
			telemetry.AttrOutcome(string(result.Outcome)),
		)
	}
	w.recordConfigArtifactDerive(ctx, result)
	attrs := []any{
		"trigger", string(result.Trigger),
		"outcome", string(result.Outcome),
		"row_count", result.Rows,
		"content_version", result.ContentVersion,
	}
	if result.ErrorClass != "" {
		attrs = append(attrs, "error_class", result.ErrorClass)
	}
	w.logStage(ctx, materialization, "derive_config_artifacts", start, attrs...)
}

// recordConfigArtifactDerive counts one Write derive on derives_total and
// observes its wall time on the derive duration histogram. Nil instruments
// record nothing.
func (w ContentWriter) recordConfigArtifactDerive(ctx context.Context, result artifacts.Result) {
	if w.instruments == nil || w.instruments.ConfigArtifactDerives == nil {
		return
	}
	w.instruments.ConfigArtifactDerives.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrTrigger(string(result.Trigger)),
		telemetry.AttrOutcome(string(result.Outcome)),
	))
	if w.instruments.ConfigArtifactDeriveDuration == nil {
		return
	}
	w.instruments.ConfigArtifactDeriveDuration.Record(ctx, result.Duration.Seconds(), metric.WithAttributes(
		telemetry.AttrTrigger(string(result.Trigger)),
	))
}

// NewConfigArtifactWriteDeriver builds the Deriver that a content-writing
// binary (ingester, projector, bootstrap-index) attaches to its ContentWriter
// with WithConfigArtifactDeriver. It loads the derive settings beside
// LoadWriterConfig, runs the process-start encoding and fence checks once over
// database, and supplies contentartifacts.ExtractorVersion, the guard-pinned
// extractor version, and no other constant. database must be a pool: the
// derive lists and reads with several statements in flight and opens its own
// write transaction.
//
// It returns a nil deriver without an error when the derive is disabled
// (logging config_artifacts.derive.disabled once): the derive stays off and
// reads stay live. When database cannot open a transaction (it is not an
// artifacts.Database) it fails startup instead of attaching a deriver that
// could never write. A refused check (non-UTF8 encoding, missing fence)
// still returns a deriver: it answers every derive with encoding_unsupported
// or fence_missing without a statement, counted on derives_total. An invalid
// derive setting fails startup, so the process never derives with a deadline
// it did not ask for.
func NewConfigArtifactWriteDeriver(
	ctx context.Context,
	database db.ExecQueryer,
	getenv func(string) string,
	logger *slog.Logger,
) (ConfigArtifactDeriver, error) {
	cfg, err := content.LoadConfigArtifactDeriveConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		if logger != nil {
			logger.InfoContext(ctx, "config artifact derive disabled; content Writes skip the derive",
				"event_name", "config_artifacts.derive.disabled")
		}
		return nil, nil
	}
	pool, ok := database.(artifacts.Database)
	if !ok {
		return nil, fmt.Errorf("config artifact derive: content database does not open transactions")
	}
	encoding := artifacts.CheckEncoding(ctx, database, logger)
	fence := artifacts.CheckFence(ctx, database, logger)
	return artifacts.NewDeriver(pool, artifacts.DeriverConfig{
		ExtractorVersion: contentartifacts.ExtractorVersion,
		Encoding:         encoding,
		Fence:            fence,
		Timeout:          cfg.Timeout,
		Logger:           logger,
	})
}
