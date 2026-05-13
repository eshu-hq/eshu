// Package awssdk adapts the AWS SDK for Go v2 ECS client to the ECS scanner
// contract.
//
// The package owns ECS pagination, batched describe calls, SDK response
// mapping, AWS API telemetry, throttle detection, and pagination spans.
package awssdk
