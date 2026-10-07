// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package app

import (
	"fmt"
	"strings"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// MountStatusServer composes the shared mounted runtime admin surface into an
// existing hosted application.
//
// A reader that implements StartupError() error and reports a configuration
// problem fails the mount, so a misconfigured process fails at startup instead
// of serving status routes that all error (#7009).
func MountStatusServer(app Application, reader statuspkg.Reader, opts ...runtimecfg.StatusAdminOption) (Application, error) {
	if err := statuspkg.ReaderStartupError(reader); err != nil {
		return Application{}, fmt.Errorf("status reader configuration: %w", err)
	}
	adminServer, err := runtimecfg.NewStatusAdminServer(app.Config, reader, opts...)
	if err != nil {
		return Application{}, err
	}

	lifecycle := ComposeLifecycles(app.Lifecycle, adminServer)
	if metricsAddr := strings.TrimSpace(app.Config.MetricsAddr); metricsAddr != "" && metricsAddr != strings.TrimSpace(app.Config.ListenAddr) {
		metricsServer, err := runtimecfg.NewStatusMetricsServer(app.Config, reader, opts...)
		if err != nil {
			return Application{}, err
		}
		lifecycle = ComposeLifecycles(lifecycle, metricsServer)
	}

	app.Lifecycle = lifecycle
	return app, nil
}

// NewHostedWithStatusServer builds one hosted application with the shared
// mounted runtime admin surface already attached.
func NewHostedWithStatusServer(serviceName string, runner Runner, reader statuspkg.Reader, opts ...runtimecfg.StatusAdminOption) (Application, error) {
	app, err := NewHosted(serviceName, runner)
	if err != nil {
		return Application{}, err
	}

	return MountStatusServer(app, reader, opts...)
}
