// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// runRecord is the -mode=record entrypoint: one credentialed pass over every
// configured (account, region, service) tuple, written as a pseudonymized
// canonical cassette. It needs the same ESHU_COLLECTOR_INSTANCES_JSON as
// claimed-live plus ESHU_RECORD_PSEUDONYM_KEY, and no database.
func runRecord(
	parent context.Context,
	cassettePath string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	config, err := loadRuntimeConfig(os.Getenv)
	if err != nil {
		return err
	}
	key, err := loadRecordPseudonymKey(os.Getenv)
	if err != nil {
		return err
	}
	source := buildRecordSource(config, tracer, instruments)

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return recordCassette(ctx, source, cassettePath, key, logger)
}

// buildRecordSource is the claimed-live source wiring without its three
// Postgres-backed collaborators (limiter, pagination checkpoints, scan
// status), all of which runtime.ClaimedSource tolerates as nil. It shares
// buildClaimedService's credential provider and scanner factory so a
// recording exercises the production scan path; the test
// TestRecordSourceIsClaimedLiveWiringMinusStores pins the two together.
func buildRecordSource(
	config runtimeConfig,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *runtime.RecordSource {
	return &runtime.RecordSource{Claimed: runtime.ClaimedSource{
		Config:      config.AWS,
		Credentials: runtime.SDKCredentialProvider{},
		Scanners: runtime.DefaultScannerFactory{
			Tracer:       tracer,
			Instruments:  instruments,
			RedactionKey: config.AWSRedactionKey,
		},
		Tracer:      tracer,
		Instruments: instruments,
	}}
}

// recordCassette drives the recorder with pseudonymization required and logs
// the three record events. The pseudonymized event carries the report's
// counts, field paths and key fingerprint -- never a value, never the key.
func recordCassette(
	ctx context.Context,
	source collector.Source,
	cassettePath string,
	key recordpseudo.Key,
	logger *slog.Logger,
) error {
	logger.Info("recording cassette", telemetry.EventAttr("collector.record.started"), "path", cassettePath, "key_fingerprint", key.Fingerprint())
	err := recorder.Run(ctx, source, recorder.Options{
		Path:                    cassettePath,
		CollectorLabel:          string(scope.CollectorAWS),
		Pseudonymize:            &recordpseudo.Config{Key: key, Policy: recordpolicy.Policy()},
		RequirePseudonymization: true,
		OnPseudonymized: func(report recordpseudo.Report) {
			logger.Info("pseudonymized recording", append([]any{telemetry.EventAttr("collector.record.pseudonymized")}, report.LogAttrs()...)...)
		},
	})
	if err != nil {
		return fmt.Errorf("record cassette: %w", err)
	}
	logger.Info("recorded cassette", telemetry.EventAttr("collector.record.completed"), "path", cassettePath, "key_fingerprint", key.Fingerprint())
	return nil
}
