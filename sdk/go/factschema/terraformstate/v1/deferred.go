// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Candidate and Warning in this file are TYPED-BUT-NOT-YET-CONSUMED (see
// doc.go). No read-side decode consumer exists for their fact kinds today, so
// this wave ships their struct, schema, and fixture pack without converting a
// decode site, adding a regression test, or benchmarking a read path (there is
// none to benchmark). The decode-site conversion, the input_invalid
// regression test, and the No-Regression benchmark land in the change that
// first reads each kind, matching how the gcp family typed
// gcp_image_reference / gcp_tag_observation ahead of their shared consumer.
//
// ProviderBinding gained its first projector consumer in #5446
// (go/internal/projector/tfstate_canonical.go's
// terraformStateProviderBindingsByResource, wrapped by
// decodeTerraformStateProviderBinding in
// go/internal/projector/factschema_decode_terraformstate.go) — it is no
// longer typed-but-unconsumed; its input_invalid regression coverage and
// No-Regression benchmark now exist alongside that consumer.
//
// Their required sets still follow the absent-vs-present-empty rule against the
// collector emitter's own fail-closed invariants, so the contract is correct
// and ready the moment a consumer arrives (or, for ProviderBinding, already did).

// Candidate is the schema-version-1 typed payload for the
// "terraform_state_candidate" fact kind (Contract System v1 §3.1).
//
// A candidate fact is Git-discovery provenance: the collector observed a file
// that looks like a Terraform state file and records where it found it, without
// parsing its contents (go/internal/collector/tfstate_candidate.go). The
// emitter unconditionally writes candidate_source, backend_kind, repo_id,
// relative_path, and path_hash, so those five are REQUIRED — an absent one
// means the discovery record cannot identify the candidate. FileSize and
// WarningFlags are OPTIONAL descriptive fields.
type Candidate struct {
	// CandidateSource is the discovery source label (for example "git"). Required.
	CandidateSource string `json:"candidate_source"`

	// BackendKind is the inferred backend kind for the candidate. Required.
	BackendKind string `json:"backend_kind"`

	// RepoID is the repository the candidate file was discovered in. Required.
	RepoID string `json:"repo_id"`

	// RelativePath is the repo-relative path of the candidate file. Required —
	// with RepoID it identifies which file the candidate is.
	RelativePath string `json:"relative_path"`

	// PathHash is the keyed hash of the candidate path, the candidate's stable
	// identity. Required.
	PathHash string `json:"path_hash"`

	// FileSize is the observed candidate file size in bytes. Optional pointer so
	// nil (unreported) stays distinct from an observed 0.
	FileSize *int64 `json:"file_size,omitempty"`

	// WarningFlags carries discovery-time warning tags (for example a size
	// warning). Optional: empty when the candidate raised no discovery warning.
	WarningFlags []string `json:"warning_flags,omitempty"`
}

// ProviderBinding is the schema-version-1 typed payload for the
// "terraform_state_provider_binding" fact kind (Contract System v1 §3.1).
//
// A provider binding records which provider a resource instance is bound to in
// state (go/internal/collector/terraformstate/providers.go). The emitter fails
// closed on an empty resource_address or provider_address (it returns early
// without emitting), so those two are REQUIRED join keys — an absent one is a
// binding that cannot connect a resource to a provider. The parsed provider
// address components (source, hostname, namespace, type, alias) are OPTIONAL:
// the emitter writes each only when non-empty.
type ProviderBinding struct {
	// ResourceAddress is the address of the resource the provider is bound to.
	// Required — the first half of the resource→provider binding identity.
	ResourceAddress string `json:"resource_address"`

	// ProviderAddress is the full provider address the resource is bound to.
	// Required — the second half of the binding identity.
	ProviderAddress string `json:"provider_address"`

	// ProviderSourceAddress is the provider's registry source address (for
	// example "hashicorp/aws"). Optional: written only when parsed non-empty.
	ProviderSourceAddress *string `json:"provider_source_address,omitempty"`

	// ProviderHostname is the provider registry hostname. Optional.
	ProviderHostname *string `json:"provider_hostname,omitempty"`

	// ProviderNamespace is the provider registry namespace. Optional.
	ProviderNamespace *string `json:"provider_namespace,omitempty"`

	// ProviderType is the provider type (for example "aws"). Optional.
	ProviderType *string `json:"provider_type,omitempty"`

	// ProviderAlias is the provider alias, when the binding uses an aliased
	// provider configuration. Optional.
	ProviderAlias *string `json:"provider_alias,omitempty"`
}

// Warning is the schema-version-1 typed payload for the
// "terraform_state_warning" fact kind (Contract System v1 §3.1).
//
// A warning records a non-fatal Terraform-state collection condition
// (go/internal/collector/terraformstate/warning_fact.go). The emitter fails
// closed on a blank warning_kind, reason, or source, so those three are
// REQUIRED. Severity and Actionability are OPTIONAL: the emitter adds them only
// when the warning kind classifies. The warning carries additional free-form
// detail keys the emitter flattens to the top level; because no consumer reads
// them today they are neither modeled nor passed through — the decode seam
// ignores unmodeled keys, and the generated schema permits them via
// additionalProperties, so an emitted warning with extra details still decodes
// and conforms.
type Warning struct {
	// WarningKind is the bounded warning category. Required.
	WarningKind string `json:"warning_kind"`

	// Reason is the human-readable explanation of the warning. Required.
	Reason string `json:"reason"`

	// Source is the redaction-safe source location the warning refers to.
	// Required.
	Source string `json:"source"`

	// Severity is the classified warning severity, when the warning kind
	// classifies. Optional.
	Severity *string `json:"severity,omitempty"`

	// Actionability is the classified operator actionability, when the warning
	// kind classifies. Optional.
	Actionability *string `json:"actionability,omitempty"`
}
