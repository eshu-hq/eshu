// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive/clientgo"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// buildCollectorService wires the read-only Kubernetes live snapshot source
// onto the shared collector commit boundary.
func buildCollectorService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source: &kuberneteslive.Source{
			Config:        config.Collector,
			ClientFactory: clusterClientFactory{auth: config.Auth},
			Tracer:        tracer,
			Instruments:   instruments,
			Logger:        logger,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary. It requires no live Kubernetes credentials.
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

// clusterClientFactory builds one read-only client-go adapter per configured
// cluster target using the per-cluster auth settings.
type clusterClientFactory struct {
	auth map[string]clientgo.AuthConfig
}

func (f clusterClientFactory) Client(_ context.Context, target kuberneteslive.ClusterTarget) (kuberneteslive.Client, error) {
	auth, ok := f.auth[target.ClusterID]
	if !ok {
		return nil, fmt.Errorf("no auth config for cluster %q", target.ClusterID)
	}
	clientset, err := clientgo.NewClientset(auth)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client for cluster %q: %w", target.ClusterID, err)
	}
	return clientgo.NewAdapter(clientset), nil
}
