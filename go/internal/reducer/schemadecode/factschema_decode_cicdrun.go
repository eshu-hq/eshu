// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// DecodeCICDRun decodes one ci.run envelope into the typed cicdrunv1.Run
// struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (provider,
// run_id) or is otherwise malformed. It is the single decode site for the
// ci.run kind on the reducer side: every extractor that consumes ci.run
// facts decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string run join key.
func DecodeCICDRun(env facts.Envelope) (cicdrunv1.Run, error) {
	run, err := factschema.DecodeCICDRun(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Run{}, factdecode.NewFactDecodeError(factschema.FactKindCICDRun, err)
	}
	return run, nil
}

// DecodeCICDArtifact decodes one ci.artifact envelope into the typed
// cicdrunv1.Artifact struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (provider, run_id) or is otherwise malformed. It is the single
// decode site for the ci.artifact kind on the reducer side.
func DecodeCICDArtifact(env facts.Envelope) (cicdrunv1.Artifact, error) {
	artifact, err := factschema.DecodeCICDArtifact(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Artifact{}, factdecode.NewFactDecodeError(factschema.FactKindCICDArtifact, err)
	}
	return artifact, nil
}

// DecodeCICDEnvironmentObservation decodes one ci.environment_observation
// envelope into the typed cicdrunv1.EnvironmentObservation struct through
// the contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (provider, run_id) or is otherwise
// malformed. It is the single decode site for this kind on the reducer
// side.
func DecodeCICDEnvironmentObservation(env facts.Envelope) (cicdrunv1.EnvironmentObservation, error) {
	observation, err := factschema.DecodeCICDEnvironmentObservation(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.EnvironmentObservation{}, factdecode.NewFactDecodeError(factschema.FactKindCICDEnvironmentObservation, err)
	}
	return observation, nil
}

// DecodeCICDDeploymentEvent decodes one ci.deployment_event envelope into
// the typed cicdrunv1.DeploymentEvent struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// a required field (provider, deployment_id, environment, sha) or is
// otherwise malformed. This is the kind's only decode site: it is called
// from buildCICDRunCorrelationDecisionsWithQuarantine
// (ci_cd_run_correlation_decode.go), and its decoded value is joined onto a
// run by attachDeploymentEventsToRuns (ci_cd_run_correlation_deploy_events.go)
// via sha rather than the provider/run_id key the other ci_cd_run wrappers in
// this file use, since a deployment carries no run_id.
func DecodeCICDDeploymentEvent(env facts.Envelope) (cicdrunv1.DeploymentEvent, error) {
	event, err := factschema.DecodeCICDDeploymentEvent(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.DeploymentEvent{}, factdecode.NewFactDecodeError(factschema.FactKindCICDDeploymentEvent, err)
	}
	return event, nil
}

// DecodeCICDTriggerEdge decodes one ci.trigger_edge envelope into the typed
// cicdrunv1.TriggerEdge struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (provider, run_id) or is otherwise malformed. It is the single
// decode site for this kind on the reducer side.
func DecodeCICDTriggerEdge(env facts.Envelope) (cicdrunv1.TriggerEdge, error) {
	edge, err := factschema.DecodeCICDTriggerEdge(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.TriggerEdge{}, factdecode.NewFactDecodeError(factschema.FactKindCICDTriggerEdge, err)
	}
	return edge, nil
}

// DecodeCICDStep decodes one ci.step envelope into the typed cicdrunv1.Step
// struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (provider,
// run_id) or is otherwise malformed. It is the single decode site for this
// kind on the reducer side.
func DecodeCICDStep(env facts.Envelope) (cicdrunv1.Step, error) {
	step, err := factschema.DecodeCICDStep(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Step{}, factdecode.NewFactDecodeError(factschema.FactKindCICDStep, err)
	}
	return step, nil
}

// DecodeCICDWorkflowImageEvidence decodes one ci.workflow_image_evidence
// envelope into the typed cicdrunv1.WorkflowImageEvidence struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required repository_id field or is otherwise
// malformed. It is the single decode site for this kind on the reducer
// side.
func DecodeCICDWorkflowImageEvidence(env facts.Envelope) (cicdrunv1.WorkflowImageEvidence, error) {
	evidence, err := factschema.DecodeCICDWorkflowImageEvidence(FactschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.WorkflowImageEvidence{}, factdecode.NewFactDecodeError(factschema.FactKindCICDWorkflowImageEvidence, err)
	}
	return evidence, nil
}
