// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Principal is the schema-version-1 typed payload for the "aws_iam_principal"
// fact kind: one AWS IAM principal (role or user) source fact.
//
// The required set matches the collector emitter
// (secretsiam.NewPrincipalEnvelope), which validates principal_arn and
// principal_type non-empty and always emits account_id and region from the scan
// context. In this migration only the named secrets/IAM trust-chain IAM-role
// helper decodes this kind, and it reads only AccountID and Region to compute a
// redaction-safe CloudResource uid; PrincipalARN and PrincipalType are modeled
// (and required, matching the emitter) so the struct is a faithful contract even
// though the migrated call site does not read them.
type Principal struct {
	// AccountID is the AWS account the principal belongs to. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS scan-boundary region for the principal. Required — it,
	// with AccountID, completes the CloudResource uid the reducer computes for
	// the assumed IAM role (the ARN carries the rest).
	Region string `json:"region"`

	// PrincipalARN is the principal's ARN. Required — the emitter rejects a
	// principal with no ARN.
	PrincipalARN string `json:"principal_arn"`

	// PrincipalID is the collector's stable principal identity mirror. Optional
	// for compatibility with older payloads that only carried principal_arn.
	PrincipalID *string `json:"principal_id,omitempty"`

	// PrincipalType is the principal type token (for example "role" or
	// "user"). Required — the emitter rejects a principal with no type.
	PrincipalType string `json:"principal_type"`

	// Provider is the source provider token ("aws_iam") emitted by the
	// secrets_iam collector. Optional for compatibility with older payloads
	// that only carried the identity fields the reducer consumed.
	Provider *string `json:"provider,omitempty"`

	// CollectorInstanceID is the collector instance boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// RedactionPolicyVersion records the source-family redaction contract used
	// to produce this metadata-only payload. Optional for older payloads.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// Name is the redaction-safe principal display name emitted by the
	// collector when it is permitted by the source contract. Optional.
	Name *string `json:"name,omitempty"`

	// Path is the IAM path emitted by the collector. Optional.
	Path *string `json:"path,omitempty"`

	// URLFingerprint is the redaction-safe fingerprint of the principal URL, if
	// the source observed one. Optional.
	URLFingerprint *string `json:"url_fingerprint,omitempty"`

	// URLPresent records whether a principal URL was observed without storing
	// the URL itself. Optional.
	URLPresent *bool `json:"url_present,omitempty"`

	// ClientIDCount records OIDC provider client-id cardinality without storing
	// raw client IDs. Optional.
	ClientIDCount *int `json:"client_id_count,omitempty"`

	// ThumbprintCount records OIDC provider thumbprint cardinality without
	// storing raw thumbprints. Optional.
	ThumbprintCount *int `json:"thumbprint_count,omitempty"`

	// CorrelationHints carries bounded, normalized correlation hint tokens.
	// Optional.
	CorrelationHints []string `json:"correlation_hints,omitempty"`
}
