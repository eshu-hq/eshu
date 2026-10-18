// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// CICDRunFamilyDeclaration is the typed fact-family contract declaration for the
// ci_cd_run family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var CICDRunFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "ci_cd_run",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "ci_cd_run_correlation",
	ProjectionHook:         "ci_cd_run_correlation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/ci-cd/run-correlations",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "ci.artifact", PayloadSchema: "sdk/go/factschema/schema/ci.artifact.v1.schema.json"},
		{Kind: "ci.deployment_event", PayloadSchema: "sdk/go/factschema/schema/ci.deployment_event.v1.schema.json"},
		{Kind: "ci.environment_observation", PayloadSchema: "sdk/go/factschema/schema/ci.environment_observation.v1.schema.json"},
		{Kind: "ci.job", Disposition: "unconsumed", NoReducerReason: "Emitted by the CI/CD collector with no reducer decode call today (deferred by design).", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "ci.pipeline_definition", Disposition: "unconsumed", NoReducerReason: "Emitted by the CI/CD collector with no reducer decode call today (deferred by design).", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "ci.run", PayloadSchema: "sdk/go/factschema/schema/ci.run.v1.schema.json"},
		{Kind: "ci.step", PayloadSchema: "sdk/go/factschema/schema/ci.step.v1.schema.json"},
		{Kind: "ci.trigger_edge", PayloadSchema: "sdk/go/factschema/schema/ci.trigger_edge.v1.schema.json"},
		{Kind: "ci.warning", Disposition: "unconsumed", NoReducerReason: "Emitted by the CI/CD collector with no reducer decode call today (deferred by design).", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries)"},
		{Kind: "ci.workflow_image_evidence", PayloadSchema: "sdk/go/factschema/schema/ci.workflow_image_evidence.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-cicd-run", Repository: "eshu-hq/eshu", Path: "go/internal/collector/cicdrun", PlannedRepository: "eshu-hq/eshu-collector-cicd-run", AuthorityMode: "core_issued_grant", Kinds: []string{"ci.artifact", "ci.deployment_event", "ci.environment_observation", "ci.job", "ci.pipeline_definition", "ci.run", "ci.step", "ci.trigger_edge", "ci.warning"}},
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: "core_owned", Kinds: []string{"ci.workflow_image_evidence"}},
	},
})
