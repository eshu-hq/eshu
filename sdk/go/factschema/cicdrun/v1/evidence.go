// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// EnvironmentObservation is the schema-version-1 typed payload for the
// "ci.environment_observation" fact kind: one environment or deployment-gate
// observation reported by a provider run/job
// (go/internal/collector/cicdrun.environmentEnvelope).
//
// Provider and RunID are required for the same run-join-key reason as
// Run/Artifact: environmentEnvelope always sets both via sharedPayload, and
// the reducer joins every environment fact back to its run through
// cicdRunKey (go/internal/reducer/ci_cd_run_correlation.go:189-193).
type EnvironmentObservation struct {
	// Provider identifies the CI/CD provider that reported this observation.
	// Required — half of the reducer's run join key.
	Provider string `json:"provider"`

	// RunID is the owning run's provider identifier. Required — half of the
	// reducer's run join key.
	RunID string `json:"run_id"`

	// RunAttempt is the owning run's attempt number as a string. Optional:
	// see Run.RunAttempt for the same default-to-"1" contract.
	RunAttempt *string `json:"run_attempt,omitempty"`

	// JobID is the owning job's provider identifier. Optional.
	JobID *string `json:"job_id,omitempty"`

	// Environment is the observed environment/deployment-gate name (for
	// example "prod"). Optional: the collector only emits this fact when the
	// job carries a non-blank environment
	// (go/internal/collector/cicdrun/github_actions_fixture.go:91-97), but a
	// present-but-empty value on replay is still a valid observed value, not
	// malformed input.
	Environment *string `json:"environment,omitempty"`

	// DeploymentStatus is the provider's deployment status for this
	// environment. Optional.
	DeploymentStatus *string `json:"deployment_status,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// TriggerEdge is the schema-version-1 typed payload for the "ci.trigger_edge"
// fact kind: one explicit run trigger or upstream run edge reported by the
// provider (go/internal/collector/cicdrun.triggerEnvelope).
//
// Provider and RunID are required for the same run-join-key reason as the
// other kinds in this file: triggerEnvelope always sets both via
// sharedPayload, and the reducer joins every trigger fact back to its run
// through cicdRunKey (go/internal/reducer/ci_cd_run_correlation.go:194-198).
type TriggerEdge struct {
	// Provider identifies the CI/CD provider that reported this trigger
	// edge. Required — half of the reducer's run join key.
	Provider string `json:"provider"`

	// RunID is the owning (triggered) run's provider identifier. Required —
	// half of the reducer's run join key.
	RunID string `json:"run_id"`

	// RunAttempt is the owning run's attempt number as a string. Optional:
	// see Run.RunAttempt for the same default-to-"1" contract.
	RunAttempt *string `json:"run_attempt,omitempty"`

	// TriggerKind classifies the trigger edge (for example
	// "workflow_call"). Optional: the collector only emits a trigger fact
	// when TriggerKind, SourceProvider, and SourceRunID are all present
	// (go/internal/collector/cicdrun/github_actions_fixture.go:130-139), but
	// the field stays optional here because it is not a reducer join key —
	// only Provider/RunID/RunAttempt are.
	TriggerKind *string `json:"trigger_kind,omitempty"`

	// SourceProvider identifies the upstream run's CI/CD provider. Optional.
	SourceProvider *string `json:"source_provider,omitempty"`

	// SourceRunID is the upstream run's provider identifier. Optional.
	SourceRunID *string `json:"source_run_id,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// Step is the schema-version-1 typed payload for the "ci.step" fact kind: one
// step under a provider job (go/internal/collector/cicdrun.stepEnvelope).
//
// Provider and RunID are required for the same run-join-key reason as the
// other kinds in this file: stepEnvelope always sets both via sharedPayload,
// and the reducer's classifyCICDRunEvidence joins a step fact back to its run
// through cicdRunKey specifically to read DeploymentHintSource
// (go/internal/reducer/ci_cd_run_correlation.go:199-203) — the ONLY step
// field the reducer's typed decode path consumes today (a shell-only
// deployment hint suppresses a run's correlation to CICDRunCorrelationRejected
// rather than trusting shell text as deployment truth). Every other step
// field (name/status/timestamps/action reference) mirrors the collector
// emitter but is not read by any reducer handler; it is modeled anyway for
// contract completeness, matching how other families type an emitted field
// the reducer does not currently consume.
type Step struct {
	// Provider identifies the CI/CD provider that reported this step.
	// Required — half of the reducer's run join key.
	Provider string `json:"provider"`

	// RunID is the owning run's provider identifier. Required — half of the
	// reducer's run join key.
	RunID string `json:"run_id"`

	// RunAttempt is the owning run's attempt number as a string. Optional:
	// see Run.RunAttempt for the same default-to-"1" contract.
	RunAttempt *string `json:"run_attempt,omitempty"`

	// JobID is the owning job's provider identifier. Optional.
	JobID *string `json:"job_id,omitempty"`

	// StepNumber is the provider's step-number as a string. Optional.
	StepNumber *string `json:"step_number,omitempty"`

	// StepName is the step's declared name. Optional.
	StepName *string `json:"step_name,omitempty"`

	// Status is the provider's step status. Optional.
	Status *string `json:"status,omitempty"`

	// Result is the provider's step conclusion. Optional.
	Result *string `json:"result,omitempty"`

	// StartedAt is the step's start timestamp as an RFC3339 string.
	// Optional.
	StartedAt *string `json:"started_at,omitempty"`

	// CompletedAt is the step's completion timestamp as an RFC3339 string.
	// Optional.
	CompletedAt *string `json:"completed_at,omitempty"`

	// ActionRef is the reusable-action reference the collector parsed from
	// the step's display name (for example "actions/checkout@v4"). Optional.
	ActionRef *string `json:"action_ref,omitempty"`

	// DeploymentHintSource classifies where a deployment-adjacent signal for
	// this step came from (for example "shell" when the collector inferred
	// intent from shell command text rather than a structured provider
	// field). Optional: the reducer treats a step whose
	// DeploymentHintSource=="shell" as unsafe, shell-text-only evidence
	// (classifyCICDRunEvidence rejects the run's correlation rather than
	// trusting it), so an absent value is the common, non-hinting case, not
	// malformed input.
	DeploymentHintSource *string `json:"deployment_hint_source,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
