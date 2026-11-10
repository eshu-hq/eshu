// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns scheduling, policy, audit, and durable admission.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ComponentExtensionPlanner plans generic component extension workflow rows.
type ComponentExtensionPlanner interface {
	PlanComponentExtensionWork(context.Context, extension.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleComponentExtensionWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleComponentExtension(instance) {
			continue
		}
		config, configOK, configErr := activation.ParseConfig(instance.Configuration)
		if configErr == nil && configOK {
			decision := s.Config.ExtensionEgressPolicy.Decide(egress.ExtensionRequest{
				ComponentID:   config.ComponentID,
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
			})
			if decision.Action == egress.ExtensionActionDeny {
				if err := s.recordExtensionEgressAudit(ctx, observedAt, instance, config, decision); err != nil {
					return fmt.Errorf("record component extension egress audit for %q: %w", instance.InstanceID, err)
				}
				if s.Logger != nil {
					s.Logger.Info(
						"workflow coordinator skipped component extension scheduling by egress policy",
						"collector_kind", instance.CollectorKind,
						"component_id", config.ComponentID,
						"instance_id", instance.InstanceID,
						"reason", decision.Reason,
					)
				}
				continue
			}
		}
		if s.ComponentExtensionPlanner == nil {
			return fmt.Errorf("component extension planner is required for active component extension collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.ComponentExtensionPlanner.PlanComponentExtensionWork(
			ctx,
			extension.PlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    scheduledPlanKey(instance, observedAt, interval),
			},
		)
		if err != nil {
			return fmt.Errorf("plan component extension work for %q: %w", instance.InstanceID, err)
		}
		// A Go reachability activation's host scope names no repository, so a
		// claim for it would have nothing to scan. Its derived scopes are its
		// only work; a bad derive block fails in the hook below.
		derivedOnly := configErr == nil && configOK && activationPlansOnlyDerivedScopes(instance.CollectorKind, config)
		if len(items) > 0 && !derivedOnly {
			if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
				return fmt.Errorf("create component extension scheduled work for %q: %w", instance.InstanceID, err)
			}
		}
		// A kind-scoped hook plans the scopes the coordinator derives for
		// this activation (vulnerability_intelligence OSV scopes, Go
		// reachability repository scopes) as a
		// second run, after the egress decision above and through the same
		// durable admission. The open-target guard admits per scope, so an
		// open host-scope item does not block them (#9311).
		if configErr == nil && configOK {
			if err := s.scheduleActivationDerivedScopes(ctx, observedAt, instance, config, interval); err != nil {
				return err
			}
		}
	}
	return nil
}

func shouldScheduleComponentExtension(instance workflow.CollectorInstance) bool {
	if !instance.Enabled || !instance.ClaimsEnabled {
		return false
	}
	return hasComponentActivationConfiguration(instance)
}

// hasComponentActivationConfiguration reports whether the instance carries
// generic component-extension activation configuration, so the generic
// component planner owns it. A malformed or incomplete activation document
// counts: the generic planner reports that error, and no in-tree planner may
// claim the instance as well. Blank and in-tree target configuration return
// false. The generic scheduler, every in-tree kind scheduler (through
// inTreeSchedulable), and the startup claim validation in Config.Validate use
// this one check, so the two sides always partition instances without overlap
// or a gap.
func hasComponentActivationConfiguration(instance workflow.CollectorInstance) bool {
	return isComponentActivationConfiguration(instance.Configuration)
}

// isComponentActivationConfiguration is the single parse behind
// hasComponentActivationConfiguration, taking the raw configuration so the
// desired-instance startup path, which has no stored row yet, shares it.
func isComponentActivationConfiguration(configuration string) bool {
	return activation.IsConfiguration(configuration)
}

// activationDerivedScopeCounter records derived scopes planned for a component
// activation. telemetry.Instruments.ActivationDerivedScopesPlanned satisfies
// it; a nil value disables the counter.
type activationDerivedScopeCounter interface {
	Add(ctx context.Context, incr int64, options ...metric.AddOption)
}

