// Package projector owns source-local projection stages that turn committed
// facts into canonical graph writes and publish readiness for shared,
// reducer-owned domains.
//
// Stages in this package read fact envelopes, build canonical node and edge
// payloads, and hand them to the cypher writers. Projection must be
// idempotent: queue retries, duplicate claims, and partial graph writes
// must converge on the same graph truth instead of creating hidden second
// paths. Projector code does not make cross-source admission decisions;
// those belong to internal/reducer.
package projector
