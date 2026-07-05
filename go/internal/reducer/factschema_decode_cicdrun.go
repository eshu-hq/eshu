// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// decodeCICDRun decodes one ci.run envelope into the typed cicdrunv1.Run
// struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (provider,
// run_id) or is otherwise malformed. It is the single decode site for the
// ci.run kind on the reducer side: every extractor that consumes ci.run
// facts decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string run join key.
func decodeCICDRun(env facts.Envelope) (cicdrunv1.Run, error) {
	run, err := factschema.DecodeCICDRun(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Run{}, newFactDecodeError(factschema.FactKindCICDRun, err)
	}
	return run, nil
}

// decodeCICDArtifact decodes one ci.artifact envelope into the typed
// cicdrunv1.Artifact struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (provider, run_id) or is otherwise malformed. It is the single
// decode site for the ci.artifact kind on the reducer side.
func decodeCICDArtifact(env facts.Envelope) (cicdrunv1.Artifact, error) {
	artifact, err := factschema.DecodeCICDArtifact(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Artifact{}, newFactDecodeError(factschema.FactKindCICDArtifact, err)
	}
	return artifact, nil
}

// decodeCICDEnvironmentObservation decodes one ci.environment_observation
// envelope into the typed cicdrunv1.EnvironmentObservation struct through
// the contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (provider, run_id) or is otherwise
// malformed. It is the single decode site for this kind on the reducer
// side.
func decodeCICDEnvironmentObservation(env facts.Envelope) (cicdrunv1.EnvironmentObservation, error) {
	observation, err := factschema.DecodeCICDEnvironmentObservation(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.EnvironmentObservation{}, newFactDecodeError(factschema.FactKindCICDEnvironmentObservation, err)
	}
	return observation, nil
}

// decodeCICDTriggerEdge decodes one ci.trigger_edge envelope into the typed
// cicdrunv1.TriggerEdge struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (provider, run_id) or is otherwise malformed. It is the single
// decode site for this kind on the reducer side.
func decodeCICDTriggerEdge(env facts.Envelope) (cicdrunv1.TriggerEdge, error) {
	edge, err := factschema.DecodeCICDTriggerEdge(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.TriggerEdge{}, newFactDecodeError(factschema.FactKindCICDTriggerEdge, err)
	}
	return edge, nil
}

// decodeCICDStep decodes one ci.step envelope into the typed cicdrunv1.Step
// struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (provider,
// run_id) or is otherwise malformed. It is the single decode site for this
// kind on the reducer side.
func decodeCICDStep(env facts.Envelope) (cicdrunv1.Step, error) {
	step, err := factschema.DecodeCICDStep(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.Step{}, newFactDecodeError(factschema.FactKindCICDStep, err)
	}
	return step, nil
}

// decodeCICDWorkflowImageEvidence decodes one ci.workflow_image_evidence
// envelope into the typed cicdrunv1.WorkflowImageEvidence struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required repository_id field or is otherwise
// malformed. It is the single decode site for this kind on the reducer
// side.
func decodeCICDWorkflowImageEvidence(env facts.Envelope) (cicdrunv1.WorkflowImageEvidence, error) {
	evidence, err := factschema.DecodeCICDWorkflowImageEvidence(factschemaEnvelope(env))
	if err != nil {
		return cicdrunv1.WorkflowImageEvidence{}, newFactDecodeError(factschema.FactKindCICDWorkflowImageEvidence, err)
	}
	return evidence, nil
}
