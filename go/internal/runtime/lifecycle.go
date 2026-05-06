package runtime

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ContextRunner blocks until the parent process context is canceled.
type ContextRunner struct{}

// Run blocks until the process context is canceled.
func (ContextRunner) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Lifecycle is the minimal start-run-stop surface shared by the bootstrap
// lane.
type Lifecycle struct {
	ServiceName string
	Telemetry   telemetry.Bootstrap
}

// NewLifecycle builds a lifecycle wrapper for the supplied config.
func NewLifecycle(cfg Config) (Lifecycle, error) {
	if err := cfg.Validate(); err != nil {
		return Lifecycle{}, err
	}

	bootstrap, err := telemetry.NewBootstrap(cfg.ServiceName)
	if err != nil {
		return Lifecycle{}, err
	}

	return Lifecycle{
		ServiceName: cfg.ServiceName,
		Telemetry:   bootstrap,
	}, nil
}

// Start performs startup hooks for the service.
func (l Lifecycle) Start(context.Context) error {
	return l.Telemetry.Validate()
}

// Run blocks until the process context is canceled.
func (l Lifecycle) Run(ctx context.Context) error {
	return ContextRunner{}.Run(ctx)
}

// Stop performs shutdown hooks for the service.
func (l Lifecycle) Stop(context.Context) error {
	return nil
}
