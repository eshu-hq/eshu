// Package cloudwatchlogs converts Amazon CloudWatch Logs log group metadata
// into AWS collector fact envelopes.
//
// The package owns scanner-level fact selection for log groups: identity,
// retention, storage size, metric filter count, class, data protection status,
// inherited properties, deletion protection, bearer-token authentication state,
// tags, and reported KMS dependency evidence. It intentionally excludes log
// events, log stream payloads, Insights query results, export payloads,
// resource policies, subscription payloads, and mutations.
package cloudwatchlogs
