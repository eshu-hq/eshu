// Package awssdk adapts the AWS SDK for Go v2 IAM client to the IAM scanner
// contract.
//
// The package owns IAM pagination, AWS API telemetry, throttle detection, and
// trust policy decoding for source records returned by AWS. Scanner packages
// own fact selection and do not import the AWS SDK directly.
package awssdk
