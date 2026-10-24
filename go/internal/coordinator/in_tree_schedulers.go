// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// instanceSelector is one scheduler's rule for picking the collector
// instances it plans or hands work to. The coordinator keeps every selector in
// this file's registries so one test can hold them all to the same ownership
// rule: an instance carrying component activation configuration belongs to the
// generic component-extension scheduler and to no in-tree kind scheduler.
type instanceSelector struct {
	// name is the declared identifier of the selector function. A test parses
	// the package source and fails when a shouldSchedule* function is declared
	// but absent from the registries, so a new selector cannot skip the
	// ownership test.
	name string
	// kind is the collector kind the selector serves. It is empty only for the
	// generic component-extension selector, which is not kind-scoped.
	kind scope.CollectorKind
	// selects reports whether the scheduler owns the instance.
	selects func(workflow.CollectorInstance) bool
}

// inTreeSchedulable is the one rule every in-tree kind scheduler's selector
// applies: the instance is of the scheduler's collector kind, is enabled and
// claims-enabled, and is not configured as a component activation document.
// An instance with component configuration belongs to the generic
// component-extension scheduler alone, whatever its collector kind, because an
// in-tree planner reads that configuration as its own target list and fails the
// whole reconcile on it (#8907, #8919). A new selector calls this instead of
// repeating the three checks, and registers itself in reconcileSchedulers so
// the ownership test covers it. The kind and gate checks come first so the
// configuration is parsed only for instances the scheduler could select.
func inTreeSchedulable(instance workflow.CollectorInstance, kind scope.CollectorKind) bool {
	if instance.CollectorKind != kind || !instance.Enabled || !instance.ClaimsEnabled {
		return false
	}
	return !hasComponentActivationConfiguration(instance)
}

// reconcileScheduler is one scheduler that runReconcile dispatches, in order,
// against the egress-filtered durable collector instances.
type reconcileScheduler struct {
	instanceSelector
	// schedule plans or hands off work for the instances this scheduler owns.
	// The first error stops the reconcile.
	schedule func(Service, context.Context, time.Time, []workflow.CollectorInstance) error
}

// reconcileSchedulers returns the schedulers runReconcile dispatches, in
// dispatch order. Add a new scheduler here, not as another block in
// runReconcile: the dispatch loop and the ownership test then share this one
// list.
func reconcileSchedulers() []reconcileScheduler {
	return []reconcileScheduler{
		{
			instanceSelector{"shouldScheduleTerraformState", scope.CollectorTerraformState, shouldScheduleTerraformState},
			Service.scheduleTerraformStateWork,
		},
		{
			instanceSelector{"shouldScheduleOCIRegistry", scope.CollectorOCIRegistry, shouldScheduleOCIRegistry},
			Service.scheduleOCIRegistryWork,
		},
		{
			instanceSelector{"shouldSchedulePackageRegistry", scope.CollectorPackageRegistry, shouldSchedulePackageRegistry},
			Service.schedulePackageRegistryWork,
		},
		{
			instanceSelector{
				"shouldScheduleVulnerabilityIntelligence",
				scope.CollectorVulnerabilityIntelligence,
				shouldScheduleVulnerabilityIntelligence,
			},
			Service.scheduleVulnerabilityIntelligenceWork,
		},
		{
			instanceSelector{"shouldScheduleSBOMAttestation", scope.CollectorSBOMAttestation, shouldScheduleSBOMAttestation},
			Service.scheduleSBOMAttestationWork,
		},
		{
			instanceSelector{"shouldScheduleScannerWorker", scope.CollectorScannerWorker, shouldScheduleScannerWorker},
			Service.scheduleScannerWorkerWork,
		},
		{
			instanceSelector{"shouldScheduleSecurityAlert", scope.CollectorSecurityAlert, shouldScheduleSecurityAlert},
			Service.scheduleSecurityAlertWork,
		},
		{
			instanceSelector{"shouldScheduleCICDRun", scope.CollectorCICDRun, shouldScheduleCICDRun},
			Service.scheduleCICDRunWork,
		},
		{
			instanceSelector{"shouldSchedulePagerDuty", scope.CollectorPagerDuty, shouldSchedulePagerDuty},
			Service.schedulePagerDutyWork,
		},
		{
			instanceSelector{"shouldScheduleJira", scope.CollectorJira, shouldScheduleJira},
			Service.scheduleJiraWork,
		},
		{
			instanceSelector{"shouldSchedulePrometheusMimir", scope.CollectorPrometheusMimir, shouldSchedulePrometheusMimir},
			Service.schedulePrometheusMimirWork,
		},
		{
			instanceSelector{"shouldScheduleTempo", scope.CollectorTempo, shouldScheduleTempo},
			Service.scheduleTempoWork,
		},
		{
			instanceSelector{"shouldScheduleGCP", scope.CollectorGCP, shouldScheduleGCP},
			Service.scheduleGCPWork,
		},
		{
			instanceSelector{"shouldScheduleGrafana", scope.CollectorGrafana, shouldScheduleGrafana},
			Service.scheduleGrafanaWork,
		},
		{
			instanceSelector{"shouldScheduleLoki", scope.CollectorLoki, shouldScheduleLoki},
			Service.scheduleLokiWork,
		},
		{
			instanceSelector{"shouldScheduleVaultLive", scope.CollectorVaultLive, shouldScheduleVaultLive},
			Service.scheduleVaultLiveWork,
		},
		{
			instanceSelector{"shouldScheduleAWS", scope.CollectorAWS, shouldScheduleAWS},
			Service.scheduleAWSScheduledWork,
		},
		{
			instanceSelector{"shouldScheduleComponentExtension", "", shouldScheduleComponentExtension},
			Service.scheduleComponentExtensionWork,
		},
		{
			instanceSelector{"shouldScheduleAWSFreshness", scope.CollectorAWS, shouldScheduleAWSFreshness},
			Service.scheduleAWSFreshnessWork,
		},
		{
			instanceSelector{"shouldScheduleGCPFreshness", scope.CollectorGCP, shouldScheduleGCPFreshness},
			Service.scheduleGCPFreshnessWork,
		},
	}
}

// incidentFreshnessSelectors returns the incident-freshness handoff selectors.
// The handoff runs on the maintenance ticker rather than inside runReconcile,
// so these are not dispatched from reconcileSchedulers, but they pick in-tree
// PagerDuty and Jira instances and follow the same ownership rule.
func incidentFreshnessSelectors() []instanceSelector {
	return []instanceSelector{
		{
			"shouldScheduleIncidentFreshness",
			scope.CollectorPagerDuty,
			func(instance workflow.CollectorInstance) bool {
				return shouldScheduleIncidentFreshness(instance, webhook.ProviderPagerDuty)
			},
		},
		{
			"shouldScheduleIncidentFreshness",
			scope.CollectorJira,
			func(instance workflow.CollectorInstance) bool {
				return shouldScheduleIncidentFreshness(instance, webhook.ProviderJira)
			},
		},
	}
}

// allInstanceSelectors returns every scheduler selector the coordinator runs:
// the reconcile-dispatched ones and the incident-freshness handoff selectors.
func allInstanceSelectors() []instanceSelector {
	schedulers := reconcileSchedulers()
	selectors := make([]instanceSelector, 0, len(schedulers)+2)
	for _, scheduler := range schedulers {
		selectors = append(selectors, scheduler.instanceSelector)
	}
	return append(selectors, incidentFreshnessSelectors()...)
}
