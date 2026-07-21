// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "time"

// TerraformStateResourceRow carries one Terraform state resource instance for
// canonical graph projection.
type TerraformStateResourceRow struct {
	UID                string
	Address            string
	Mode               string
	ResourceType       string
	Name               string
	ModuleAddress      string
	ProviderAddress    string
	Lineage            string
	Serial             int64
	BackendKind        string
	LocatorHash        string
	StatePath          string
	SourceFactID       string
	StableFactKey      string
	SourceSystem       string
	SourceRecordID     string
	SourceConfidence   string
	CollectorKind      string
	CorrelationAnchors []string
	TagKeyHashes       []string
	// Attributes is the collector's classified Terraform resource-attribute
	// object, carried unmodified from tfstatev1.Resource.Attributes (#5441).
	// It is untyped provider-specific pass-through, exactly as documented on
	// that field; the canonical graph writer
	// (go/internal/storage/cypher/terraform_attribute_promotion.go) is
	// responsible for reducing it to a bounded, redaction-safe, allowlisted
	// subset before any value reaches a graph node. Nothing in this package
	// filters or persists it directly.
	Attributes map[string]any
	ObservedAt time.Time
	// OwningRepoID is the config repository resolved to own this resource's
	// Terraform backend (#5443), matched by (BackendKind, LocatorHash) the
	// same way tfstatebackend.Resolver.ResolveConfigCommitForBackend already
	// resolves ownership for drift correlation. Empty when ownership could
	// not be resolved (no config repo has emitted a matching
	// terraform_backends fact, or more than one repo claims the same
	// backend). Populated by an enrichment pass the graph writer runs before
	// projection — extractTerraformStateRows itself stays pure and has no
	// database access, so this field is always empty immediately after
	// extraction and is filled in later, outside this package.
	OwningRepoID string
	// ConfigMatchAmbiguous is true when the MATCHES_STATE config-edge lookup
	// (repo_id: OwningRepoID, name: Address) matches more than one
	// TerraformResource node (#5443 P1 review finding): no uniqueness
	// constraint backs (repo_id, name) -- tf_resource_unique is (name, path,
	// line_number) -- so two Terraform roots in one monorepo can both
	// declare the same address (e.g. two "aws_instance.web" resources under
	// different environments). Like OwningRepoID, this stays false
	// immediately after extraction (extractTerraformStateRows has no
	// database access) and is filled in later by an enrichment pass the
	// graph writer runs before projection
	// (CanonicalNodeWriter.resolveTerraformStateConfigMatchAmbiguity). The
	// writer MUST NOT write a MATCHES_STATE edge for a row with this set:
	// silently picking one candidate would mislink state to the wrong
	// config declaration, and this repository's own precedent is to record
	// ambiguity honestly rather than force a guess.
	ConfigMatchAmbiguous bool
}

// TerraformStateModuleRow carries one Terraform module observed in state.
type TerraformStateModuleRow struct {
	UID              string
	ModuleAddress    string
	ResourceCount    int64
	Lineage          string
	Serial           int64
	BackendKind      string
	LocatorHash      string
	StatePath        string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

// TerraformStateOutputRow carries one Terraform output observed in state.
type TerraformStateOutputRow struct {
	UID              string
	Name             string
	Sensitive        bool
	ValueShape       string
	Lineage          string
	Serial           int64
	BackendKind      string
	LocatorHash      string
	StatePath        string
	SourceFactID     string
	StableFactKey    string
	SourceSystem     string
	SourceRecordID   string
	SourceConfidence string
	CollectorKind    string
	ObservedAt       time.Time
}

// terraformStateSnapshotContext carries the lineage/serial/backend identity
// of the most recent terraform_state_snapshot fact in a materialization
// batch, threaded through the resource/module/output row builders in
// tfstate_canonical.go so every row from one state file shares the same
// snapshot-level fields.
type terraformStateSnapshotContext struct {
	Lineage     string
	Serial      int64
	BackendKind string
	LocatorHash string
	StatePath   string
}
