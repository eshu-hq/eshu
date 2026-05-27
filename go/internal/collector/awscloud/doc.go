// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS service-kind constants, shared claim boundaries,
// reported-confidence fact envelope builders, scalar redaction helpers, and
// bounded scan-status accounting types. It does not call AWS APIs, schedule
// workflow claims, choose credentials, write graph truth, or answer queries.
//
// Service-specific scanners convert AWS API data into these contracts before
// the shared collector and reducer paths persist and materialize them. Those
// scanners must keep metadata-only services out of data-plane reads, secret
// values, policy persistence, payload capture, query result rows, named-query
// SQL bodies, prepared-statement query bodies, query history strings, and
// mutation APIs. ECS and Lambda environment values must be redacted through
// this package before callers build envelopes.
package awscloud
