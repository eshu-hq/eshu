// Package awssdk adapts AWS SDK for Go v2 CloudWatch Logs responses to the
// scanner-owned metadata contract.
//
// The adapter owns DescribeLogGroups pagination with Limit=50, log group tag
// reads through the non-wildcard ARN, throttle classification, and per-call AWS
// API telemetry. It intentionally avoids log events, log stream payloads,
// Insights query results, export payloads, resource policies, subscription
// payloads, and mutation APIs.
package awssdk
