// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// CICDRunSchemaID is the checked-in JSON Schema $id for the schema-version-1
// "ci.run" payload.
const CICDRunSchemaID = schemaBaseID + "cicdrun/v1/run.schema.json"

// CICDRunSchema returns the JSON Schema bytes for cicdrunv1.Run.
func CICDRunSchema() ([]byte, error) {
	return reflectSchema(CICDRunSchemaID, "Eshu ci.run Payload (schema version 1)", &cicdrunv1.Run{})
}

// CICDArtifactSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.artifact" payload.
const CICDArtifactSchemaID = schemaBaseID + "cicdrun/v1/artifact.schema.json"

// CICDArtifactSchema returns the JSON Schema bytes for cicdrunv1.Artifact.
func CICDArtifactSchema() ([]byte, error) {
	return reflectSchema(CICDArtifactSchemaID, "Eshu ci.artifact Payload (schema version 1)", &cicdrunv1.Artifact{})
}

// CICDEnvironmentObservationSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "ci.environment_observation" payload.
const CICDEnvironmentObservationSchemaID = schemaBaseID + "cicdrun/v1/environment_observation.schema.json"

// CICDEnvironmentObservationSchema returns the JSON Schema bytes for
// cicdrunv1.EnvironmentObservation.
func CICDEnvironmentObservationSchema() ([]byte, error) {
	return reflectSchema(CICDEnvironmentObservationSchemaID, "Eshu ci.environment_observation Payload (schema version 1)", &cicdrunv1.EnvironmentObservation{})
}

// CICDTriggerEdgeSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.trigger_edge" payload.
const CICDTriggerEdgeSchemaID = schemaBaseID + "cicdrun/v1/trigger_edge.schema.json"

// CICDTriggerEdgeSchema returns the JSON Schema bytes for
// cicdrunv1.TriggerEdge.
func CICDTriggerEdgeSchema() ([]byte, error) {
	return reflectSchema(CICDTriggerEdgeSchemaID, "Eshu ci.trigger_edge Payload (schema version 1)", &cicdrunv1.TriggerEdge{})
}

// CICDStepSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.step" payload.
const CICDStepSchemaID = schemaBaseID + "cicdrun/v1/step.schema.json"

// CICDStepSchema returns the JSON Schema bytes for cicdrunv1.Step.
func CICDStepSchema() ([]byte, error) {
	return reflectSchema(CICDStepSchemaID, "Eshu ci.step Payload (schema version 1)", &cicdrunv1.Step{})
}

// CICDWorkflowImageEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.workflow_image_evidence" payload.
const CICDWorkflowImageEvidenceSchemaID = schemaBaseID + "cicdrun/v1/workflow_image_evidence.schema.json"

// CICDWorkflowImageEvidenceSchema returns the JSON Schema bytes for
// cicdrunv1.WorkflowImageEvidence.
func CICDWorkflowImageEvidenceSchema() ([]byte, error) {
	return reflectSchema(CICDWorkflowImageEvidenceSchemaID, "Eshu ci.workflow_image_evidence Payload (schema version 1)", &cicdrunv1.WorkflowImageEvidence{})
}
