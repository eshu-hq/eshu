package app

import "context"

// ComposeLifecycles combines one or more lifecycle hooks into one ordered
// start/stop chain.
func ComposeLifecycles(lifecycles ...Lifecycle) Lifecycle {
	filtered := make([]Lifecycle, 0, len(lifecycles))
	for _, lifecycle := range lifecycles {
		if lifecycle == nil {
			continue
		}
		filtered = append(filtered, lifecycle)
	}

	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return lifecycleChain{parts: filtered}
	}
}

type lifecycleChain struct {
	parts []Lifecycle
}

func (c lifecycleChain) Start(ctx context.Context) error {
	started := make([]Lifecycle, 0, len(c.parts))
	for _, lifecycle := range c.parts {
		if err := lifecycle.Start(ctx); err != nil {
			for i := len(started) - 1; i >= 0; i-- {
				_ = started[i].Stop(context.Background())
			}
			return err
		}
		started = append(started, lifecycle)
	}

	return nil
}

func (c lifecycleChain) Stop(ctx context.Context) error {
	for i := len(c.parts) - 1; i >= 0; i-- {
		if err := c.parts[i].Stop(ctx); err != nil {
			return err
		}
	}

	return nil
}
