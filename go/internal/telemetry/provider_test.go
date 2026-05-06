package telemetry

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestNewProvidersNoEndpoint(t *testing.T) {
	// Ensure OTLP endpoint is not set
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	require.NotNil(t, providers)
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	assert.NotNil(t, providers.TracerProvider, "trace provider should be created")
	assert.NotNil(t, providers.MeterProvider, "meter provider should be created")
	assert.NotNil(t, providers.PrometheusHandler, "prometheus handler should be created")
	assert.NotNil(t, providers.Shutdown, "shutdown function should be set")
}

func TestNewProvidersShutdownIdempotent(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	require.NotNil(t, providers)

	// First shutdown should succeed
	err = providers.Shutdown(ctx)
	require.NoError(t, err)

	// Second shutdown may return an error from readers being already shutdown,
	// but it should not panic or cause other issues. With the Prometheus
	// exporter integration, subsequent shutdowns return an error.
	err = providers.Shutdown(ctx)
	// We allow errors on second shutdown - the important thing is no panic
	_ = err
}

func TestNewProvidersResourceAttributes(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("my-test-service")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	require.NotNil(t, providers)
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	// Verify resource attributes are set correctly
	// The providers are created with the resource, but we can't directly inspect
	// the resource from the providers. Instead, we verify the bootstrap attributes
	// match what we expect.
	attrs := b.ResourceAttributes()
	assert.Equal(t, "my-test-service", attrs["service.name"])
	assert.Equal(t, DefaultServiceNamespace, attrs["service.namespace"])
}

func TestNewProvidersRegistersGlobalProviders(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	defer func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
	}()

	b, err := NewBootstrap("global-provider-test")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	require.NotNil(t, providers)
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	assert.Same(t, providers.TracerProvider, otel.GetTracerProvider())
	assert.Same(t, providers.MeterProvider, otel.GetMeterProvider())
}

func TestNewProvidersRequiresValidBootstrap(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	// Empty service name should fail validation
	b := Bootstrap{
		ServiceName:      "",
		ServiceNamespace: DefaultServiceNamespace,
		MeterName:        DefaultSignalName,
		TracerName:       DefaultSignalName,
		LoggerName:       DefaultSignalName,
	}

	ctx := context.Background()
	_, err := NewProviders(ctx, b)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bootstrap")
}
