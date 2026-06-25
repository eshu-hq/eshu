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
	"github.com/eshu-hq/eshu/go/internal/collector/cassette"
	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive/vaultapi"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary. It requires no live Vault credentials.
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
	source, err := vaultlive.NewClaimedSource(vaultlive.ClaimedSourceConfig{
		Config:        config.Collector,
		ClientFactory: vaultClientFactory{auth: config.Auth, instruments: instruments},
		Tracer:        tracer,
		Instruments:   instruments,
		Logger:        logger,
	})
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorVaultLive,
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
		return "vault-live-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("vault-live-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

// vaultAuth holds the read-only connection settings for one Vault target. The
// token is resolved from the environment (never from the targets JSON) so a
// secret never lands in serialized config.
type vaultAuth struct {
	Address   string
	Token     string
	Namespace string
}

// vaultClientFactory builds one read-only, metadata-only Vault adapter per
// configured target using the per-target connection settings. The auth map is
// keyed by (cluster, namespace) because the scope identity is namespace-scoped:
// one cluster may host multiple namespace targets with distinct tokens.
type vaultClientFactory struct {
	auth        map[string]vaultAuth
	instruments *telemetry.Instruments
}

// authKey is the (cluster, namespace) key shared by config parsing and the
// client factory so the namespace dimension of the scope identity is honored
// end to end.
func authKey(clusterID, namespace string) string {
	return clusterID + "\x00" + namespace
}

func (f vaultClientFactory) Client(_ context.Context, target vaultlive.ClusterTarget) (vaultlive.Client, error) {
	scopeID, err := vaultlive.VaultScopeID(target.VaultClusterID, target.Namespace)
	if err != nil {
		return nil, fmt.Errorf("build vault target scope_id: %w", err)
	}
	auth, ok := f.auth[authKey(target.VaultClusterID, target.Namespace)]
	if !ok {
		return nil, fmt.Errorf("no auth config for vault target scope_id %q", scopeID)
	}
	client, err := vaultapi.New(vaultapi.Config{
		Address:   auth.Address,
		Token:     auth.Token,
		Namespace: auth.Namespace,
		OnAPICall: f.apiCallObserver(),
	})
	if err != nil {
		return nil, fmt.Errorf("build vault client for vault target scope_id %q: %w", scopeID, err)
	}
	return client, nil
}

// apiCallObserver records each Vault list operation outcome to the
// secrets/IAM source api-call counter (source="vault"). Labels are bounded
// enums only — no path, token, or address. Returns nil when no instruments are
// wired.
func (f vaultClientFactory) apiCallObserver() func(operation, result string) {
	if f.instruments == nil {
		return nil
	}
	instruments := f.instruments
	return func(operation, result string) {
		instruments.SecretsIAMSourceAPICalls.Add(context.Background(), 1, metric.WithAttributes(
			telemetry.AttrSource("vault"),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
	}
}
