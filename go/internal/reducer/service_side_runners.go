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
	if s.SupplyChainImpactWinnersMaintainer != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.SupplyChainImpactWinnersMaintainer)
	}
	if s.CollectorEvidenceSummaryMaintainer != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.CollectorEvidenceSummaryMaintainer)
	}
	if s.CodeCallProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.CodeCallProjectionRunner)
	}
	if s.RepoDependencyProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.RepoDependencyProjectionRunner)
	}
	if s.CodeReachabilityProjectionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.CodeReachabilityProjectionRunner)
	}
	if s.GraphProjectionPhaseRepairer != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.GraphProjectionPhaseRepairer)
	}
	if s.GenerationRetentionRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.GenerationRetentionRunner)
	}
	if s.GraphOrphanSweepRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.GraphOrphanSweepRunner)
	}
	if s.CodeValueFlowStaleCleanupRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.CodeValueFlowStaleCleanupRunner)
	}
	if s.SearchVectorBuildRunner != nil {
		startServiceSideRunner(ctx, wg, recordErr, s.SearchVectorBuildRunner)
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
