// Package mediadoc builds source-neutral documentation facts from reviewed
// local media transcript results.
//
// The package runs media metadata preflight first, then passes safe local media
// inputs to an injected transcript engine. It emits only documentation document
// and section facts with transcript_chunk incident-media provenance; it does
// not infer services, deployments, ownership, incidents, graph edges, or claim
// candidates from transcript text. Unsafe source locations and source identity
// fields are redacted before persistence. Runtime enablement, sandboxing,
// telemetry wiring, codec dependencies, and security review remain caller-owned
// before any hosted extraction path is turned on.
package mediadoc
