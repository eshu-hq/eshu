// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// minScanInterval is the floor for a per-instance scan interval. It matches the
// sub-second rejection the issue asks for; the effective floor is usually the
// global reconcile interval, enforced by validateScanInterval.
const minScanInterval = time.Second

// validateScanInterval rejects a configured scan_interval the coordinator cannot
// honor. The reconcile ticker fires at the global interval, so a per-instance
// bucket narrower than that would be visited less often than it promises and
// would mint a fresh plan key on most visits, and a bucket that is not an
// integer multiple of it would be planned at uneven spacing. Unset passes: the
// instance then uses the global interval.
func validateScanInterval(raw string, reconcileInterval time.Duration) error {
	interval, set, err := schedule.ParseScanInterval(raw)
	if err != nil {
		return err
	}
	if !set {
		return nil
	}
	if interval < minScanInterval {
		return fmt.Errorf("%s %s must be at least %s", schedule.ScanIntervalConfigKey, interval, minScanInterval)
	}
	if reconcileInterval <= 0 {
		reconcileInterval = schedule.DefaultReconcileInterval
	}
	if interval < reconcileInterval {
		return fmt.Errorf(
			"%s %s must not be shorter than the reconcile interval %s",
			schedule.ScanIntervalConfigKey, interval, reconcileInterval,
		)
	}
	// Ticks arrive every reconcile interval, so a bucket that is an integer
	// multiple of it holds exactly that many ticks and consecutive buckets
	// are planned exactly one interval apart. A 45s bucket over a 30s ticker
	// instead puts consecutive ticks in consecutive buckets every other
	// cycle, so scans would recur 30s apart forever.
	if interval%reconcileInterval != 0 {
		return fmt.Errorf(
			"%s %s must be an integer multiple of the reconcile interval %s",
			schedule.ScanIntervalConfigKey, interval, reconcileInterval,
		)
	}
	return nil
}

// scanInterval returns the scheduled-scan bucket width for one collector
// instance: its configured scan_interval when set, else the global reconcile
// interval. It returns the decode error rather than falling back, so a
// malformed value surfaces at the reconcile that reads it instead of quietly
// re-planning on the global cadence. Config.Validate rejects the same values
// at startup for every desired instance.
//
// Bootstrap instances always get the global interval. Their plan key is the
// fixed "bootstrap", so a wider bucket would not slow their planning; it
// would only hold one page of derived targets for the whole override while
// the bootstrap run is non-terminal. Ignoring the field here keeps the plan
// key and the rotation on the same clock for them too.
func (s Service) scanInterval(instance workflow.CollectorInstance) (time.Duration, error) {
	global := s.Config.ReconcileInterval
	if global <= 0 {
		global = schedule.DefaultReconcileInterval
	}
	if instance.Bootstrap {
		return global, nil
	}
	interval, set, err := schedule.ParseScanInterval(instance.Configuration)
	if err != nil {
		return 0, err
	}
	if !set {
		return global, nil
	}
	return interval, nil
}

// scheduledPlanKey is the shared plan key for every periodic scheduled
// planner. Bootstrap instances plan under one fixed key. Otherwise the key is
// the instance mode plus the wall clock truncated to the instance's scan
// interval, so a repeat reconcile inside one bucket reproduces the same run
// and work-item identifiers and the store's open-target guard treats it as
// the same plan. Truncation rounds from Go's zero time (time.Truncate), so a
// 12h bucket always turns over at 00:00 and 12:00 UTC regardless of when the
// coordinator started; derivedTargetRotationOffset indexes the same truncated
// clock so a rotating instance's page and plan key change together.
func scheduledPlanKey(instance workflow.CollectorInstance, observedAt time.Time, interval time.Duration) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	if interval <= 0 {
		interval = schedule.DefaultReconcileInterval
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	if prefix == "" {
		prefix = "schedule"
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}

func (s Service) createWorkflowWorkIfNoOpenTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	run workflow.Run,
	items []workflow.WorkItem,
) (int, error) {
	authorizedItems, denied, err := s.authorizeWorkflowWorkItems(ctx, run, items)
	if err != nil {
		return 0, err
	}
	if denied > 0 && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped workflow work by tenant grant",
			"collector_kind", instance.CollectorKind,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(items),
			"authorized_work_items", len(authorizedItems),
			"denied_work_items", denied,
			"reason", "tenant_scope_missing_or_stale_policy",
		)
	}
	if len(authorizedItems) == 0 {
		return 0, nil
	}
	if denied > 0 {
		run = filterWorkflowRunRequestedScopeSet(run, authorizedItems)
	}
	admission, err := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, authorizedItems)
	if err != nil {
		return 0, err
	}
	// The two shortfalls are different events and are logged as such. A target
	// the open-target guard dropped is already being collected by an open run,
	// which is the benign skip that makes a second coordinator safe. A row the
	// guard admitted and the store then refused is work nobody will collect, so
	// it is a warning, not a duplicate notice (#4586).
	if admission.EligibleTargets < len(authorizedItems) && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped duplicate workflow work",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(authorizedItems),
			"enqueued_work_items", admission.InsertedWorkItems,
			"skipped_work_items", len(authorizedItems)-admission.EligibleTargets,
			"reason", "target_already_planned",
		)
	}
	if admission.InsertedWorkItems < admission.EligibleTargets && s.Logger != nil {
		s.Logger.Warn(
			"workflow coordinator lost admitted workflow work at insert",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(authorizedItems),
			"admitted_work_items", admission.EligibleTargets,
			"enqueued_work_items", admission.InsertedWorkItems,
			"dropped_work_items", admission.EligibleTargets-admission.InsertedWorkItems,
			"reason", "insert_conflict_dropped_row",
		)
	}
	return admission.InsertedWorkItems, nil
}

// logScanIntervalOverrides records, once at startup before the first
// reconcile, every enabled, claim-enabled, non-bootstrap desired collector
// instance whose configuration sets scan_interval, with the global reconcile
// interval beside it, so an operator can see from the log which instances
// carry a bucket of their own and what it is (#6720). Disabled and
// claims-disabled instances (every shouldSchedule<Kind> refuses them) and
// bootstrap instances (scanInterval ignores the field for them) are skipped,
// because logging them would claim a cadence they do not have. Whether a
// logged instance's kind consumes the bucket is not evaluated here: a kind
// with no scheduled planner, an AWS instance with scheduled_scan_enabled
// false, or a single_pass derivation sets a bucket nothing reads. The
// plan-key suffix of the run IDs the instance produces is the ground truth.
// Config.Validate has already rejected malformed values by the time Run is
// called, so a decode error here is logged rather than fatal.
func (s Service) logScanIntervalOverrides() {
	if s.Logger == nil {
		return
	}
	for _, instance := range s.Config.CollectorInstances {
		if instance.Bootstrap || !instance.Enabled || !instance.ClaimsEnabled {
			continue
		}
		interval, set, err := schedule.ParseScanInterval(instance.Configuration)
		if err != nil {
			s.Logger.Warn(
				"workflow coordinator collector instance scan interval unreadable",
				"collector_instance_id", instance.InstanceID,
				"collector_kind", instance.CollectorKind,
				"error", err.Error(),
			)
			continue
		}
		if !set {
			continue
		}
		s.Logger.Info(
			"workflow coordinator collector instance sets scan interval",
			"collector_instance_id", instance.InstanceID,
			"collector_kind", instance.CollectorKind,
			"scan_interval", interval.String(),
			"reconcile_interval", s.Config.ReconcileInterval.String(),
		)
	}
}
