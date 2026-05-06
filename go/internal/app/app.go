package app

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/runtime"
)

// Lifecycle captures the shared start-stop hooks for a hosted service.
type Lifecycle interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Runner captures the long-running behavior hosted by the application shell.
type Runner interface {
	Run(context.Context) error
}

// Application wires the shared runtime config and lifecycle for a command.
type Application struct {
	Config        runtime.Config
	Observability runtime.Observability
	Lifecycle     Lifecycle
	Runner        Runner
}

// New constructs a bootable application for the named service.
func New(serviceName string) (Application, error) {
	return NewHosted(serviceName, runtime.ContextRunner{})
}

// NewHosted constructs a bootable application for the named service runner.
func NewHosted(serviceName string, runner Runner) (Application, error) {
	cfg, err := runtime.LoadConfig(serviceName)
	if err != nil {
		return Application{}, err
	}

	lifecycle, err := runtime.NewLifecycle(cfg)
	if err != nil {
		return Application{}, err
	}

	return Application{
		Config:        cfg,
		Observability: runtime.NewObservability(),
		Lifecycle:     lifecycle,
		Runner:        runner,
	}, nil
}

// Run starts the service and waits for shutdown.
func (a Application) Run(ctx context.Context) error {
	if a.Lifecycle == nil {
		return errors.New("lifecycle is required")
	}
	if a.Runner == nil {
		return errors.New("runner is required")
	}

	if err := a.Lifecycle.Start(ctx); err != nil {
		return err
	}
	defer func() {
		_ = a.Lifecycle.Stop(context.Background())
	}()

	return a.Runner.Run(ctx)
}
