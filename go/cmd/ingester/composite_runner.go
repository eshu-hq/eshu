package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/app"
)

// compositeRunner runs multiple Runner implementations concurrently.
// If any runner returns an error, it cancels all others and returns the first error.
type compositeRunner struct {
	runners []app.Runner
}

func newCompositeRunner(runners ...app.Runner) compositeRunner {
	return compositeRunner{runners: runners}
}

// Run starts all runners concurrently and returns the first error received.
func (c compositeRunner) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errc := make(chan error, len(c.runners))
	for _, r := range c.runners {
		go func(runner app.Runner) {
			errc <- runner.Run(ctx)
		}(r)
	}

	err := <-errc
	cancel()
	for i := 1; i < len(c.runners); i++ {
		<-errc
	}
	return err
}
