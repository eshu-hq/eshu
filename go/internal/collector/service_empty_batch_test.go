package collector

import (
	"context"
	"testing"
	"time"
)

func TestServiceRunSkipsAfterBatchDrainedOnEmptyBatchByDefault(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hookCalls := 0
	service := Service{
		Source: &stubSource{
			empty: func() {
				cancel()
			},
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := hookCalls; got != 0 {
		t.Fatalf("AfterBatchDrained() calls = %d, want 0", got)
	}
}

func TestServiceRunCallsAfterBatchDrainedForConfiguredEmptyBatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hookCalls := 0
	service := Service{
		Source: &stubSource{
			empty: func() {
				cancel()
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
	}
}

func TestServiceRunCallsEmptyBatchDrainHookOnceWhileIdle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emptyPolls := 0
	hookCalls := 0
	service := Service{
		Source: &stubSource{
			empty: func() {
				emptyPolls++
				if emptyPolls == 2 {
					cancel()
				}
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
	}
}
