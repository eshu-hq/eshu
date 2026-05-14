// Package awssdk adapts AWS SDK for Go v2 DynamoDB responses to the
// scanner-owned DynamoDB metadata contract.
//
// The package owns DynamoDB control-plane pagination, table point reads, tag
// reads, throttle classification, and per-call AWS API telemetry. It does not
// own workflow claims, credential acquisition, fact selection, graph writes,
// reducer admission, workload ownership, or query behavior.
package awssdk
