package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Store is the narrow durable surface the dark coordinator needs in Slice 2.
type Store interface {
	ReconcileCollectorInstances(context.Context, time.Time, []workflow.DesiredCollectorInstance) error
	ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error)
	ReapExpiredClaims(context.Context, time.Time, int, time.Duration) ([]workflow.Claim, error)
	ReconcileWorkflowRuns(context.Context, time.Time) (int, error)
}

// Service is the dark-deployed workflow coordinator runner.
type Service struct {
	Config  Config
	Store   Store
	Metrics Metrics
	Logger  *slog.Logger
	Clock   func() time.Time
}

// Run periodically reconciles declarative collector instance state and, in
// active mode, advances workflow control-plane truth.
func (s Service) Run(ctx context.Context) error {
	if s.Store == nil {
		return fmt.Errorf("workflow coordinator store is required")
	}
	s.Config = s.Config.withDefaults()
	if err := s.Config.Validate(); err != nil {
		return err
	}

	if err := s.runReconcile(ctx); err != nil {
		return fmt.Errorf("initial collector reconciliation: %w", err)
	}
	if s.Config.DeploymentMode == deploymentModeActive {
		if err := s.runReapExpiredClaims(ctx); err != nil {
			return fmt.Errorf("initial expired-claim reap: %w", err)
		}
		if err := s.runWorkflowReconciliation(ctx); err != nil {
			return fmt.Errorf("initial workflow run reconciliation: %w", err)
		}
	}
	if s.Logger != nil {
		message := "workflow coordinator running in dark mode"
		if s.Config.DeploymentMode == deploymentModeActive {
			message = "workflow coordinator running in active mode"
		}
		s.Logger.Info(
			message,
			"deployment_mode", s.Config.DeploymentMode,
			"claims_enabled", s.Config.ClaimsEnabled,
			"collector_instances", len(s.Config.CollectorInstances),
			"reconcile_interval", s.Config.ReconcileInterval.String(),
			"reap_interval", s.Config.ReapInterval.String(),
		)
	}

	reconcileTicker := time.NewTicker(s.Config.ReconcileInterval)
	defer reconcileTicker.Stop()
	var reapTicker *time.Ticker
	if s.Config.DeploymentMode == deploymentModeActive {
		reapTicker = time.NewTicker(s.Config.ReapInterval)
		defer reapTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-reconcileTicker.C:
			if err := s.runReconcile(ctx); err != nil {
				return fmt.Errorf("reconcile collector instances: %w", err)
			}
			if s.Config.DeploymentMode == deploymentModeActive {
				if err := s.runWorkflowReconciliation(ctx); err != nil {
					return fmt.Errorf("reconcile workflow runs: %w", err)
				}
			}
		case <-tickerChan(reapTicker):
			if err := s.runReapExpiredClaims(ctx); err != nil {
				return fmt.Errorf("reap expired claims: %w", err)
			}
		}
	}
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s Service) runReconcile(ctx context.Context) error {
	startedAt := time.Now()
	observedAt := s.now().UTC()
	desiredCount := len(s.Config.CollectorInstances)

	if err := s.Store.ReconcileCollectorInstances(ctx, observedAt, s.Config.CollectorInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
		})
		return err
	}

	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeStateReadError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
		})
		return fmt.Errorf("list durable collector instances: %w", err)
	}

	durableCount := len(instances)
	drift := desiredCount - durableCount
	if drift < 0 {
		drift = -drift
	}
	s.recordReconcile(ctx, ReconcileObservation{
		Outcome:      reconcileOutcomeSuccess,
		Duration:     time.Since(startedAt),
		DesiredCount: desiredCount,
		DurableCount: durableCount,
	})
	if drift > 0 && s.Logger != nil {
		s.Logger.Warn(
			"workflow coordinator collector instance drift detected",
			"desired_collector_instances", desiredCount,
			"durable_collector_instances", durableCount,
			"collector_instance_drift", drift,
		)
	}
	return nil
}

func (s Service) recordReconcile(ctx context.Context, observation ReconcileObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReconcile(ctx, observation)
}

func (s Service) runReapExpiredClaims(ctx context.Context) error {
	startedAt := time.Now()
	claims, err := s.Store.ReapExpiredClaims(
		ctx,
		s.now().UTC(),
		s.Config.ExpiredClaimLimit,
		s.Config.ExpiredClaimRequeueDelay,
	)
	if err != nil {
		s.recordReap(ctx, ReapObservation{
			Outcome:  reaperOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	s.recordReap(ctx, ReapObservation{
		Outcome:    reaperOutcomeSuccess,
		Duration:   time.Since(startedAt),
		ReapedRows: len(claims),
	})
	return nil
}

func (s Service) runWorkflowReconciliation(ctx context.Context) error {
	startedAt := time.Now()
	reconciledRuns, err := s.Store.ReconcileWorkflowRuns(ctx, s.now().UTC())
	if err != nil {
		s.recordRunReconciliation(ctx, RunReconciliationObservation{
			Outcome:  runReconcileOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	s.recordRunReconciliation(ctx, RunReconciliationObservation{
		Outcome:        runReconcileOutcomeSuccess,
		Duration:       time.Since(startedAt),
		ReconciledRuns: reconciledRuns,
	})
	return nil
}

func (s Service) recordReap(ctx context.Context, observation ReapObservation) {
	metrics, ok := s.Metrics.(interface {
		RecordReap(context.Context, ReapObservation)
	})
	if !ok || metrics == nil {
		return
	}
	metrics.RecordReap(ctx, observation)
}

func (s Service) recordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	metrics, ok := s.Metrics.(interface {
		RecordRunReconciliation(context.Context, RunReconciliationObservation)
	})
	if !ok || metrics == nil {
		return
	}
	metrics.RecordRunReconciliation(ctx, observation)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
