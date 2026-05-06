package runtime

import (
	"net/http"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// StatusAdminOption configures optional behavior on the status admin server.
type StatusAdminOption func(*statusAdminOptions)

type statusAdminOptions struct {
	recoveryHandler   *RecoveryHandler
	prometheusHandler http.Handler
}

// WithRecoveryHandler attaches a recovery handler to the admin mux, mounting
// /admin/replay and /admin/refinalize routes alongside the standard probes.
func WithRecoveryHandler(rh *RecoveryHandler) StatusAdminOption {
	return func(o *statusAdminOptions) {
		o.recoveryHandler = rh
	}
}

// WithPrometheusHandler attaches an OTEL Prometheus exporter handler that is
// served alongside the existing status-based metrics on /metrics.
func WithPrometheusHandler(h http.Handler) StatusAdminOption {
	return func(o *statusAdminOptions) {
		o.prometheusHandler = h
	}
}

// NewStatusAdminServer builds the shared admin HTTP server for a long-running
// runtime using the storage-backed status reader seam.
func NewStatusAdminServer(cfg Config, reader statuspkg.Reader, opts ...StatusAdminOption) (*HTTPServer, error) {
	adminMux, err := NewStatusAdminMux(cfg.ServiceName, reader, nil, opts...)
	if err != nil {
		return nil, err
	}

	return NewHTTPServer(HTTPServerConfig{
		Addr:    cfg.ListenAddr,
		Handler: adminMux,
	})
}

// NewStatusMetricsServer builds the shared dedicated metrics HTTP server for a
// long-running runtime when a separate metrics address is configured.
func NewStatusMetricsServer(cfg Config, reader statuspkg.Reader, opts ...StatusAdminOption) (*HTTPServer, error) {
	metricsAddr := strings.TrimSpace(cfg.MetricsAddr)
	if metricsAddr == "" {
		return nil, nil
	}

	metricsHandler, err := newStatusMetricsServerHandler(cfg.ServiceName, reader, opts...)
	if err != nil {
		return nil, err
	}

	return NewHTTPServer(HTTPServerConfig{
		Addr:    metricsAddr,
		Handler: metricsHandler,
	})
}

func newStatusMetricsServerHandler(
	serviceName string,
	reader statuspkg.Reader,
	opts ...StatusAdminOption,
) (http.Handler, error) {
	var options statusAdminOptions
	for _, opt := range opts {
		opt(&options)
	}

	metricsHandler, err := NewStatusMetricsHandler(serviceName, reader)
	if err != nil {
		return nil, err
	}

	return NewCompositeMetricsHandler(metricsHandler, options.prometheusHandler), nil
}
