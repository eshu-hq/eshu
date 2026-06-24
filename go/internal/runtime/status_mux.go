// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"net/http"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const defaultStatusReadinessTimeout = 3 * time.Second

// NewStatusAdminMux builds the shared status, metrics, recovery, and optional
// application routes for a long-running Go runtime.
func NewStatusAdminMux(
	serviceName string,
	reader statuspkg.Reader,
	appHandler http.Handler,
	opts ...StatusAdminOption,
) (*http.ServeMux, error) {
	var options statusAdminOptions
	for _, opt := range opts {
		opt(&options)
	}

	statusHandler, err := statuspkg.NewHTTPHandler(reader, statuspkg.HTTPHandlerOptions{})
	if err != nil {
		return nil, err
	}
	metricsHandler, err := NewStatusMetricsHandler(serviceName, reader)
	if err != nil {
		return nil, err
	}
	metricsHandler = NewCompositeMetricsHandler(metricsHandler, options.prometheusHandler)

	probes := make([]ReadinessProbe, 0, len(options.readinessProbes)+1)
	probes = append(probes, statusSnapshotReadinessProbe(reader, defaultStatusReadinessTimeout))
	probes = append(probes, options.readinessProbes...)

	adminMux, err := NewAdminMux(AdminMuxConfig{
		ServiceName:     serviceName,
		Ready:           combineReadinessProbes(probes),
		StatusHandler:   statusHandler,
		MetricsHandler:  metricsHandler,
		RecoveryHandler: options.recoveryHandler,
	})
	if err != nil {
		return nil, err
	}
	if appHandler != nil {
		adminMux.Handle("/", appHandler)
	}

	return adminMux, nil
}
