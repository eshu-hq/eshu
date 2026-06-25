// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	cassette "github.com/eshu-hq/eshu/go/internal/collector/cassette"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

var newGCPADCLiveClient = gcpruntime.NewADCLiveClient

// buildCollectorService constructs the GCP cloud collector service from a
// declarative config document and an offline fixture page provider. The inner
// durable committer is the shared Postgres ingestion store, wrapped by the GCP
// status committer so commit outcomes record the bounded claim metric.
//
// Fixture mode always uses the offline FixturePageProvider. Claimed-live mode is
// built by buildClaimedService so live transport stays explicit and
// workflow-owned.
func buildCollectorService(
	database postgres.SQLDB,
	configPath string,
	redactionKey redact.Key,
	tracer trace.Tracer,
	meter metric.Meter,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	source, runtimeCfg, gcpMetrics, err := buildSource(configPath, redactionKey, meter, logger)
	if err != nil {
		return collector.Service{}, err
	}

	ingestion := postgres.NewIngestionStore(database)
	ingestion.Logger = logger
	committer := newGCPStatusCommitter(ingestion, gcpMetrics)

	return collector.Service{
		Source:       source,
		Committer:    committer,
		PollInterval: pollInterval(runtimeCfg),
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}

func buildClaimedService(
	ctx context.Context,
	database postgres.ExecQueryer,
	redactionKey redact.Key,
	getenv func(string) string,
	tracer trace.Tracer,
	meter metric.Meter,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	liveClient, err := newGCPADCLiveClient(ctx, config.CredentialRef)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	gcpMetrics, err := gcpcloud.NewMetrics(meter)
	if err != nil {
		return collector.ClaimedService{}, fmt.Errorf("gcp collector metrics: %w", err)
	}
	ingestion := postgres.NewIngestionStore(database)
	ingestion.Logger = logger
	committer := newGCPStatusCommitter(ingestion, gcpMetrics)
	return collector.ClaimedService{
		ControlStore: postgres.NewWorkflowControlStore(database),
		Source: &gcpruntime.Source{
			Config:       config.Source,
			Provider:     liveClient,
			RedactionKey: redactionKey,
			Metrics:      gcpMetrics,
			Logger:       logger,
		},
		Committer:           committer,
		CollectorKind:       scope.CollectorGCP,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		MaxAttempts:         workflow.DefaultClaimMaxAttempts(),
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "gcp-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("gcp-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

// buildSource builds the fixture-backed GCP source and its scoped metrics from a
// declarative config document. It is shared by the service wiring and the smoke
// test so both exercise identical construction.
func buildSource(
	configPath string,
	redactionKey redact.Key,
	meter metric.Meter,
	logger *slog.Logger,
) (*gcpruntime.Source, gcpruntime.Config, *gcpcloud.Metrics, error) {
	fileCfg, err := loadFileConfig(configPath)
	if err != nil {
		return nil, gcpruntime.Config{}, nil, err
	}
	runtimeCfg, err := fileCfg.runtimeConfig()
	if err != nil {
		return nil, gcpruntime.Config{}, nil, err
	}
	provider, err := gcpruntime.NewFixturePageProviderFromFiles(fileCfg.fixtureFiles(runtimeCfg))
	if err != nil {
		return nil, gcpruntime.Config{}, nil, err
	}
	gcpMetrics, err := gcpcloud.NewMetrics(meter)
	if err != nil {
		return nil, gcpruntime.Config{}, nil, fmt.Errorf("gcp collector metrics: %w", err)
	}
	source := &gcpruntime.Source{
		Config:       runtimeCfg,
		Provider:     provider,
		RedactionKey: redactionKey,
		Metrics:      gcpMetrics,
		Logger:       logger,
	}
	return source, runtimeCfg, gcpMetrics, nil
}

func pollInterval(cfg gcpruntime.Config) time.Duration {
	if cfg.PollInterval > 0 {
		return cfg.PollInterval
	}
	return gcpruntime.DefaultPollInterval
}

// buildCassetteService constructs the GCP cloud collector service from a
// pre-recorded cassette file. The cassette source replays recorded API
// responses without any live credentials or redaction key material.
func buildCassetteService(
	database postgres.ExecQueryer,
	cassettePath string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return collector.Service{}, fmt.Errorf("load cassette: %w", err)
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source:       src,
		Committer:    committer,
		PollInterval: 24 * time.Hour,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}
