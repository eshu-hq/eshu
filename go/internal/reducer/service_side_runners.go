package reducer

import (
	"context"
	"sync"
)

type serviceSideRunner interface {
	Run(context.Context) error
}

func (s Service) startSideRunners(
	ctx context.Context,
	wg *sync.WaitGroup,
	recordErr func(error),
) {
	if s.SharedProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.SharedProjectionRunner)
	}
	if s.CodeCallProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.CodeCallProjectionRunner)
	}
	if s.RepoDependencyProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.RepoDependencyProjectionRunner)
	}
	if s.GraphProjectionPhaseRepairer != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.GraphProjectionPhaseRepairer)
	}
	if s.GenerationRetentionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.GenerationRetentionRunner)
	}
}

func startServiceSideRunner(
	ctx context.Context,
	wg *sync.WaitGroup,
	recordErr func(error),
	runner serviceSideRunner,
) {
	if runner == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runner.Run(ctx); err != nil {
			recordErr(err)
		}
	}()
}
