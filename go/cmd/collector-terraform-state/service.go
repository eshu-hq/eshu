// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary. It requires no live Terraform state credentials.
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
	committer.Instruments = instruments
	return collector.Service{
		Source:       src,
		Committer:    committer,
		PollInterval: 24 * time.Hour,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}

func buildClaimedService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	discoveryMetrics terraformstate.DiscoveryMetrics,
	logger *slog.Logger,
	meter metric.Meter,
) (collector.ClaimedService, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	discoveryConfig, err := terraformstate.ParseDiscoveryConfig(config.Instance.Configuration)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	// Register the schema-resolver entry-count gauge so operators can size
	// the collector pod for the startup-loaded provider-schema bundle. The
	// resolver is loaded once at startup and held for the lifetime of the
	// process; the gauge reports the per-process footprint.
	if counter, ok := config.SchemaResolver.(terraformstate.SchemaResolverEntryCounter); ok {
		if err := telemetry.RegisterTfstateSchemaResolverEntries(meter, counter.EntryCount); err != nil {
			return collector.ClaimedService{}, fmt.Errorf("register tfstate schema resolver entries gauge: %w", err)
		}
	}

	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments

	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config:         discoveryConfig,
			GitReadiness:   postgres.TerraformStateGitReadinessChecker{DB: database},
			BackendFacts:   postgres.TerraformStateBackendFactReader{DB: database},
			PriorSnapshots: postgres.TerraformStatePriorSnapshotReader{DB: database},
			Tracer:         tracer,
			Metrics:        discoveryMetrics,
		},
		SourceFactory: newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
			DefaultCredentials:      config.AWSCredentials,
			TargetScopes:            config.AWSTargetScopes,
			S3FallbackLockTableName: config.AWSDynamoDBLockTable,
			MaxBytes:                config.SourceMaxBytes,
		}),
		RedactionKey:   config.RedactionKey,
		RedactionRules: config.RedactionRules,
		SchemaResolver: config.SchemaResolver,
		Tracer:         tracer,
		Instruments:    instruments,
		Logger:         logger,
	}

	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorTerraformState,
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
		return "tfstate-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("tfstate-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
