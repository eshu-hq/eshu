// Package secretsiam builds redaction-safe secrets/IAM posture source facts.
//
// The package owns envelope construction for the secrets_iam_posture collector
// family. Callers provide already-normalized AWS IAM, GCP IAM (service-account
// principal and permission-grant), Kubernetes ServiceAccount/RBAC, or Vault
// metadata observations; this package stamps
// collector identity, stable IDs, reported confidence, and metadata-only
// payloads. Reducers remain responsible for all trust-chain, permission,
// effective RBAC, Vault policy interpretation, and graph promotion decisions.
package secretsiam
