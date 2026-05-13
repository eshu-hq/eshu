package main

import (
	"context"
	"fmt"
	"log/slog"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/acr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/dockerhub"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/gar"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ghcr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/harbor"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/jfrog"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ociruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func buildCollectorService(
	ctx context.Context,
	database postgres.ExecQueryer,
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
	return collector.Service{
		Source: &ociruntime.Source{
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

type providerFactory struct{}

func (providerFactory) Client(ctx context.Context, target ociruntime.TargetConfig) (ociruntime.RegistryClient, error) {
	switch target.Provider {
	case ociregistry.ProviderDockerHub:
		return dockerhub.NewDistributionClient(ctx, dockerhub.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
		})
	case ociregistry.ProviderGHCR:
		return ghcr.NewDistributionClient(ctx, ghcr.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
		})
	case ociregistry.ProviderJFrog:
		return jfrog.NewDistributionClient(jfrog.Config{
			BaseURL:       target.BaseURL,
			RepositoryKey: target.RepositoryKey,
			Username:      target.Username,
			Password:      target.Password,
			BearerToken:   target.BearerToken,
		})
	case ociregistry.ProviderECR:
		return newECRDistributionClient(ctx, target)
	case ociregistry.ProviderHarbor:
		return harbor.NewDistributionClient(harbor.Config{
			BaseURL:     target.Registry,
			Repository:  target.Repository,
			Username:    target.Username,
			Password:    target.Password,
			BearerToken: target.BearerToken,
		})
	case ociregistry.ProviderGoogleArtifactRegistry:
		return gar.NewDistributionClient(gar.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
		})
	case ociregistry.ProviderAzureContainerRegistry:
		return acr.NewDistributionClient(acr.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
		})
	default:
		return nil, fmt.Errorf("unsupported OCI registry provider %q", target.Provider)
	}
}

func newECRDistributionClient(ctx context.Context, target ociruntime.TargetConfig) (ociruntime.RegistryClient, error) {
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
	return ecr.NewDistributionClient(registryHost, credentials.Username, credentials.Password, nil)
}
