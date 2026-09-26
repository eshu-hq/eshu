// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newGrantObserver builds the producer-grant decision observer the
// coordinator hands to config loading. It is the same adapter the extension
// worker uses, so both processes emit the same counter, span event, and log
// keys; an operator separates them by the OTEL service.name resource
// attribute ("workflow-coordinator" here). Only the readback and activation
// stages fire in this process.
func newGrantObserver(instruments *telemetry.Instruments, logger *slog.Logger) component.GrantObserver {
	return extensionhost.NewGrantTelemetry(instruments, logger)
}
