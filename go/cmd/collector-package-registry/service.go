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
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry/packageruntime"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary. It requires no live package registry credentials.
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
	source, err := packageruntime.NewClaimedSource(config.Source)
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
		CollectorKind:       scope.CollectorPackageRegistry,
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
		return "package-registry-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("package-registry-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
