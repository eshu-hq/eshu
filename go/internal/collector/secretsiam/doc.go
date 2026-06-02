// Package secretsiam builds redaction-safe secrets/IAM posture source facts.
//
// The package owns envelope construction for the secrets_iam_posture collector
// family. Callers provide already-normalized provider observations; this package
// stamps collector identity, stable IDs, reported confidence, and metadata-only
// payloads. Reducers remain responsible for all trust-chain, permission, and
// graph promotion decisions.
package secretsiam
