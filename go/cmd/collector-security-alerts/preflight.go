package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	preflightProviderAccessFlag = "--preflight-provider-access"
	preflightServiceName        = "collector-security-alerts-preflight"
)

type providerAccessPreflightTelemetry struct {
	tracer      trace.Tracer
	instruments *telemetry.Instruments
	shutdown    func(context.Context) error
}

func hasProviderAccessPreflightFlag(args []string) bool {
	for _, arg := range args {
		if arg == preflightProviderAccessFlag {
			return true
		}
	}
	return false
}

func runProviderAccessPreflight(ctx context.Context, getenv func(string) string) error {
	return runProviderAccessPreflightWithFactory(ctx, getenv, nil)
}

func runProviderAccessPreflightWithFactory(
	ctx context.Context,
	getenv func(string) string,
	factory alertruntime.ClientFactory,
) error {
	signals, err := newProviderAccessPreflightTelemetry(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = signals.shutdown(context.Background())
	}()
	return runProviderAccessPreflightWithSignals(ctx, getenv, factory, signals.tracer, signals.instruments)
}

func runProviderAccessPreflightWithSignals(
	ctx context.Context,
	getenv func(string) string,
	factory alertruntime.ClientFactory,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return err
	}
	config.Source.ClientFactory = factory
	config.Source.Tracer = tracer
	config.Source.Instruments = instruments
	source, err := alertruntime.NewClaimedSource(config.Source)
	if err != nil {
		return err
	}
	result, err := source.PreflightProviderAccess(ctx)
	if err != nil {
		return fmt.Errorf("security alert provider access preflight failed for %d target(s): %w", result.TargetCount, err)
	}
	return nil
}

func newProviderAccessPreflightTelemetry(ctx context.Context) (providerAccessPreflightTelemetry, error) {
	bootstrap, err := telemetry.NewBootstrap(preflightServiceName)
	if err != nil {
		return providerAccessPreflightTelemetry{}, fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		return providerAccessPreflightTelemetry{}, fmt.Errorf("telemetry providers: %w", err)
	}
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		_ = providers.Shutdown(context.Background())
		return providerAccessPreflightTelemetry{}, fmt.Errorf("telemetry instruments: %w", err)
	}
	return providerAccessPreflightTelemetry{
		tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		instruments: instruments,
		shutdown:    providers.Shutdown,
	}, nil
}
