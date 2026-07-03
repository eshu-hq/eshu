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

	// PrincipalType is the principal type token (for example "role" or
	// "user"). Required — the emitter rejects a principal with no type.
	PrincipalType string `json:"principal_type"`
}
