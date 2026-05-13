// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS source observations up to durable fact envelopes. It
// does not call AWS APIs directly, schedule workflow claims, or write graph
// truth. Service-specific scanners convert AWS API data into these contracts,
// then the shared collector and reducer paths persist and materialize them.
// Sensitive service fields, including ECS and Lambda environment values, must
// be redacted before callers build envelopes.
package awscloud
