// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// TerraformStateCandidateFactKind identifies one repo-local Terraform state
	// file candidate observed by the Git collector without raw state content.
	TerraformStateCandidateFactKind = "terraform_state_candidate"
	// TerraformStateSnapshotFactKind identifies a Terraform state snapshot fact.
	TerraformStateSnapshotFactKind = "terraform_state_snapshot"
	// TerraformStateResourceFactKind identifies one resource instance observed
	// from Terraform state.
	TerraformStateResourceFactKind = "terraform_state_resource"
	// TerraformStateOutputFactKind identifies one output observed from Terraform state.
	TerraformStateOutputFactKind = "terraform_state_output"
	// TerraformStateModuleFactKind identifies one module observed from Terraform state.
	TerraformStateModuleFactKind = "terraform_state_module"
	// TerraformStateProviderBindingFactKind identifies a provider binding observed
	// from Terraform state.
	TerraformStateProviderBindingFactKind = "terraform_state_provider_binding"
	// TerraformStateTagObservationFactKind identifies one resource tag observed
	// from Terraform state.
	TerraformStateTagObservationFactKind = "terraform_state_tag_observation"
	// TerraformStateWarningFactKind identifies a non-fatal Terraform state warning.
	TerraformStateWarningFactKind = "terraform_state_warning"

	// TerraformStateCandidateSchemaVersion is the first candidate fact schema.
	TerraformStateCandidateSchemaVersion = "1.0.0"
	// TerraformStateSnapshotSchemaVersion is the first snapshot fact schema.
	TerraformStateSnapshotSchemaVersion = "1.0.0"
	// TerraformStateResourceSchemaVersion is the first resource fact schema.
	TerraformStateResourceSchemaVersion = "1.0.0"
	// TerraformStateOutputSchemaVersion is the first output fact schema.
	TerraformStateOutputSchemaVersion = "1.0.0"
	// TerraformStateModuleSchemaVersion is the first module fact schema.
	TerraformStateModuleSchemaVersion = "1.0.0"
	// TerraformStateProviderBindingSchemaVersion is the first provider binding
	// fact schema.
	TerraformStateProviderBindingSchemaVersion = "1.0.0"
	// TerraformStateTagObservationSchemaVersion is the first tag observation
	// fact schema.
	TerraformStateTagObservationSchemaVersion = "1.0.0"
	// TerraformStateWarningSchemaVersion is the first warning fact schema.
	TerraformStateWarningSchemaVersion = "1.0.0"
)

var terraformStateFactKinds = []string{
	TerraformStateCandidateFactKind,
	TerraformStateSnapshotFactKind,
	TerraformStateResourceFactKind,
	TerraformStateOutputFactKind,
	TerraformStateModuleFactKind,
	TerraformStateProviderBindingFactKind,
	TerraformStateTagObservationFactKind,
	TerraformStateWarningFactKind,
}

var terraformStateSchemaVersions = map[string]string{
	TerraformStateCandidateFactKind:       TerraformStateCandidateSchemaVersion,
	TerraformStateSnapshotFactKind:        TerraformStateSnapshotSchemaVersion,
	TerraformStateResourceFactKind:        TerraformStateResourceSchemaVersion,
	TerraformStateOutputFactKind:          TerraformStateOutputSchemaVersion,
	TerraformStateModuleFactKind:          TerraformStateModuleSchemaVersion,
	TerraformStateProviderBindingFactKind: TerraformStateProviderBindingSchemaVersion,
	TerraformStateTagObservationFactKind:  TerraformStateTagObservationSchemaVersion,
	TerraformStateWarningFactKind:         TerraformStateWarningSchemaVersion,
}

// TerraformStateFactKinds returns the accepted Terraform state fact kinds in
// their emission order.
func TerraformStateFactKinds() []string {
	return slices.Clone(terraformStateFactKinds)
}

// TerraformStateSchemaVersion returns the schema version for a Terraform state
// fact kind.
func TerraformStateSchemaVersion(factKind string) (string, bool) {
	version, ok := terraformStateSchemaVersions[factKind]
	return version, ok
}

// TerraformStateFamilyDeclaration is the typed fact-family contract declaration for the
// terraform_state family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var TerraformStateFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "terraform_state",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "config_state_drift",
	ProjectionHook:         "terraform_relationship_projection",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/iac/resources",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "terraform_state_candidate", PayloadSchema: "sdk/go/factschema/schema/terraform_state_candidate.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Intentionally not consumed; the terraform-state canonical extractor documents it as unhandled.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries); projector terraform-state extractor doc comment"},
		{Kind: "terraform_state_module", PayloadSchema: "sdk/go/factschema/schema/terraform_state_module.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode, go/internal/projector/runtime"},
		{Kind: "terraform_state_output", PayloadSchema: "sdk/go/factschema/schema/terraform_state_output.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "terraform_state_provider_binding", PayloadSchema: "sdk/go/factschema/schema/terraform_state_provider_binding.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "terraform_state_resource", PayloadSchema: "sdk/go/factschema/schema/terraform_state_resource.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode, go/internal/projector/runtime"},
		{Kind: "terraform_state_snapshot", PayloadSchema: "sdk/go/factschema/schema/terraform_state_snapshot.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode, go/internal/projector/runtime"},
		{Kind: "terraform_state_tag_observation", PayloadSchema: "sdk/go/factschema/schema/terraform_state_tag_observation.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "terraform_state_warning", PayloadSchema: "sdk/go/factschema/schema/terraform_state_warning.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Intentionally not consumed; the terraform-state canonical extractor documents it as unhandled.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries); projector terraform-state extractor doc comment"},
	},
	Producers: []contract.Producer{
		{ID: "collector-terraform-state", Repository: "eshu-hq/eshu", Path: "go/internal/collector/terraformstate", PlannedRepository: "eshu-hq/eshu-collector-terraform-state", AuthorityMode: "core_issued_grant", Kinds: []string{"terraform_state_module", "terraform_state_output", "terraform_state_provider_binding", "terraform_state_resource", "terraform_state_snapshot", "terraform_state_tag_observation", "terraform_state_warning"}},
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: "core_owned", Kinds: []string{"terraform_state_candidate"}},
	},
})
