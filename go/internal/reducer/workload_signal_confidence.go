// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
)

// WorkloadSignalKind identifies a provenance signal that ExtractWorkloadCandidates
// uses to score whether a repository defines a deployable workload. The string
// values are the provenance kinds recorded on WorkloadCandidate.Provenance.
type WorkloadSignalKind string

const (
	// SignalK8sResource is a parsed Kubernetes resource manifest: the strongest
	// evidence that a repository defines an orchestrated runtime workload.
	SignalK8sResource WorkloadSignalKind = "k8s_resource"
	// SignalArgoCDApplication is an Argo CD Application or ApplicationSet: an
	// explicit declaration that the repository is deployed to a cluster.
	SignalArgoCDApplication WorkloadSignalKind = "argocd_application"
	// SignalHelmChart is a Helm Chart.yaml: a packaged deployment unit.
	SignalHelmChart WorkloadSignalKind = "helm_chart"
	// SignalDockerfileRuntime is a Dockerfile with parsed build stages: a
	// runnable image definition.
	SignalDockerfileRuntime WorkloadSignalKind = "dockerfile_runtime"
	// SignalDockerComposeRuntime is a docker-compose service definition: a local
	// or single-host runtime topology.
	SignalDockerComposeRuntime WorkloadSignalKind = "docker_compose_runtime"
	// SignalCloudFormationTemplate is a CloudFormation template: infrastructure
	// definition that may or may not host the repository's own workload.
	SignalCloudFormationTemplate WorkloadSignalKind = "cloudformation_template"
	// SignalGitHubActionsWorkflow is a GitHub Actions workflow: CI provenance
	// that describes where automation runs, not what is deployed.
	SignalGitHubActionsWorkflow WorkloadSignalKind = "github_actions_workflow"
	// SignalJenkinsPipeline is a Jenkins pipeline: CI provenance, the weakest
	// workload signal.
	SignalJenkinsPipeline WorkloadSignalKind = "jenkins_pipeline"
)

// WorkloadSignalTier is a documented strength band for workload provenance
// signals. Tiers make the relative ordering of signal strength explicit and
// testable rather than implied by scattered float literals in the loader.
type WorkloadSignalTier string

const (
	// WorkloadTierOrchestratedRuntime is an explicit orchestrated deployment
	// declaration (Kubernetes resources, Argo CD applications).
	WorkloadTierOrchestratedRuntime WorkloadSignalTier = "ORCHESTRATED_RUNTIME"
	// WorkloadTierPackagedRuntime is a packaged deployment unit (Helm chart).
	WorkloadTierPackagedRuntime WorkloadSignalTier = "PACKAGED_RUNTIME"
	// WorkloadTierLocalRuntime is a runnable image or local topology
	// (Dockerfile, docker-compose).
	WorkloadTierLocalRuntime WorkloadSignalTier = "LOCAL_RUNTIME"
	// WorkloadTierTemplate is an infrastructure template whose link to the
	// repository's own workload is weaker (CloudFormation).
	WorkloadTierTemplate WorkloadSignalTier = "TEMPLATE"
	// WorkloadTierCIProvenance is CI/controller provenance that describes where
	// automation runs rather than what is deployed (GitHub Actions, Jenkins).
	// Values in this tier are deliberately low so a CI-only repository is not
	// admitted as a workload without stronger runtime evidence.
	WorkloadTierCIProvenance WorkloadSignalTier = "CI_PROVENANCE"
)

var workloadSignalTierFloors = map[WorkloadSignalTier]float64{
	WorkloadTierOrchestratedRuntime: 0.95,
	WorkloadTierPackagedRuntime:     0.90,
	WorkloadTierLocalRuntime:        0.78,
	WorkloadTierTemplate:            0.50,
	WorkloadTierCIProvenance:        0.0,
}

// Floor returns the inclusive minimum confidence for the tier.
func (t WorkloadSignalTier) Floor() float64 {
	return workloadSignalTierFloors[t]
}

// WorkloadSignalEntry is one registered workload-signal confidence with the
// provenance needed to audit or recalibrate it.
type WorkloadSignalEntry struct {
	// Confidence is the signal-strength prior recorded as the candidate
	// confidence when this signal is the strongest one present.
	Confidence float64
	// Tier is the documented strength band this value belongs to.
	Tier WorkloadSignalTier
	// Rationale explains why the signal earns this strength.
	Rationale string
}

// WorkloadSignalConfidenceRegistry is the single source of truth for workload
// provenance confidences. It replaces the float literals previously inlined in
// candidate_loader.go addProvenance calls. It is immutable after construction;
// recalibration builds a derived registry via WithOverrides.
type WorkloadSignalConfidenceRegistry struct {
	byKind map[WorkloadSignalKind]WorkloadSignalEntry
}

// DefaultWorkloadSignalConfidence holds the calibrated-by-hand workload signal
// priors. The values match the historical addProvenance literals exactly;
// centralizing them is the structural fix for issue #3490 on the reducer side.
var DefaultWorkloadSignalConfidence = newDefaultWorkloadSignalConfidence()

