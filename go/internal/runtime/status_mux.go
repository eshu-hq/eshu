package runtime

import (
	"context"
	"fmt"
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

	adminMux, err := NewAdminMux(AdminMuxConfig{
		ServiceName:     serviceName,
		Ready:           statusReadinessCheck(reader),
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

func statusReadinessCheck(reader statuspkg.Reader) AdminCheck {
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), defaultStatusReadinessTimeout)
		defer cancel()

		_, err := reader.ReadStatusSnapshot(ctx, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("read status snapshot: %w", err)
		}
		return nil
	}
}
