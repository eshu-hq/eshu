// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// TestServiceStartsActivationObligationRunner is the Service.startSideRunners
// wiring case for the #7584 activation obligation consumer: a configured
// runner polls its store once the service starts and stops on cancel.
func TestServiceStartsActivationObligationRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &wiringActivationStore{claimed: make(chan struct{}, 1)}
	runner := &maintenance.ActivationObligationRunner{
		Store:      store,
		Maintainer: wiringActivationMaintainer{},
		Config: maintenance.ActivationObligationRunnerConfig{
			Owner: "wiring-test", PollInterval: time.Hour,
		},
	}
	service := Service{ActivationObligationRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})
	select {
	case <-store.claimed:
	case <-time.After(2 * time.Second):
		t.Fatal("activation obligation runner never claimed")
	}
	cancel()
	wg.Wait()
	if gotErr != nil {
		t.Fatalf("side runner error = %v, want nil", gotErr)
	}
}

type wiringActivationStore struct {
	once    sync.Once
	claimed chan struct{}
}

func (s *wiringActivationStore) ClaimActivation(context.Context, string, time.Duration) (*maintenance.ActivationObligation, error) {
	s.once.Do(func() { s.claimed <- struct{}{} })
	return nil, nil
}

func (s *wiringActivationStore) FinalizeActivation(context.Context, maintenance.ActivationObligation) (maintenance.ActivationFinalizeResult, error) {
	return maintenance.ActivationFinalizeResult{}, nil
}

func (s *wiringActivationStore) RetireActivationInapplicable(context.Context, maintenance.ActivationObligation) (maintenance.ActivationFinalizeResult, error) {
	return maintenance.ActivationFinalizeResult{}, nil
}

func (s *wiringActivationStore) CatchUpActivations(context.Context, string, int) (maintenance.ActivationCatchUpPage, error) {
	return maintenance.ActivationCatchUpPage{}, nil
}

func (s *wiringActivationStore) PruneActivations(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

func (s *wiringActivationStore) ActivationStats(context.Context) (maintenance.ActivationStats, error) {
	return maintenance.ActivationStats{}, nil
}

type wiringActivationMaintainer struct{}

func (wiringActivationMaintainer) MaintainActivation(context.Context, maintenance.ActivationObligation) error {
	return nil
}
