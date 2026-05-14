// Package awssdk adapts AWS SDK for Go v2 Secrets Manager responses into the
// scanner-owned metadata model.
//
// The adapter pages ListSecrets only and records bounded AWS API telemetry for
// each page. It deliberately avoids value, version, resource-policy, and
// mutation APIs so the service package never receives secret material.
package awssdk
