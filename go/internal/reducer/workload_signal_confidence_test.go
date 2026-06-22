package reducer

import "testing"

// TestWorkloadSignalConfidenceCoversEveryProvenanceKind pins that every
// provenance kind the candidate loader emits has a documented registry entry.
// A new signal added without an entry fails here, preventing a fresh magic
// number from reentering candidate_loader.go.
func TestWorkloadSignalConfidenceCoversEveryProvenanceKind(t *testing.T) {
	t.Parallel()

	kinds := []WorkloadSignalKind{
		SignalK8sResource,
		SignalArgoCDApplication,
		SignalHelmChart,
		SignalDockerfileRuntime,
		SignalDockerComposeRuntime,
		SignalCloudFormationTemplate,
		SignalGitHubActionsWorkflow,
		SignalJenkinsPipeline,
	}
	for _, kind := range kinds {
		entry, ok := DefaultWorkloadSignalConfidence.Lookup(kind)
		if !ok {
			t.Errorf("workload signal %q has no confidence registry entry", kind)
			continue
		}
		if entry.Rationale == "" {
			t.Errorf("workload signal %q has empty rationale", kind)
		}
	}
}

// TestWorkloadSignalConfidenceValuesBounded pins that every registered value is
// a valid probability.
func TestWorkloadSignalConfidenceValuesBounded(t *testing.T) {
	t.Parallel()

	for kind, entry := range DefaultWorkloadSignalConfidence.entries() {
		if entry.Confidence < 0 || entry.Confidence > 1 {
			t.Errorf("workload signal %q confidence = %.4f, want within [0,1]", kind, entry.Confidence)
		}
	}
}

// TestWorkloadSignalRuntimeSignalsOutrankCISignals pins the core ordering
// invariant of the issue for the workload-admission taxonomy: signals that
// describe a deployed runtime (k8s, argocd, helm, dockerfile, compose) must
// outrank CI/controller-only provenance (github-actions, jenkins), which only
// describe where automation runs. This is what keeps CI provenance from
// over-admitting a repository as a workload.
func TestWorkloadSignalRuntimeSignalsOutrankCISignals(t *testing.T) {
	t.Parallel()

	runtimeSignals := []WorkloadSignalKind{
		SignalK8sResource,
		SignalArgoCDApplication,
		SignalHelmChart,
		SignalDockerfileRuntime,
		SignalDockerComposeRuntime,
	}
	ciSignals := []WorkloadSignalKind{
		SignalGitHubActionsWorkflow,
		SignalJenkinsPipeline,
	}

	for _, runtime := range runtimeSignals {
		for _, ci := range ciSignals {
			rc := DefaultWorkloadSignalConfidence.ConfidenceFor(runtime)
			cc := DefaultWorkloadSignalConfidence.ConfidenceFor(ci)
			if rc <= cc {
				t.Errorf(
					"runtime signal %q (%.2f) must outrank CI signal %q (%.2f)",
					runtime, rc, ci, cc,
				)
			}
		}
	}
}

// TestWorkloadSignalTierFloorsMonotonic pins that the tier floors decrease
// strictly across the strength ordering.
func TestWorkloadSignalTierFloorsMonotonic(t *testing.T) {
	t.Parallel()

	ordered := []WorkloadSignalTier{
		WorkloadTierOrchestratedRuntime,
		WorkloadTierPackagedRuntime,
		WorkloadTierLocalRuntime,
		WorkloadTierTemplate,
		WorkloadTierCIProvenance,
	}
	for i := 0; i+1 < len(ordered); i++ {
		if ordered[i].Floor() <= ordered[i+1].Floor() {
			t.Errorf(
				"tier floors not monotonic: %s (%.2f) must exceed %s (%.2f)",
				ordered[i], ordered[i].Floor(), ordered[i+1], ordered[i+1].Floor(),
			)
		}
	}
}

// TestWorkloadSignalConfidenceForReturnsRegisteredValue pins the accessor used
// by the loader.
func TestWorkloadSignalConfidenceForReturnsRegisteredValue(t *testing.T) {
	t.Parallel()

	if got := DefaultWorkloadSignalConfidence.ConfidenceFor(SignalK8sResource); got != 0.98 {
		t.Fatalf("ConfidenceFor(k8s_resource) = %.4f, want 0.98", got)
	}
}

// TestWorkloadSignalConfidenceOverrideIsCalibratable pins the calibration hook.
func TestWorkloadSignalConfidenceOverrideIsCalibratable(t *testing.T) {
	t.Parallel()

	calibrated, err := DefaultWorkloadSignalConfidence.WithOverrides(map[WorkloadSignalKind]float64{
		SignalDockerComposeRuntime: 0.80,
	})
	if err != nil {
		t.Fatalf("WithOverrides returned error: %v", err)
	}
	if got := calibrated.ConfidenceFor(SignalDockerComposeRuntime); got != 0.80 {
		t.Fatalf("calibrated docker_compose = %.4f, want 0.80", got)
	}
	if got := DefaultWorkloadSignalConfidence.ConfidenceFor(SignalDockerComposeRuntime); got != 0.78 {
		t.Fatalf("default docker_compose mutated to %.4f, want 0.78", got)
	}
	if _, err := DefaultWorkloadSignalConfidence.WithOverrides(map[WorkloadSignalKind]float64{
		SignalDockerComposeRuntime: -0.1,
	}); err == nil {
		t.Fatal("WithOverrides accepted out-of-band confidence -0.1, want error")
	}
}
