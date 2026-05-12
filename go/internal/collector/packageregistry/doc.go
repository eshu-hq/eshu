// Package packageregistry normalizes package-registry evidence before it enters
// the durable fact envelope.
//
// The package belongs to the collector boundary: it observes and normalizes
// package/feed metadata, then emits reported-confidence facts through
// NewPackageEnvelope and NewPackageVersionEnvelope. NormalizePackageIdentity
// keeps ecosystem identity rules separate so package facts stay idempotent
// across retries and replay. The package does not claim ownership, dependency
// truth, graph nodes, or query answers; reducers must corroborate registry facts
// with source, build, lockfile, or runtime evidence before promoting canonical
// relationships.
package packageregistry
