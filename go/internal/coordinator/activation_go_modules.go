// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns scheduling and durable admission; reachability owns the planning.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/reachability"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// GoModuleRepositoryReader reads one rotation page of repositories that hold a
// go.mod, with their observed commit and remote. It is a bounded read: one
// primary-key probe when the bucket's run exists, otherwise at most one page of
// candidates.
type GoModuleRepositoryReader interface {
	ListGoModuleRepositories(
		context.Context,
		scannercomponent.GoModuleRepositoryFilter,
	) (scannercomponent.GoModuleRepositoryPage, error)
}

// checkGoModuleDerivation is the rule set for a Go reachability activation's
// derive block, applied at startup and again at reconcile as a backstop for a
// stale durable row. Only the Go reachability component derives; any other
// activation returns a zero derivation and no error. It reads nothing and logs
// nothing.
func checkGoModuleDerivation(kind scope.CollectorKind, config activation.Config) (reachability.Derivation, error) {
	if kind != scope.CollectorScannerWorker || !activationCarriesComponentConfig(kind, config.ComponentID) {
		return reachability.Derivation{}, nil
	}
	derivation, err := reachability.DerivationFromConfig(config.ComponentConfig)
	if err != nil || !derivation.Enabled() {
		return reachability.Derivation{}, err
	}
	if err := checkActivationHost(config, scope.CollectorScannerWorker); err != nil {
		return reachability.Derivation{}, err
	}
	return derivation, nil
}

// activationPlansOnlyDerivedScopes reports whether the coordinator's derived
// scopes are an activation's only work, so its host scope gets no work item.
func activationPlansOnlyDerivedScopes(kind scope.CollectorKind, config activation.Config) bool {
	derivation, err := checkGoModuleDerivation(kind, config)
	return err == nil && derivation.Enabled()
}

// scheduleGoModuleDerivedScopes plans one rotation page of Go repositories for a
// Go reachability activation. The page is fixed for its plan bucket: once the
// bucket's run exists the reader reports it and a repeat pass stops, so the run's
// requested_scope_set is never rewritten under an open claim. A commit that
// lands mid-bucket is planned on that repository's next rotation visit.
func (s Service) scheduleGoModuleDerivedScopes(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config activation.Config,
	interval time.Duration,
) error {
	derivation, err := checkGoModuleDerivation(instance.CollectorKind, config)
	if err != nil {
		var hostErr activationHostMismatchError
		if errors.As(err, &hostErr) && s.Logger != nil {
			s.Logger.Error(
				"workflow coordinator refused derived scopes for component activation",
				"collector_kind", instance.CollectorKind,
				"collector_instance_id", instance.InstanceID,
				"host_source_system", hostErr.SourceSystem,
				"host_scope_kind", hostErr.ScopeKind,
				"reason", "host_scope_kind_or_source_system_mismatch",
			)
		}
		return fmt.Errorf("plan go reachability derived scopes for %q: %w", instance.InstanceID, err)
	}
	if !derivation.Enabled() {
		return nil
	}
	if s.GoModuleRepositoryReader == nil {
		return fmt.Errorf("go module repository reader is required for go reachability activation %q", instance.InstanceID)
	}
	// A bootstrap key is fixed, which would pin the fleet to its first page. Each
	// bucket still plans at most one page, so a bootstrap instance rotates too.
	bucketInstance := instance
	bucketInstance.Bootstrap = false
	planKey := "go-modules-" + scheduledPlanKey(bucketInstance, observedAt, interval)
	page, err := s.GoModuleRepositoryReader.ListGoModuleRepositories(ctx, scannercomponent.GoModuleRepositoryFilter{
		RunID:          reachability.RunID(instance, planKey),
		RotationBucket: schedule.DerivedTargetRotationBucket(observedAt, interval),
		PageSize:       scannercomponent.GoModulePageSize,
	})
	if err != nil {
		return fmt.Errorf("read go module repositories for %q: %w", instance.InstanceID, err)
	}
	if page.AlreadyPlanned {
		return nil
	}
	run, items, skipped, err := reachability.Plan(reachability.PlanRequest{
		Instance: instance, ObservedAt: observedAt, PlanKey: planKey, Repositories: page.Repositories,
	})
	if err != nil {
		return fmt.Errorf("plan go reachability derived scopes for %q: %w", instance.InstanceID, err)
	}
	enqueued := 0
	if len(items) > 0 {
		s.recordActivationDerivedScopes(ctx, instance, items)
		enqueued, err = s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items)
		if err != nil {
			return fmt.Errorf("create go reachability derived scoped work for %q: %w", instance.InstanceID, err)
		}
	}
	if s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator planned derived scopes for component activation",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"plan_key", planKey,
			"planned_work_items", len(items),
			"enqueued_work_items", enqueued,
			"candidates", page.Candidates,
			"total_candidates", page.TotalCandidates,
			"go_module_repositories", len(page.Repositories),
			"slot", page.Slot,
			"slot_count", page.SlotCount,
			"skipped_invalid_repository_id", skipped[reachability.SkipInvalidRepositoryID],
			"skipped_duplicate_repository", skipped[reachability.SkipDuplicateRepository],
			"skipped_unsupported_remote", skipped[reachability.SkipUnsupportedRemote],
			"skipped_missing_commit", skipped[reachability.SkipMissingCommit],
		)
	}
	return nil
}
