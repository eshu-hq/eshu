// Package secretsmanager converts AWS Secrets Manager metadata into AWS
// collector fact envelopes.
//
// Scanner accepts only the Secrets Manager service boundary and emits reported
// secret metadata plus direct KMS and rotation Lambda relationship evidence. It
// deliberately excludes secret values, version payloads, resource policy JSON,
// partner rotation metadata, and mutations so downstream reducers can reason
// about dependencies without the collector handling secret material.
package secretsmanager
