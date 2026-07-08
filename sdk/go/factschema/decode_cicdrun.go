// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// DecodeCICDRun decodes env.Payload into the latest cicdrunv1.Run struct for
// the "ci.run" fact kind, dispatching on env.SchemaVersion major per Contract
// System v1 §3.2. Callers (reducer handlers) receive either the decoded
// struct or a classified *DecodeError; they must never substitute a
// zero-value struct on error.
func DecodeCICDRun(env Envelope) (cicdrunv1.Run, error) {
	return decodeLatestMajor[cicdrunv1.Run](FactKindCICDRun, env)
}

// EncodeCICDRun marshals a cicdrunv1.Run into the map[string]any payload
// shape an Envelope carries. It is the inverse of DecodeCICDRun for
// schema-version-1 payloads, used by collectors emitting this fact kind and
// by this module's own round-trip tests.
func EncodeCICDRun(run cicdrunv1.Run) (map[string]any, error) {
	return encodeDirectPayload(run)
}

// DecodeCICDArtifact decodes env.Payload into the latest cicdrunv1.Artifact
// struct for the "ci.artifact" fact kind. See DecodeCICDRun for the dispatch
// and error contract.
func DecodeCICDArtifact(env Envelope) (cicdrunv1.Artifact, error) {
	return decodeLatestMajor[cicdrunv1.Artifact](FactKindCICDArtifact, env)
}

// EncodeCICDArtifact marshals a cicdrunv1.Artifact into the map[string]any
// payload shape an Envelope carries. It is the inverse of
// DecodeCICDArtifact for schema-version-1 payloads.
func EncodeCICDArtifact(artifact cicdrunv1.Artifact) (map[string]any, error) {
	return encodeDirectPayload(artifact)
}

// DecodeCICDEnvironmentObservation decodes env.Payload into the latest
// cicdrunv1.EnvironmentObservation struct for the
// "ci.environment_observation" fact kind. See DecodeCICDRun for the dispatch
// and error contract.
func DecodeCICDEnvironmentObservation(env Envelope) (cicdrunv1.EnvironmentObservation, error) {
	return decodeLatestMajor[cicdrunv1.EnvironmentObservation](FactKindCICDEnvironmentObservation, env)
}

// EncodeCICDEnvironmentObservation marshals a
// cicdrunv1.EnvironmentObservation into the map[string]any payload shape an
// Envelope carries. It is the inverse of DecodeCICDEnvironmentObservation
// for schema-version-1 payloads.
func EncodeCICDEnvironmentObservation(observation cicdrunv1.EnvironmentObservation) (map[string]any, error) {
	return encodeDirectPayload(observation)
}

// DecodeCICDTriggerEdge decodes env.Payload into the latest
// cicdrunv1.TriggerEdge struct for the "ci.trigger_edge" fact kind. See
// DecodeCICDRun for the dispatch and error contract.
func DecodeCICDTriggerEdge(env Envelope) (cicdrunv1.TriggerEdge, error) {
	return decodeLatestMajor[cicdrunv1.TriggerEdge](FactKindCICDTriggerEdge, env)
}

// EncodeCICDTriggerEdge marshals a cicdrunv1.TriggerEdge into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeCICDTriggerEdge for schema-version-1 payloads.
func EncodeCICDTriggerEdge(edge cicdrunv1.TriggerEdge) (map[string]any, error) {
	return encodeDirectPayload(edge)
}

// DecodeCICDStep decodes env.Payload into the latest cicdrunv1.Step struct
// for the "ci.step" fact kind. See DecodeCICDRun for the dispatch and error
// contract.
func DecodeCICDStep(env Envelope) (cicdrunv1.Step, error) {
	return decodeLatestMajor[cicdrunv1.Step](FactKindCICDStep, env)
}

// EncodeCICDStep marshals a cicdrunv1.Step into the map[string]any payload
// shape an Envelope carries. It is the inverse of DecodeCICDStep for
// schema-version-1 payloads.
func EncodeCICDStep(step cicdrunv1.Step) (map[string]any, error) {
	return encodeDirectPayload(step)
}

// DecodeCICDWorkflowImageEvidence decodes env.Payload into the latest
// cicdrunv1.WorkflowImageEvidence struct for the "ci.workflow_image_evidence"
// fact kind. See DecodeCICDRun for the dispatch and error contract.
func DecodeCICDWorkflowImageEvidence(env Envelope) (cicdrunv1.WorkflowImageEvidence, error) {
	return decodeLatestMajor[cicdrunv1.WorkflowImageEvidence](FactKindCICDWorkflowImageEvidence, env)
}

// EncodeCICDWorkflowImageEvidence marshals a
// cicdrunv1.WorkflowImageEvidence into the map[string]any payload shape an
// Envelope carries. It is the inverse of DecodeCICDWorkflowImageEvidence for
// schema-version-1 payloads.
func EncodeCICDWorkflowImageEvidence(evidence cicdrunv1.WorkflowImageEvidence) (map[string]any, error) {
	return encodeDirectPayload(evidence)
}
