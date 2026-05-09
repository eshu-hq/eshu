// Package redact provides collector-neutral redaction classification and keyed
// markers for sensitive values before they cross persistence or telemetry
// boundaries.
//
// Callers must provide deployment-scoped key material through NewKey before
// producing markers. The package rejects blank key material, normalizes missing
// reason/source labels to "unknown", and fails closed for unsupported scalar
// types by hashing their type class instead of serializing raw values.
package redact