func newDefaultWorkloadSignalConfidence() *WorkloadSignalConfidenceRegistry {
	return &WorkloadSignalConfidenceRegistry{
		byKind: map[WorkloadSignalKind]WorkloadSignalEntry{
			SignalK8sResource: {
				Confidence: 0.98, Tier: WorkloadTierOrchestratedRuntime,
				Rationale: "a parsed Kubernetes resource is a direct orchestrated-runtime declaration",
			},
			SignalArgoCDApplication: {
				Confidence: 0.95, Tier: WorkloadTierOrchestratedRuntime,
				Rationale: "an Argo CD application declares the repository is deployed to a cluster",
			},
			SignalHelmChart: {
				Confidence: 0.92, Tier: WorkloadTierPackagedRuntime,
				Rationale: "a Helm Chart.yaml is a packaged, deployable runtime unit",
			},
			SignalDockerfileRuntime: {
				Confidence: 0.88, Tier: WorkloadTierLocalRuntime,
				Rationale: "a Dockerfile with build stages defines a runnable image",
			},
			SignalDockerComposeRuntime: {
				Confidence: 0.78, Tier: WorkloadTierLocalRuntime,
				Rationale: "a docker-compose service defines a local or single-host runtime topology",
			},
			SignalCloudFormationTemplate: {
				Confidence: 0.58, Tier: WorkloadTierTemplate,
				Rationale: "a CloudFormation template may provision infra without hosting the repository's own workload",
			},
			SignalGitHubActionsWorkflow: {
				Confidence: 0.45, Tier: WorkloadTierCIProvenance,
				Rationale: "a GitHub Actions workflow is CI provenance; it describes automation, not deployment",
			},
			SignalJenkinsPipeline: {
				Confidence: 0.42, Tier: WorkloadTierCIProvenance,
				Rationale: "a Jenkins pipeline is CI provenance and the weakest workload signal",
			},
		},
	}
}

// Lookup returns the registered entry for a kind and whether it exists.
func (r *WorkloadSignalConfidenceRegistry) Lookup(kind WorkloadSignalKind) (WorkloadSignalEntry, bool) {
	entry, ok := r.byKind[kind]
	return entry, ok
}

// ConfidenceFor returns the registered confidence for a kind, or 0 when the
// kind is unregistered. Returning 0 is deliberate: an unregistered signal must
// never silently inherit a passing confidence.
func (r *WorkloadSignalConfidenceRegistry) ConfidenceFor(kind WorkloadSignalKind) float64 {
	return r.byKind[kind].Confidence
}

// entries exposes the backing map for invariant tests in the same package.
func (r *WorkloadSignalConfidenceRegistry) entries() map[WorkloadSignalKind]WorkloadSignalEntry {
	return r.byKind
}

// WithOverrides returns a new registry with the supplied per-kind overrides
// applied. This is the calibration hook for the workload-admission taxonomy.
// Overrides are validated to be in [0,1]; out-of-band values are rejected. The
// shared default is never mutated.
func (r *WorkloadSignalConfidenceRegistry) WithOverrides(
	overrides map[WorkloadSignalKind]float64,
) (*WorkloadSignalConfidenceRegistry, error) {
	clone := &WorkloadSignalConfidenceRegistry{
		byKind: make(map[WorkloadSignalKind]WorkloadSignalEntry, len(r.byKind)),
	}
	for kind, entry := range r.byKind {
		clone.byKind[kind] = entry
	}

	for _, kind := range sortedWorkloadOverrideKeys(overrides) {
		value := overrides[kind]
		if value < 0 || value > 1 {
			return nil, fmt.Errorf("override confidence for %q = %.4f, must be within [0,1]", kind, value)
		}
		existing, ok := clone.byKind[kind]
		if !ok {
			return nil, fmt.Errorf("override for unregistered workload signal %q", kind)
		}
		existing.Confidence = value
		existing.Tier = workloadTierForConfidence(value)
		existing.Rationale = existing.Rationale + " (calibrated override)"
		clone.byKind[kind] = existing
	}

	return clone, nil
}

// workloadTierForConfidence classifies a value into the strongest tier whose
// floor it meets.
func workloadTierForConfidence(value float64) WorkloadSignalTier {
	ordered := []WorkloadSignalTier{
		WorkloadTierOrchestratedRuntime,
		WorkloadTierPackagedRuntime,
		WorkloadTierLocalRuntime,
		WorkloadTierTemplate,
		WorkloadTierCIProvenance,
	}
	for _, tier := range ordered {
		if value >= tier.Floor() {
			return tier
		}
	}
	return WorkloadTierCIProvenance
}

func sortedWorkloadOverrideKeys(overrides map[WorkloadSignalKind]float64) []WorkloadSignalKind {
	keys := make([]WorkloadSignalKind, 0, len(overrides))
	for kind := range overrides {
		keys = append(keys, kind)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
