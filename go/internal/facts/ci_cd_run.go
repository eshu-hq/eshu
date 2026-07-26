// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// CICDPipelineDefinitionFactKind identifies one provider workflow or
	// pipeline definition observed before a run starts.
	CICDPipelineDefinitionFactKind = "ci.pipeline_definition"
	// CICDWorkflowImageEvidenceFactKind identifies a static workflow command
	// that explicitly names or ambiguously references a container image.
	CICDWorkflowImageEvidenceFactKind = "ci.workflow_image_evidence"
	// CICDRunFactKind identifies one provider run execution.
	CICDRunFactKind = "ci.run"
	// CICDJobFactKind identifies one provider job under a run.
	CICDJobFactKind = "ci.job"
	// CICDStepFactKind identifies one provider step under a job.
	CICDStepFactKind = "ci.step"
	// CICDArtifactFactKind identifies one artifact emitted by a provider run.
	CICDArtifactFactKind = "ci.artifact"
	// CICDTriggerEdgeFactKind identifies an explicit run trigger or upstream
	// run edge reported by the provider.
	CICDTriggerEdgeFactKind = "ci.trigger_edge"
	// CICDEnvironmentObservationFactKind identifies an environment observation
	// reported by a provider run, job, or deployment gate.
	CICDEnvironmentObservationFactKind = "ci.environment_observation"
	// CICDDeploymentEventFactKind identifies one provider deployment or
	// deployment-status event (for example a GitHub Deployments API
	// deployment/deployment_status pair) reported against a run's target
	// environment and commit.
	CICDDeploymentEventFactKind = "ci.deployment_event"
	// CICDWarningFactKind identifies non-fatal CI/CD collection warnings.
	CICDWarningFactKind = "ci.warning"

	// CICDSchemaVersion is the first CI/CD run fact schema.
	CICDSchemaVersion = "1.0.0"
)

var cicdRunFactKinds = []string{
	CICDPipelineDefinitionFactKind,
	CICDWorkflowImageEvidenceFactKind,
	CICDRunFactKind,
	CICDJobFactKind,
	CICDStepFactKind,
	CICDArtifactFactKind,
	CICDTriggerEdgeFactKind,
	CICDEnvironmentObservationFactKind,
	CICDDeploymentEventFactKind,
	CICDWarningFactKind,
}

var cicdRunSchemaVersions = map[string]string{
	CICDPipelineDefinitionFactKind:     CICDSchemaVersion,
	CICDWorkflowImageEvidenceFactKind:  CICDSchemaVersion,
	CICDRunFactKind:                    CICDSchemaVersion,
	CICDJobFactKind:                    CICDSchemaVersion,
	CICDStepFactKind:                   CICDSchemaVersion,
	CICDArtifactFactKind:               CICDSchemaVersion,
	CICDTriggerEdgeFactKind:            CICDSchemaVersion,
	CICDEnvironmentObservationFactKind: CICDSchemaVersion,
	CICDDeploymentEventFactKind:        CICDSchemaVersion,
	CICDWarningFactKind:                CICDSchemaVersion,
}

// CICDRunFactKinds returns the accepted CI/CD run fact kinds in provider
// emission order.
func CICDRunFactKinds() []string {
	return slices.Clone(cicdRunFactKinds)
}

// CICDRunSchemaVersion returns the schema version for a CI/CD run fact kind.
func CICDRunSchemaVersion(factKind string) (string, bool) {
	version, ok := cicdRunSchemaVersions[factKind]
	return version, ok
}
