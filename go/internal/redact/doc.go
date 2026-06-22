// Package redact provides collector-neutral redaction classification, keyed
// markers, and hosted governance leakage canaries for sensitive values before
// they cross persistence or telemetry boundaries.
//
// Callers must provide deployment-scoped key material through NewKey before
// producing markers. The package rejects blank key material, normalizes missing
// reason/source labels to "unknown", and fails closed for unsupported scalar
// types by hashing their type class instead of serializing raw values. The
// hosted registry names allowed low-cardinality field classes and forbidden
// synthetic canaries for facts, logs, metrics, status, graph, API/MCP, audit,
// console, docs, and onboarding surfaces.
package redact
