// Package packageruntime implements the claim-driven runtime for
// package-registry metadata collection.
//
// The runtime receives workflow work items planned from bounded
// package-registry targets, fetches one explicit metadata document for the
// claimed scope, routes it through the packageregistry parser registry, and
// returns package-registry fact envelopes through the shared collector commit
// boundary. Package-version observations, including advisories and registry
// events, stay inside the configured target's package and version limits. The
// runtime keeps provider credentials in runtime-only fields and records bounded
// telemetry by provider, ecosystem, status class, document type, and fact kind.
package packageruntime
