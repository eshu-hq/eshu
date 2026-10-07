// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const defaultStatusReadinessTimeout = 3 * time.Second

// checkReaderStartup fails a status constructor when the reader reports an
// invalid configuration (see statuspkg.ReaderStartupError). Every runtime that
// serves a status endpoint builds it through NewStatusAdminMux or
// NewStatusMetricsHandler, directly or through app.MountStatusServer, so the
// check lives here and a runtime that mounts its own mux cannot skip it (#7009).
func checkReaderStartup(reader statuspkg.Reader) error {
	if err := statuspkg.ReaderStartupError(reader); err != nil {
		return fmt.Errorf("status reader configuration: %w", err)
	}
	return nil
}

// NewStatusAdminMux builds the shared status, metrics, recovery, and optional
// application routes for a long-running Go runtime.
func NewStatusAdminMux(
	serviceName string,
	reader statuspkg.Reader,
	appHandler http.Handler,
	opts ...StatusAdminOption,
) (*http.ServeMux, error) {
	if err := checkReaderStartup(reader); err != nil {
		return nil, err
	}
	checker, ok := reader.(statuspkg.ReadinessChecker)
	if !ok {
		return nil, errors.New("status reader must implement status readiness checks")
	}
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
	probes = append(probes, statusSchemaReadinessProbe(checker, defaultStatusReadinessTimeout))
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
