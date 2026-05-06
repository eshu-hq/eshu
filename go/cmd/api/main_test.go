package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintAPIVersionFlagReturnsBeforeRuntimeStartup(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-api"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout bytes.Buffer
	handled, err := printAPIVersionFlag([]string{"--version"}, &stdout)
	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, "eshu-api v1.2.3-api\n", stdout.String())
}

func TestNewLoggerOutputsStructuredJSON(t *testing.T) {
	t.Parallel()

	bootstrap, err := telemetry.NewBootstrap("eshu-api")
	require.NoError(t, err)

	var buf bytes.Buffer
	logger := newLogger(bootstrap, &buf)

	logger.Info("api starting", slog.String("listen_addr", ":8080"))

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))

	assert.Equal(t, "eshu-api", logEntry["service_name"])
	assert.Equal(t, "eshu", logEntry["service_namespace"])
	assert.Equal(t, "api", logEntry["component"])
	assert.Equal(t, "api", logEntry["runtime_role"])
	assert.Equal(t, "api starting", logEntry["message"])
	assert.Equal(t, "INFO", logEntry["severity_text"])
	assert.Equal(t, ":8080", logEntry["listen_addr"])
}
