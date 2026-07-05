// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Snapshot is the schema-version-1 typed payload for the
// "terraform_state_snapshot" fact kind (Contract System v1 §3.1).
//
// A snapshot carries the per-state-file provenance the projector folds onto
// every resource/module/output node it materializes from the same generation
// (lineage, serial, backend kind, locator hash). The projector reads these
// best-effort (go/internal/projector/tfstate_canonical.go
// terraformStateSnapshot) and tolerates any of them being empty — it derives a
// fallback state path from the scope id when backend_kind or locator_hash is
// blank — so NO snapshot field's absence produces a broken graph identity.
// Every field is therefore OPTIONAL: marking one required would turn a
// today-valid incomplete snapshot into an input_invalid dead-letter, an
// accuracy regression Contract System v1 forbids.
//
// The collector emitter (terraformstate.stateParser.snapshotFact) always writes
// format_version, terraform_version, serial, lineage, backend_kind,
// locator_hash, and source_size_bytes, and conditionally writes etag, so an
// absent value is not a shape the emitter produces; the optional typing simply
// preserves the pre-typing tolerance rather than asserting a stricter contract
// the read path does not require.
type Snapshot struct {
	// Lineage is the Terraform state lineage id, the stable identity a
	// snapshot's resources/modules/outputs are grouped under. Optional: read
	// best-effort by the projector, which tolerates an empty lineage.
	Lineage *string `json:"lineage,omitempty"`

	// Serial is the monotonically increasing Terraform state serial. Optional:
	// a pointer so an absent serial stays distinct from an observed 0.
	Serial *int64 `json:"serial,omitempty"`

	// BackendKind is the state backend kind (for example "s3" or "local").
	// Optional: the projector uses it to derive the state path and falls back to
	// the scope id when it is empty.
	BackendKind *string `json:"backend_kind,omitempty"`

	// LocatorHash is the hashed backend locator (bucket/key or absolute path)
	// identifying the state file. Optional: also folded into the state path with
	// a scope-id fallback.
	LocatorHash *string `json:"locator_hash,omitempty"`

	// FormatVersion is the Terraform state file format version. Optional:
	// emitted for provenance, not read by the projector today.
	FormatVersion *string `json:"format_version,omitempty"`

	// TerraformVersion is the Terraform version that produced the state.
	// Optional: emitted for provenance, not read by the projector today.
	TerraformVersion *string `json:"terraform_version,omitempty"`

	// SourceSizeBytes is the observed size of the state source in bytes.
	// Optional pointer so nil (unreported) stays distinct from an observed 0.
	SourceSizeBytes *int64 `json:"source_size_bytes,omitempty"`

	// ETag is the backend object etag, when the backend reported one. Optional:
	// the emitter writes it only when non-empty.
	ETag *string `json:"etag,omitempty"`
}

// Resource is the schema-version-1 typed payload for the
// "terraform_state_resource" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per resource instance observed in Terraform state. The
// projector materializes a canonical TerraformStateResource node keyed by a uid
// folded from the resource Address (go/internal/projector/tfstate_canonical.go
// terraformStateResourceRow), which DROPS a resource whose address is empty
// rather than fabricating a node. Address is therefore the sole REQUIRED
// identity field: an absent address must dead-letter as input_invalid, not
// silently produce an empty-address node or vanish with no operator signal.
//
// The remaining named fields (mode, type, name, module, provider) are common
// descriptive fields the projector copies onto the node; each is OPTIONAL
// because the projector tolerates an empty value and the resource still
// materializes on its address alone. CorrelationAnchors is the redacted
// name/URI anchor list the projector normalizes for name-only correlation.
type Resource struct {
	// Address is the fully-qualified Terraform resource instance address (for
	// example "module.vpc.aws_subnet.public[0]"). Required — it is the resource
	// node's identity; an absent address dead-letters rather than fabricating an
	// empty-identity node.
	Address string `json:"address"`

	// Mode is the resource mode ("managed" or "data"). Optional.
	Mode *string `json:"mode,omitempty"`

	// ResourceType is the Terraform resource type (for example "aws_subnet").
	// Optional: copied onto the node when present.
	ResourceType *string `json:"type,omitempty"`

	// Name is the resource's local name within its module. Optional.
	Name *string `json:"name,omitempty"`

	// Module is the resource's module address ("" for the root module).
	// Optional.
	Module *string `json:"module,omitempty"`

	// Provider is the provider address the resource is bound to. Optional.
	Provider *string `json:"provider,omitempty"`

	// CorrelationAnchors are the redaction-safe {anchor_kind, value_hash} anchor
	// objects the collector published for name-only join resolution. Optional: a
	// resource with no derivable anchors omits the key. Each element is an
	// object; the projector reads anchor_kind and value_hash from each.
	CorrelationAnchors []map[string]any `json:"correlation_anchors,omitempty"`

	// Attributes is the collector's classified Terraform resource-attribute
	// object (arn, id, self_link, and other provider-specific keys). The
	// collector emits it unconditionally on every terraform_state_resource
	// fact (go/internal/collector/terraformstate/resources.go emitResourceInstance).
	// It is UNTYPED because the key set is provider- and resource-type-specific;
	// it is carried as a pass-through so the contract round-trips it losslessly.
	// The Postgres drift loaders read arn/id/self_link out of this object to
	// join Terraform state to observed cloud resources
	// (go/internal/storage/postgres/*_cloud_runtime_drift_evidence_sql.go), so
	// dropping it would silently break AWS and multi-cloud drift matching.
	// Optional: a resource with no classified attributes omits the key.
	Attributes map[string]any `json:"attributes,omitempty"`
}
