// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/acr"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/dockerhub"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/gar"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/ghcr"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/harbor"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/jfrog"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/runtime"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared
// collector commit boundary. It requires no live OCI registry credentials.
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

func buildCollectorService(
	ctx context.Context,
	database db.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	_ = ctx
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return collector.Service{
		Source: &runtime.Source{
			Config:        config,
			ClientFactory: providerFactory{},
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
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return collector.ClaimedService{
		ControlStore: postgres.NewWorkflowControlStore(database),
		Source: runtime.ClaimedSource{
			Source: runtime.Source{
				Config:        config.OCI,
				ClientFactory: providerFactory{},
				Tracer:        tracer,
				Instruments:   instruments,
				Logger:        logger,
			},
		},
		Committer:           committer,
		CollectorKind:       scope.CollectorOCIRegistry,
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
		return "oci-registry-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("oci-registry-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

type providerFactory struct{}

func (providerFactory) Client(ctx context.Context, target runtime.TargetConfig) (runtime.RegistryClient, error) {
	// Resolve the per-target HTTP client once so every provider honors any
	// configured custom-CA trust (for example a localhost registry:2 over TLS).
	// The resolved tls_mode is a low-cardinality value safe to surface to an
	// operator; a misconfigured CA bundle fails loudly here rather than silently
	// falling back to the system trust pool.
	httpClient, _, err := target.HTTPClient()
	if err != nil {
		return nil, fmt.Errorf("resolve OCI registry TLS client (tls_mode=%s): %w", target.TLSMode(), err)
	}
	switch target.Provider {
	case oci.ProviderDockerHub:
		return dockerhub.NewDistributionClient(ctx, dockerhub.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
			Client:     httpClient,
		})
	case oci.ProviderGHCR:
		return ghcr.NewDistributionClient(ctx, ghcr.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
			Client:     httpClient,
		})
	case oci.ProviderJFrog:
		return jfrog.NewDistributionClient(jfrog.Config{
			BaseURL:       target.BaseURL,
			RepositoryKey: target.RepositoryKey,
			Username:      target.Username,
			Password:      target.Password,
			BearerToken:   target.BearerToken,
			Client:        httpClient,
		})
	case oci.ProviderECR:
		return newECRDistributionClient(ctx, target, httpClient)
	case oci.ProviderHarbor:
		return harbor.NewDistributionClient(harbor.Config{
			BaseURL:     target.Registry,
			Repository:  target.Repository,
			Username:    target.Username,
			Password:    target.Password,
			BearerToken: target.BearerToken,
			Client:      httpClient,
		})
	case oci.ProviderGoogleArtifactRegistry:
		return gar.NewDistributionClient(gar.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
			Client:       httpClient,
		})
	case oci.ProviderAzureContainerRegistry:
		return acr.NewDistributionClient(acr.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
			Client:       httpClient,
		})
	default:
		return nil, fmt.Errorf("unsupported OCI registry provider %q", target.Provider)
	}
}

func newECRDistributionClient(ctx context.Context, target runtime.TargetConfig, httpClient *http.Client) (runtime.RegistryClient, error) {
	options := make([]func(*awsconfig.LoadOptions) error, 0, 2)
	if target.Region != "" {
		options = append(options, awsconfig.WithRegion(target.Region))
	}
	if target.AWSProfile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(target.AWSProfile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for ECR: %w", err)
	}
	credentials, err := ecr.GetDistributionCredentials(ctx, awsecr.NewFromConfig(cfg))
	if err != nil {
		return nil, err
	}
	registryHost := target.RegistryHost
	if registryHost == "" {
		registryHost = target.Registry
	}
	return ecr.NewDistributionClient(registryHost, credentials.Username, credentials.Password, httpClient)
}
