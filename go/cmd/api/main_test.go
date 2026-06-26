// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

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

func TestAPIShutdownTimeoutDefault30s(t *testing.T) {
	t.Parallel()

	d := apiShutdownTimeout(nil)
	assert.Equal(t, 30*time.Second, d, "default shutdown timeout should be 30s")
}

func TestAPIShutdownTimeoutExplicit5sHonored(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ESHU_API_SHUTDOWN_TIMEOUT" {
			return "5s"
		}
		return ""
	}
	d := apiShutdownTimeout(getenv)
	assert.Equal(t, 5*time.Second, d, "explicit 5s should be honored")
}

func TestAPIShutdownTimeoutInvalidValueFallsBackToDefault(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ESHU_API_SHUTDOWN_TIMEOUT" {
			return "not-a-duration"
		}
		return ""
	}
	d := apiShutdownTimeout(getenv)
	assert.Equal(t, 30*time.Second, d, "invalid value should fall back to 30s default")
}

func TestAPIShutdownTimeoutEmptyValueUsesDefault(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "" }
	d := apiShutdownTimeout(getenv)
	assert.Equal(t, 30*time.Second, d, "empty value should use 30s default")
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
