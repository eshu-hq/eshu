// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator // Root owns scheduling, policy, audit, and durable admission.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) scheduleVulnerabilityActivationDerivedScopes(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config activation.Config,
	interval time.Duration,
) error {
	derivation, err := checkActivationDerivation(instance.CollectorKind, config)
	var listErr workflow.DeriveListNotExplicitError
	var hostErr activationHostMismatchError
	switch {
	case err == nil:
	case errors.As(err, &hostErr):
		return s.refuseActivationHostMismatch(instance, hostErr)
	default:
		if errors.As(err, &listErr) && s.Logger != nil {
			s.Logger.Error(
				"workflow coordinator refused derived scopes for component activation",
				"collector_kind", instance.CollectorKind,
				"collector_instance_id", instance.InstanceID,
				"derive_section", listErr.Section,
				"derive_list", listErr.List,
				"reason", "derive_lists_not_explicit",
			)
		}
		return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
	}
	// An NVD window anchor mints nothing while rolling windows are
	// disabled — neither a window nor a standing static (unlike the
	// in-tree planner's static claim). Name the drop instead of letting
	// the activation read as a silent success.
	if !derivation.DeriveNVDWindows.Enabled && s.Logger != nil {
		for _, anchorScope := range vulnerability.ActivationNVDWindowAnchorScopes(string(config.ComponentConfig)) {
			s.Logger.Warn(
				"workflow coordinator skipped NVD window anchor while rolling windows are disabled",
				"collector_kind", instance.CollectorKind,
				"collector_instance_id", instance.InstanceID,
				"scope_id", anchorScope,
				"reason", "nvd_anchor_rolling_disabled",
			)
		}
	}
	if !derivation.Enabled() {
		// A derive-disabled activation can still carry standing static
		// targets the planner mints beside the derived scopes (#9880):
		// plan when one would mint, read nothing and return otherwise.
		// The evidence readers below all gate on their own section, so a
		// disabled derivation still reads nothing on this path.
		hostScopeID := ""
		if config.Host != nil {
			hostScopeID = config.Host.Scope.ID
		}
		standing, standingErr := vulnerability.ActivationHasStandingStaticTargets(string(config.ComponentConfig), hostScopeID)
		if standingErr != nil {
			return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, standingErr)
		}
		if !standing {
			return nil
		}
		// Static scopes are stamped with the host's labels like derived
		// scopes, so the host rule of an enabled derivation applies here.
		if hostErr := checkActivationHost(config, scope.CollectorVulnerabilityIntelligence); hostErr != nil {
			var mismatch activationHostMismatchError
			if !errors.As(hostErr, &mismatch) {
				// checkActivationHost only returns activationHostMismatchError;
				// never log a zero-value mismatch if that ever changes.
				mismatch = activationHostMismatchError{Want: scope.CollectorVulnerabilityIntelligence}
				if config.Host != nil {
					mismatch.SourceSystem, mismatch.ScopeKind = config.Host.SourceSystem, config.Host.Scope.Kind
				}
			}
			return s.refuseActivationHostMismatch(instance, mismatch)
		}
	}
	// An enabled derivation (checkActivationDerivation) or the standing
	// static gate above refuses a host that is not
	// vulnerability_intelligence, so Host is set here.
	sourceSystem, hostScopeID := config.Host.SourceSystem, config.Host.Scope.ID
	ownedPage, err := s.vulnerabilityOwnedTargets(ctx, derivation.DeriveFromOwnedPackages, observedAt, interval, workflow.OwnedTargetSlotSelector{})
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
	}
	freshness := s.reportOwnedTargetFreshness(ctx, instance, derivation.DeriveFromOwnedPackages, ownedPage, interval)
	s.logOwnedPackageWindow(instance, derivation.DeriveFromOwnedPackages.Enabled, ownedPage, freshness)
	osTargets, sbomTargets, err := s.vulnerabilityInstalledEvidenceTargets(ctx, derivation.DeriveFromInstalledEvidence, observedAt, interval)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence installed evidence targets for %q: %w", instance.InstanceID, err)
	}
	// The rolling NVD window derivation reads the stored cursor through the
	// same probe as the in-tree path (eshu-hq/eshu#9877 Path A). A disabled
	// derivation reads nothing; an enabled one without a reader fails
	// loudly instead of silently planning every window from zero.
	nvdCursor, err := s.vulnerabilityNVDWindowCursor(ctx, derivation.DeriveNVDWindows, instance.InstanceID, observedAt)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence NVD window cursor for %q: %w", instance.InstanceID, err)
	}
	// The rolling GHSA window derivation reads the stored per-type cursors
	// through the same probe as the in-tree path (eshu-hq/eshu#10517). A
	// disabled derivation reads nothing; an enabled one without a reader
	// fails loudly instead of silently planning every window from zero.
	ghsaCursors, err := s.vulnerabilityGHSAWindowCursors(ctx, derivation.DeriveGHSAWindows, instance.InstanceID)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence GHSA window cursors for %q: %w", instance.InstanceID, err)
	}
	// Findings-derived EPSS reads the active finding CVE ids and the
	// findings publication progress only when the section is enabled: a
	// disabled derivation reads nothing and plans no first_epss targets.
	var findingCVEIDs []string
	var findingsLastPlanned map[string]time.Time
	if derivation.DeriveFromFindings.Enabled {
		findingCVEIDs, err = s.vulnerabilityFindingCVEIDs(ctx, derivation.DeriveFromFindings)
		if err != nil {
			return fmt.Errorf("load vulnerability intelligence finding CVE ids for %q: %w", instance.InstanceID, err)
		}
		findingsLastPlanned, err = s.vulnerabilityFindingsRefreshProgress(ctx, instance.InstanceID)
		if err != nil {
			return fmt.Errorf("load vulnerability intelligence findings refresh progress for %q: %w", instance.InstanceID, err)
		}
	}
	// The one-time NVD baseline replay plans from live state the loader maps
	// onto the request (eshu-hq/eshu#10746): the same inputs the in-tree
	// tick plans from, through the loader the two sites share. An
	// unreadable input holds the lane, never the tick.
	planKey := derivedTargetPlanKey(instance, observedAt, interval, derivation.PlanKeyDerivation().PlanningMode)
	baseline := s.loadNVDBaselinePlanInputs(ctx, string(config.ComponentConfig), instance.InstanceID, observedAt)
	run, items, observation, err := vulnerability.PlanActivationDerivedWork(vulnerability.ActivationDerivedPlanRequest{
		Instance:                 instance,
		ObservedAt:               observedAt,
		PlanKey:                  planKey,
		SourceSystem:             sourceSystem,
		HostScopeID:              hostScopeID,
		Derivation:               derivation,
		ComponentConfig:          string(config.ComponentConfig),
		NVDWindowCursorUnixMicro: nvdCursor.CursorUnixMicro,
		NVDWindowCursorSet:       nvdCursor.Set,
		GHSAWindowCursors:        ghsaCursors,
		OwnedPackageTargets:      ownedPage.Targets,
		OwnedPackageWindow:       ownedPage.Window,
		OSPackageTargets:         osTargets,
		SBOMComponentTargets:     sbomTargets,
		FindingCVEIDs:            findingCVEIDs,
		TargetLastPlanned:        findingsLastPlanned,
		NVDBaseline:              baseline,
	})
	if err != nil {
		return fmt.Errorf("plan vulnerability intelligence derived scopes for %q: %w", instance.InstanceID, err)
	}
	// The activation owns no planner, so the service records the plan
	// signals the in-tree planner records itself, and wakes the operator
	// when the held-year set moved.
	vulnerability.RecordNVDBaselinePlanSignals(ctx, s.NVDBaselineInstruments, vulnerability.IntelligencePlanRequest{NVDBaseline: baseline.Derivation}, observation)
	s.reportNVDBaselineHeldChanged(ctx, strings.TrimSpace(baseline.Derivation.RunID), observation)
	// The EPSS observation fires on the same gate as the read: an enabled
	// findings derivation logs and counts the plan even when every chunk
	// paced out, while a disabled derivation records nothing.
	if derivation.DeriveFromFindings.Enabled {
		s.reportEPSSDerived(ctx, instance, observation)
	}
	// The owned-package derivation signal fires on the same gate as the
	// window log above: a disabled derivation made no read and records
	// nothing (#9287 criterion 4). The activation records selected-only:
	// the activation planner returns no skip evidence, so the skipped
	// counter stays in-tree-only by contract while selected covers both
	// paths. An instance that plans exclusively through activations
	// would otherwise keep both counters dark.
	s.recordOwnedDerivedPage(ctx, instance, ownedPage, vulnerability.IntelligenceDerivationObservation{})
	if len(items) == 0 {
		return nil
	}
	s.recordActivationDerivedScopes(ctx, instance, items)
	admitted, err := s.createWorkflowWork(ctx, instance, run, items, nil)
	if err != nil {
		return fmt.Errorf("create vulnerability intelligence derived scoped work for %q: %w", instance.InstanceID, err)
	}
	// Record the inserted findings chunks as last-planned after admission,
	// never inside the shared admission transaction: a crash between the
	// two only refetches once, and the next due tick repairs it. Only an
	// inserted chunk paces (eshu-hq/eshu#10417): a chunk the open-target
	// guard or the insert dropped stays due, and an empty inserted set —
	// the all-denied path or a count-only backend — records nothing. The
	// recorded scopes pace the next plan to one fetch per publication
	// window (eshu-hq/eshu#9876 Decision 5).
	if derivation.DeriveFromFindings.Enabled {
		if err := s.recordVulnerabilityTargetRefreshPlanned(ctx, instance.InstanceID, filterInsertedScopeIDs(vulnerabilityFindingsPlannedScopeIDs(items), admitted), observedAt); err != nil {
			return fmt.Errorf("record vulnerability intelligence findings refresh progress for %q: %w", instance.InstanceID, err)
		}
	}
	if s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator planned derived scopes for component activation",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"plan_key", planKey,
			"run_id", run.RunID,
			"planned_work_items", len(items),
			"enqueued_work_items", admitted.inserted,
			"owned_package_candidates", len(ownedPage.Targets),
			"installed_evidence_candidates", len(osTargets)+len(sbomTargets),
		)
	}
	return nil
}
