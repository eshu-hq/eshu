// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns scheduling, policy, audit, and durable admission.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

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
		if len(items) > 0 {
			if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
				return fmt.Errorf("create component extension scheduled work for %q: %w", instance.InstanceID, err)
			}
		}
		// A kind-scoped hook plans the scopes the coordinator derives for
		// this activation (today vulnerability_intelligence OSV scopes) as a
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
// core collector kind whose coordinator plans host-derived scopes needs it,
// today vulnerability_intelligence; every other kind's stored configuration
// stays exactly what it was, so their consumers see no change.
func activationCarriesComponentConfig(kind scope.CollectorKind) bool {
	return kind == scope.CollectorVulnerabilityIntelligence
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
	if instance.CollectorKind != scope.CollectorVulnerabilityIntelligence {
		return nil
	}
	return s.scheduleVulnerabilityActivationDerivedScopes(ctx, observedAt, instance, config, interval)
}

// activationHostMismatchError reports an activation that enables derived
// scopes while its host.sourceSystem or host.scope.kind is not
// vulnerability_intelligence. The worker stamps every generation with
// host.scope.kind, so derived generations under any other kind or source
// would be mislabeled against the in-tree vulnerability_intelligence history.
// Both values are collector labels, not secrets, and the message names them.
type activationHostMismatchError struct {
	SourceSystem string
	ScopeKind    string
}

func (e activationHostMismatchError) Error() string {
	return fmt.Sprintf("host.sourceSystem %q and host.scope.kind %q must both be %q",
		e.SourceSystem, e.ScopeKind, scope.CollectorVulnerabilityIntelligence)
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
	if !activationCarriesComponentConfig(kind) {
		return nil
	}
	config, ok, err := activation.ParseConfig(configuration)
	if err != nil || !ok {
		return nil
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
// [activationHostMismatchError] when an enabled derivation's host is not
// vulnerability_intelligence. It reads nothing and logs nothing.
func checkActivationDerivation(kind scope.CollectorKind, config activation.Config) (vulnerability.ActivationDerivation, error) {
	if !activationCarriesComponentConfig(kind) {
		return vulnerability.ActivationDerivation{}, nil
	}
	derivation, err := vulnerability.ActivationDerivationFromConfig(config.ComponentConfig)
	if err != nil || !derivation.Enabled() {
		return vulnerability.ActivationDerivation{}, err
	}
	var host activationHostMismatchError
	if config.Host != nil {
		host = activationHostMismatchError{SourceSystem: config.Host.SourceSystem, ScopeKind: config.Host.Scope.Kind}
	}
	want := string(scope.CollectorVulnerabilityIntelligence)
	if host.SourceSystem != want || host.ScopeKind != want {
		return vulnerability.ActivationDerivation{}, host
	}
	return derivation, nil
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
		return fmt.Errorf("plan vulnerability intelligence derived scopes for %q: %w", instance.InstanceID, err)
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
	if !derivation.Enabled() {
		return nil
	}
	// checkActivationDerivation refuses an enabled derivation without a
	// vulnerability_intelligence host, so Host is set here.
	sourceSystem, hostScopeID := config.Host.SourceSystem, config.Host.Scope.ID
	ownedPage, err := s.vulnerabilityOwnedTargets(ctx, derivation.DeriveFromOwnedPackages, observedAt, interval)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
	}
	s.logOwnedPackageWindow(instance, derivation.DeriveFromOwnedPackages.Enabled, ownedPage)
	osTargets, sbomTargets, err := s.vulnerabilityInstalledEvidenceTargets(ctx, derivation.DeriveFromInstalledEvidence, observedAt, interval)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence installed evidence targets for %q: %w", instance.InstanceID, err)
	}
	planKey := derivedTargetPlanKey(instance, observedAt, interval, derivation.PlanKeyDerivation().PlanningMode)
	run, items, err := vulnerability.PlanActivationDerivedWork(vulnerability.ActivationDerivedPlanRequest{
		Instance:             instance,
		ObservedAt:           observedAt,
		PlanKey:              planKey,
		SourceSystem:         sourceSystem,
		HostScopeID:          hostScopeID,
		Derivation:           derivation,
		OwnedPackageTargets:  ownedPage.Targets,
		OwnedPackageWindow:   ownedPage.Window,
		OSPackageTargets:     osTargets,
		SBOMComponentTargets: sbomTargets,
	})
	if err != nil {
		return fmt.Errorf("plan vulnerability intelligence derived scopes for %q: %w", instance.InstanceID, err)
	}
	if len(items) == 0 {
		return nil
	}
	s.recordActivationDerivedScopes(ctx, instance, items)
	enqueued, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items)
	if err != nil {
		return fmt.Errorf("create vulnerability intelligence derived scoped work for %q: %w", instance.InstanceID, err)
	}
	if s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator planned derived scopes for component activation",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"plan_key", planKey,
			"run_id", run.RunID,
			"planned_work_items", len(items),
			"enqueued_work_items", enqueued,
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