// activationCarriesComponentConfig reports whether the registry readback keeps
// an activation's config block as activation.Config.ComponentConfig. Only a
// component whose coordinator plans host-derived scopes needs it: today every
// vulnerability_intelligence activation and the Go reachability scanner. Every
// other activation's stored configuration stays exactly what it was, including
// the other scanner_worker components, so their consumers see no change.
func activationCarriesComponentConfig(kind scope.CollectorKind, componentID string) bool {
	if kind == scope.CollectorVulnerabilityIntelligence {
		return true
	}
	return kind == scope.CollectorScannerWorker && strings.TrimSpace(componentID) == scannercomponent.IDGoReachability
}

// scheduleActivationDerivedScopes is the kind-scoped hook that
// scheduleComponentExtensionWork calls for each activation after the egress
// decision allowed it. It plans the scopes the coordinator derives for that
// kind, through the same readers, plan key, rotation, and durable admission
// (createWorkflowWorkIfNoOpenTargets) the in-tree scheduler uses. Kinds without
// derived scopes return at once and read nothing.
func (s Service) scheduleActivationDerivedScopes(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config activation.Config,
	interval time.Duration,
) error {
	switch instance.CollectorKind {
	case scope.CollectorVulnerabilityIntelligence:
		return s.scheduleVulnerabilityActivationDerivedScopes(ctx, observedAt, instance, config, interval)
	case scope.CollectorScannerWorker:
		return s.scheduleGoModuleDerivedScopes(ctx, observedAt, instance, config)
	default:
		return nil
	}
}

// activationHostMismatchError reports an activation that enables derived
// scopes while its host.sourceSystem or host.scope.kind is not the collector
// kind it derives for. The worker stamps every generation with
// host.scope.kind, so derived generations under any other kind or source
// would be mislabeled against that kind's history. All three values are
// collector labels, not secrets, and the message names them.
type activationHostMismatchError struct {
	SourceSystem string
	ScopeKind    string
	Want         scope.CollectorKind
}

func (e activationHostMismatchError) Error() string {
	return fmt.Sprintf("host.sourceSystem %q and host.scope.kind %q must both be %q",
		e.SourceSystem, e.ScopeKind, e.Want)
}

// checkActivationDocumentDerivation applies [checkActivationDerivation] to a
// desired instance's raw configuration. Config.Validate calls it for every
// instance, so an activation document that arrives through
// ESHU_COLLECTOR_INSTANCES_JSON fails startup the same way a registry
// activation does, and so does a disabled one, like an in-tree instance with a
// bad derive config. A configuration that is not an activation document, or a
// malformed one, returns nil here: in-tree validators own the first, and the
// generic planner reports the second at reconcile (#8919).
func checkActivationDocumentDerivation(kind scope.CollectorKind, configuration string) error {
	config, ok, err := activation.ParseConfig(configuration)
	if err != nil || !ok {
		return nil
	}
	if !activationCarriesComponentConfig(kind, config.ComponentID) {
		return nil
	}
	if _, err = checkGoModuleDerivation(kind, config); err != nil {
		return err
	}
	_, err = checkActivationDerivation(kind, config)
	return err
}

// checkActivationDerivation is the one rule set for an activation's derive
// block. Coordinator startup calls it twice: componentActivationRuntimeConfig
// for a registry activation and Config.Validate (through
// [checkActivationDocumentDerivation]) for every desired instance, including a
// static ESHU_COLLECTOR_INSTANCES_JSON one, so a refused block fails LoadConfig
// before any durable write. The reconcile hook calls it again as a backstop
// for a stale durable row. A kind without
// host-planned derived scopes returns a zero derivation and no error. For
// vulnerability_intelligence it returns the decoded derivation, or the error
// from [vulnerability.ActivationDerivationFromConfig] (an invalid value, or
// [workflow.DeriveListNotExplicitError] for a blank list), or an
// [activationHostMismatchError] when an enabled derivation's host — or the
// host of a derive-disabled activation carrying standing static targets
// (#9880) — is not vulnerability_intelligence. It reads nothing and logs nothing.
func checkActivationDerivation(kind scope.CollectorKind, config activation.Config) (vulnerability.ActivationDerivation, error) {
	if kind != scope.CollectorVulnerabilityIntelligence {
		return vulnerability.ActivationDerivation{}, nil
	}
	derivation, err := vulnerability.ActivationDerivationFromConfig(config.ComponentConfig)
	if err != nil {
		return vulnerability.ActivationDerivation{}, err
	}
	// Standing static targets mint on both derivation branches at
	// reconcile (#9880), stamped with the host's labels like derived
	// scopes. Validate them here, above the enabled split, so a
	// duplicate-static or undecodable block fails LoadConfig before any
	// durable write instead of passing startup and crash-failing every
	// reconcile tick in the planner.
	hostScopeID := ""
	if config.Host != nil {
		hostScopeID = config.Host.Scope.ID
	}
	standing, standingErr := vulnerability.ActivationHasStandingStaticTargets(string(config.ComponentConfig), hostScopeID)
	if standingErr != nil {
		return vulnerability.ActivationDerivation{}, standingErr
	}
	if !derivation.Enabled() && !standing {
		return vulnerability.ActivationDerivation{}, nil
	}
	if err := checkActivationHost(config, scope.CollectorVulnerabilityIntelligence); err != nil {
		return vulnerability.ActivationDerivation{}, err
	}
	if !derivation.Enabled() {
		return vulnerability.ActivationDerivation{}, nil
	}
	return derivation, nil
}

