// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// stubAcceptanceObserver reports a canned shared-acceptance row count.
type stubAcceptanceObserver struct{ count int64 }

func (s stubAcceptanceObserver) AcceptanceRowCount(context.Context) (int64, error) {
	return s.count, nil
}

// stubAWSClaimObserver reports canned AWS claim counts by account.
type stubAWSClaimObserver struct{ counts map[string]int64 }

func (s stubAWSClaimObserver) AWSClaimConcurrency(context.Context) (map[string]int64, error) {
	return s.counts, nil
}

// stubEdgesObserver reports canned edge counts by source tool.
type stubEdgesObserver struct{ counts map[string]int64 }

func (s stubEdgesObserver) EdgesBySourceTool(context.Context) (map[string]int64, error) {
	return s.counts, nil
}

// stubFilesLangObserver reports canned file counts by language.
type stubFilesLangObserver struct{ counts map[string]int64 }

func (s stubFilesLangObserver) FilesByLanguage(context.Context) (map[string]int64, error) {
	return s.counts, nil
}

// stubFamilyDepthObserver reports canned family queue depths.
type stubFamilyDepthObserver struct {
	depths map[string]map[string]map[string]int64
}

func (s stubFamilyDepthObserver) WorkflowFamilyQueueDepths(context.Context) (map[string]map[string]map[string]int64, error) {
	return s.depths, nil
}

// stubOrphanObserver reports canned orphan node counts by label.
type stubOrphanObserver struct{ counts map[string]int64 }

func (s stubOrphanObserver) GraphOrphanNodeCounts(context.Context) (map[string]int64, error) {
	return s.counts, nil
}

// stubClaimInvariantObserver reports canned projector claim invariants.
type stubClaimInvariantObserver struct {
	leases  int64
	missing int64
}

func (s stubClaimInvariantObserver) ProjectorScopesWithMultipleLiveLeases(context.Context) (int64, error) {
	return s.leases, nil
}

func (s stubClaimInvariantObserver) ProjectorScopesMissingClaimFence(context.Context) (int64, error) {
	return s.missing, nil
}

// stubMarkedWriteObserver reports a canned marked-write age.
type stubMarkedWriteObserver struct{ age float64 }

func (s stubMarkedWriteObserver) ProjectorMarkedWriteOldestAge(context.Context) (float64, error) {
	return s.age, nil
}

// stubRawRowsObserver reports a canned raw-row count.
type stubRawRowsObserver struct{ count int64 }

func (s stubRawRowsObserver) ContentPolicyRawRows(context.Context) (int64, error) {
	return s.count, nil
}

