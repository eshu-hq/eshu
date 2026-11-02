// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

// serviceTemplate renders a collector command's service.go: the cassette
// service, the claimed service with its claim store selection through
// go/internal/collector/claim/store (#8875), and the claim id helper.
const serviceTemplate = managedHeader + `package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"{{.Source.ImportPath}}"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	{{- if .MaxAttemptsExpr }}
	"github.com/eshu-hq/eshu/go/internal/workflow"
	{{- end }}
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared collector commit boundary.
func buildCassetteService(
	database db.ExecQueryer,
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

func buildClaimedService(
	database db.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	{{.Source.ConfigAttacher}}(&config.Source, tracer, instruments)
	source, err := {{.Source.Constructor}}(config.Source)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	claimStore, err := store.New(context.Background(), store.Options{
		Service:     "{{.RuntimeName}}",
		Getenv:      getenv,
		Postgres:    postgres.NewWorkflowControlStore(database),
		Logger:      logger,
		Instruments: instruments,
	})
	if err != nil {
		return collector.ClaimedService{}, err
	}
	return collector.ClaimedService{
		ControlStore:        claimStore.Store,
		Source:              source,
		Committer:           committer,
		CollectorKind:       {{.CollectorKindExpr}},
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		{{- if .MaxAttemptsExpr }}
		MaxAttempts:         {{.MaxAttemptsExpr}},
		{{- end }}
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "{{.ClaimIDPrefix}}-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("{{.ClaimIDPrefix}}-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

func {{.Source.ConfigAttacher}}(config *{{.Source.ConfigType}}, tracer trace.Tracer, instruments *telemetry.Instruments) {
	config.Tracer = tracer
	config.Instruments = instruments
}
`
