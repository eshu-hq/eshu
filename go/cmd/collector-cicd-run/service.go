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

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary so the golden-corpus gate can replay recorded
// ci.run/ci.artifact facts without live GitHub Actions credentials, mirroring
// collector-oci-registry's cassette mode.
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
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	config.Source.Tracer = tracer
	config.Source.Instruments = instruments
	source, err := ghactionsruntime.NewClaimedSource(config.Source)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorCICDRun,
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
		return "ci-cd-run-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("ci-cd-run-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
