// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewProvidersWithOTLPEndpoint proves the OTLP exporter branches wire
// without a live collector: exporter creation is lazy, so no connection
// leaves the test process.
func TestNewProvidersWithOTLPEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "1000")

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
}
