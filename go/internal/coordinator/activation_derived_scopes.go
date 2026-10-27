// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns the kind-scoped derived-scope hook because it calls the shared owned-package and installed-evidence readers and durable admission.

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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

func (s Service) scheduleVulnerabilityActivationDerivedScopes(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config activation.Config,
	interval time.Duration,
) error {
	derivation, err := vulnerability.ActivationDerivationFromConfig(config.ComponentConfig)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
	}
	if !derivation.Enabled() {
		return nil
	}
	sourceSystem, scopeKind, hostScopeID := "", "", ""
	if config.Host != nil {
		sourceSystem, scopeKind, hostScopeID = config.Host.SourceSystem, config.Host.Scope.Kind, config.Host.Scope.ID
	}
	// The worker stamps every generation with host.scope.kind, so derived
	// generations under any other kind or source would be mislabeled against
	// the in-tree vulnerability_intelligence history.
	want := string(scope.CollectorVulnerabilityIntelligence)
	if sourceSystem != want || scopeKind != want {
		if s.Logger != nil {
			s.Logger.Error(
				"workflow coordinator refused derived scopes for component activation",
				"collector_kind", instance.CollectorKind,
				"collector_instance_id", instance.InstanceID,
				"host_source_system", sourceSystem,
				"host_scope_kind", scopeKind,
				"reason", "host_scope_kind_or_source_system_mismatch",
			)
		}
		return fmt.Errorf(
			"plan vulnerability intelligence derived scopes for %q: host.sourceSystem %q and host.scope.kind %q must both be %q",
			instance.InstanceID, sourceSystem, scopeKind, want,
		)
	}
	ownedTargets, err := s.vulnerabilityOwnedTargets(ctx, derivation.DeriveFromOwnedPackages, observedAt, interval)
	if err != nil {
		return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
	}
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
		OwnedPackageTargets:  ownedTargets,
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
			"owned_package_candidates", len(ownedTargets),
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