// TestRegisterInstrumentsFailLoud proves every meter/inst registration
// helper propagates a failing meter as a "register ..." error, so startup
// cannot silently run blind.
func TestRegisterInstrumentsFailLoud(t *testing.T) {
	t.Parallel()

	calls := []struct {
		name string
		call func(metric.Meter) error
	}{
		{"activation_obligation", func(m metric.Meter) error { return registerActivationObligationInstruments(m, &Instruments{}) }},
		{"auth_identity", func(m metric.Meter) error { return registerAuthIdentityStoreUnavailable(m, &Instruments{}) }},
		{"changed_since", func(m metric.Meter) error { return registerChangedSinceLinkInstruments(m, &Instruments{}) }},
		{"cicd_webhook", func(m metric.Meter) error { return registerCICDRunWebhookInstruments(m, &Instruments{}) }},
		{"collector_terminal_failure", func(m metric.Meter) error { return registerCollectorTerminalFailureAcknowledgements(m, &Instruments{}) }},
		{"completion_wake", func(m metric.Meter) error { return registerCompletionWakeInstruments(m, &Instruments{}) }},
		{"component_extension", func(m metric.Meter) error { return registerComponentExtensionInstruments(m, &Instruments{}) }},
		{"config_artifact_candidates", func(m metric.Meter) error { return registerConfigArtifactCandidates(m, &Instruments{}) }},
		{"config_artifact_derive", func(m metric.Meter) error { return registerConfigArtifactDeriveInstruments(m, &Instruments{}) }},
		{"config_artifact_walker", func(m metric.Meter) error { return registerConfigArtifactWalkerInstruments(m, &Instruments{}) }},
		{"config_artifact_shadow", func(m metric.Meter) error { return registerConfigArtifactShadowCompareInstruments(m, &Instruments{}) }},
		{"config_artifact_read", func(m metric.Meter) error { return registerConfigArtifactReadInstruments(m, &Instruments{}) }},
		{"content_store_policy", func(m metric.Meter) error { return registerContentStorePolicyInstruments(m, &Instruments{}) }},
		{"deferred_backfill_block", func(m metric.Meter) error { return registerDeferredBackfillBlockInstruments(m, &Instruments{}) }},
		{"vulnerability_source_state", func(m metric.Meter) error { return registerVulnerabilitySourceStateInstruments(m, &Instruments{}) }},
		{"claim_commit_observer", func(m metric.Meter) error { return registerClaimCommitObserverInstruments(m, &Instruments{}) }},
		{"deferred_backfill_targeted", func(m metric.Meter) error { return registerDeferredBackfillTargeted(m, &Instruments{}) }},
		{"delta_baseline_fence", func(m metric.Meter) error { return registerProjectorDeltaBaselineFence(m, &Instruments{}) }},
		{"delta_touched_ratio", func(m metric.Meter) error { return registerCollectorDeltaTouchedRatio(m, &Instruments{}) }},
		{"delta_union", func(m metric.Meter) error { return registerProjectorDeltaUnion(m, &Instruments{}) }},
		{"entity_context_resolution", func(m metric.Meter) error { return registerEntityContextResolutionInstruments(m, &Instruments{}) }},
		{"fileless_guard", func(m metric.Meter) error { return registerCanonicalFilelessWipeGuard(m, &Instruments{}) }},
		{"generation_retention_phase", func(m metric.Meter) error { return registerGenerationRetentionPhaseInstruments(m, &Instruments{}) }},
		{"generation_retention_expired", func(m metric.Meter) error {
			return registerGenerationRetentionExpiredReportInstruments(m, &Instruments{})
		}},
		{"graph_id_anchor_census", func(m metric.Meter) error { return registerGraphIDAnchorCensusInstruments(m, &Instruments{}) }},
		{"infra_search_key", func(m metric.Meter) error { return registerInfraSearchKeyInstruments(m, &Instruments{}) }},
		{"language_query_metadata", func(m metric.Meter) error { return registerLanguageQueryMetadataInstruments(m, &Instruments{}) }},
		{"operator_retirement", func(m metric.Meter) error { return registerOperatorRepositoryRetirement(m, &Instruments{}) }},
		{"own_gate_wake", func(m metric.Meter) error { return registerOwnGateWakeInstruments(m, &Instruments{}) }},
		{"producer_activation", func(m metric.Meter) error { return registerProducerActivationInstruments(m, &Instruments{}) }},
		{"reconcile_parse_skip", func(m metric.Meter) error { return registerReconcileParseSkipDecisions(m, &Instruments{}) }},
		{"recovery_delta_active", func(m metric.Meter) error { return registerRecoveryDeltaActiveScopes(m, &Instruments{}) }},
		{"package_manifest_dependency_read", func(m metric.Meter) error { return registerPackageManifestDependencyReadInstruments(m, &Instruments{}) }},
		{"repo_dependency_replay", func(m metric.Meter) error { return registerRepoDependencyReplayCounters(m, &Instruments{}) }},
		{"repository_retirements", func(m metric.Meter) error { return registerCanonicalRepositoryRetirements(m, &Instruments{}) }},
		{"repository_selection", func(m metric.Meter) error { return registerRepositorySelection(m, &Instruments{}) }},
		{"runtime_declaration_query", func(m metric.Meter) error { return registerRuntimeDeclarationQuery(m, &Instruments{}) }},
		{"search_document_supersede", func(m metric.Meter) error { return registerSearchDocumentGenerationSuperseded(m, &Instruments{}) }},
		{"secret_lines_repair", func(m metric.Meter) error { return registerSecretLinesRepairInstruments(m, &Instruments{}) }},
		{"security_alert_query", func(m metric.Meter) error { return registerSecurityAlertQueryInstruments(m, &Instruments{}) }},
		{"security_alert_reconciliation", func(m metric.Meter) error { return registerSecurityAlertReconciliationImpactWait(m, &Instruments{}) }},
		{"service_catalog_readiness", func(m metric.Meter) error { return registerServiceCatalogRuntimeReadiness(m, &Instruments{}) }},
		{"service_catalog_retrigger", func(m metric.Meter) error { return registerServiceCatalogRuntimeRetrigger(m, &Instruments{}) }},
		{"shared_projection_cycle", func(m metric.Meter) error { return registerSharedProjectionCycle(m, &Instruments{}) }},
		{"status_summary_writer", func(m metric.Meter) error { return registerStatusSummaryWriterInstruments(m, &Instruments{}) }},
		{"status_summary_read", func(m metric.Meter) error { return registerStatusSummaryReadInstruments(m, &Instruments{}) }},
		{"metrics_gather_errors", func(m metric.Meter) error {
			_, err := registerMetricsGatherErrors(m)
			return err
		}},
		{"new_instruments", func(m metric.Meter) error { _, err := NewInstruments(m); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			failed := false
			for failOn := 1; failOn <= 12; failOn++ {
				err := tc.call(flakyTestMeter(failOn))
				if err == nil {
					continue
				}
				failed = true
				if !strings.Contains(err.Error(), "register") {
					t.Fatalf("%s error = %q, want it wrapped as a register failure", tc.name, err)
				}
			}
			if !failed {
				t.Fatalf("%s never failed over 12 failing meters, want at least one register error", tc.name)
			}
		})
	}
}