// checkActivationHost refuses an enabled derivation whose host does not label
// its generations with the collector kind it derives for.
func checkActivationHost(config activation.Config, want scope.CollectorKind) error {
	host := activationHostMismatchError{Want: want}
	if config.Host != nil {
		host.SourceSystem, host.ScopeKind = config.Host.SourceSystem, config.Host.Scope.Kind
	}
	if host.SourceSystem != string(want) || host.ScopeKind != string(want) {
		return host
	}
	return nil
}

// refuseActivationHostMismatch logs one host-mismatch refusal and returns it
// wrapped for the caller. Both the enabled-derivation check and the
// standing static gate refuse through it, so the operator signal and the
// error text stay one shape.
func (s Service) refuseActivationHostMismatch(instance workflow.CollectorInstance, hostErr activationHostMismatchError) error {
	if s.Logger != nil {
		s.Logger.Error(
			"workflow coordinator refused derived scopes for component activation",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"host_source_system", hostErr.SourceSystem,
			"host_scope_kind", hostErr.ScopeKind,
			"reason", "host_scope_kind_or_source_system_mismatch",
		)
	}
	return fmt.Errorf("plan vulnerability intelligence derived scopes for %q: %w", instance.InstanceID, hostErr)
}

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
	nvdCursor, nvdCursorSet, err := s.vulnerabilityNVDWindowCursor(ctx, derivation.DeriveNVDWindows, instance.InstanceID)
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
	planKey := derivedTargetPlanKey(instance, observedAt, interval, derivation.PlanKeyDerivation().PlanningMode)
	run, items, observation, err := vulnerability.PlanActivationDerivedWork(vulnerability.ActivationDerivedPlanRequest{
		Instance:                 instance,
		ObservedAt:               observedAt,
		PlanKey:                  planKey,
		SourceSystem:             sourceSystem,
		HostScopeID:              hostScopeID,
		Derivation:               derivation,
		ComponentConfig:          string(config.ComponentConfig),
		NVDWindowCursorUnixMicro: nvdCursor,
		NVDWindowCursorSet:       nvdCursorSet,
		GHSAWindowCursors:        ghsaCursors,
		OwnedPackageTargets:      ownedPage.Targets,
		OwnedPackageWindow:       ownedPage.Window,
		OSPackageTargets:         osTargets,
		SBOMComponentTargets:     sbomTargets,
		FindingCVEIDs:            findingCVEIDs,
		TargetLastPlanned:        findingsLastPlanned,
	})
	if err != nil {
		return fmt.Errorf("plan vulnerability intelligence derived scopes for %q: %w", instance.InstanceID, err)
	}
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

func (s Service) recordActivationDerivedScopes(ctx context.Context, instance workflow.CollectorInstance, items []workflow.WorkItem) {
	if s.ActivationDerivedScopes == nil {
		return
	}
	for ecosystem, count := range vulnerability.DerivedTargetEcosystemCounts(items) {
		s.ActivationDerivedScopes.Add(ctx, int64(count), metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionCollectorKind, string(instance.CollectorKind)),
			attribute.String(telemetry.MetricDimensionCollectorInstanceID, instance.InstanceID),
			attribute.String(telemetry.MetricDimensionEcosystem, ecosystem),
		))
	}
}
