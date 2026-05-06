package app

import (
	"strings"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// MountStatusServer composes the shared mounted runtime admin surface into an
// existing hosted application.
func MountStatusServer(app Application, reader statuspkg.Reader, opts ...runtimecfg.StatusAdminOption) (Application, error) {
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