// stubQueueObserver reports canned queue depths and oldest ages.
type stubQueueObserver struct {
	depths map[string]map[string]int64
	ages   map[string]float64
}

func (s stubQueueObserver) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	return s.depths, nil
}

func (s stubQueueObserver) QueueOldestAge(context.Context) (map[string]float64, error) {
	return s.ages, nil
}

// stubWorkerObserver reports canned active worker counts.
type stubWorkerObserver struct{ counts map[string]int64 }

func (s stubWorkerObserver) ActiveWorkers(context.Context) (map[string]int64, error) {
	return s.counts, nil
}

// TestRegisterObservableGaugesFailLoud proves the composite observable
// registration propagates meter failures with the instrument named.
func TestRegisterObservableGaugesFailLoud(t *testing.T) {
	t.Parallel()

	queueObs := stubQueueObserver{
		depths: map[string]map[string]int64{"projector": {"pending": 2}},
		ages:   map[string]float64{"projector": 1.5},
	}
	workerObs := stubWorkerObserver{counts: map[string]int64{"projector": 3}}
	failed := false
	for failOn := 1; failOn <= 12; failOn++ {
		err := RegisterObservableGauges(&Instruments{}, flakyTestMeter(failOn), queueObs, workerObs)
		if err == nil {
			continue
		}
		failed = true
		if !strings.Contains(err.Error(), "register") {
			t.Fatalf("observable gauges error = %q, want it wrapped as a register failure", err)
		}
	}
	if !failed {
		t.Fatal("observable gauges never failed over 12 failing meters, want at least one register error")
	}
	if err := RegisterObservableGauges(nil, nil, nil, nil); err == nil {
		t.Fatal("nil observable gauges registration succeeded, want an error")
	}
}

// TestObserverGaugeGuardsFailLoud proves the observer-backed gauge
// registrations reject nil instruments/meters and propagate meter
// failures with the instrument named.
func TestObserverGaugeGuardsFailLoud(t *testing.T) {
	t.Parallel()

	meter, _ := gaugeTestMeter()
	calls := []struct {
		name string
		// skipNilGuards marks helpers without entry nil guards. Their only
		// production caller (RegisterObservableGauges) passes non-nil
		// instruments, meters, and type-asserted observers, so nil is
		// unreachable there; the sweep still pins their meter-failure
		// propagation. See the step-4 handoff note.
		skipNilGuards bool
		call          func(inst *Instruments, m metric.Meter) error
	}{
		{"acceptance", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterAcceptanceObservableGauges(inst, m, stubAcceptanceObserver{count: 1})
		}},
		{"aws_claim", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterAWSClaimConcurrencyGauge(inst, m, stubAWSClaimObserver{counts: map[string]int64{"123456789012": 2}})
		}},
		{"edges_by_source_tool", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterEdgesBySourceToolObservableGauge(inst, m, stubEdgesObserver{counts: map[string]int64{"terraform": 1}})
		}},
		{"files_by_language", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterFilesByLanguageObservableGauge(inst, m, stubFilesLangObserver{counts: map[string]int64{"go": 1}})
		}},
		{"family_queue_depth", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterWorkflowFamilyQueueDepthObservableGauge(inst, m, stubFamilyDepthObserver{})
		}},
		{"graph_orphan", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterGraphOrphanObservableGauge(inst, m, stubOrphanObserver{counts: map[string]int64{"Repository": 1}})
		}},
		{"claim_invariant", true, func(inst *Instruments, m metric.Meter) error {
			return registerProjectorClaimInvariantGauges(inst, m, stubClaimInvariantObserver{})
		}},
		{"marked_write", true, func(inst *Instruments, m metric.Meter) error {
			return registerProjectorMarkedWriteGauges(inst, m, stubMarkedWriteObserver{})
		}},
		{"raw_rows", false, func(inst *Instruments, m metric.Meter) error {
			return RegisterContentPolicyRawRowsObservableGauge(inst, m, stubRawRowsObserver{count: 1})
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !tc.skipNilGuards {
				if err := tc.call(nil, meter); err == nil {
					t.Fatalf("%s with nil instruments succeeded, want an error", tc.name)
				}
				if err := tc.call(&Instruments{}, nil); err == nil {
					t.Fatalf("%s with nil meter succeeded, want an error", tc.name)
				}
			}
			failed := false
			for failOn := 1; failOn <= 4; failOn++ {
				err := tc.call(&Instruments{}, flakyTestMeter(failOn))
				if err == nil {
					continue
				}
				failed = true
				if !strings.Contains(err.Error(), "register") {
					t.Fatalf("%s error = %q, want it wrapped as a register failure", tc.name, err)
				}
			}
			if !failed {
				t.Fatalf("%s never failed over 4 failing meters, want at least one register error", tc.name)
			}
		})
	}
}

// TestAcceptanceAndAWSClaimGaugesReport proves the two observer gauges
// without callback tests report their canned values on collection.
func TestAcceptanceAndAWSClaimGaugesReport(t *testing.T) {
	t.Parallel()

	meter, reader := gaugeTestMeter()
	if err := RegisterAcceptanceObservableGauges(&Instruments{}, meter, stubAcceptanceObserver{count: 9}); err != nil {
		t.Fatalf("acceptance register err = %v, want nil", err)
	}
	awsMeter, awsReader := gaugeTestMeter()
	if err := RegisterAWSClaimConcurrencyGauge(&Instruments{}, awsMeter,
		stubAWSClaimObserver{counts: map[string]int64{"123456789012": 4}}); err != nil {
		t.Fatalf("aws claim register err = %v, want nil", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("acceptance Collect err = %v, want nil", err)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_shared_acceptance_rows", map[string]string{}); got != 9 {
		t.Fatalf("shared acceptance rows = %d, want 9", got)
	}
	var awsRM metricdata.ResourceMetrics
	if err := awsReader.Collect(context.Background(), &awsRM); err != nil {
		t.Fatalf("aws Collect err = %v, want nil", err)
	}
	if got := observableInt64GaugeValue(t, awsRM, "eshu_dp_aws_claim_concurrency", map[string]string{"account": "123456789012"}); got != 4 {
		t.Fatalf("aws claim concurrency = %d, want 4", got)
	}
}
