// Package awssdk adapts AWS SDK for Go v2 EKS and IAM responses into
// scanner-owned EKS records.
//
// The package owns EKS API pagination, Describe API enrichment, IAM OIDC
// provider lookup, per-call telemetry, and response mapping for one claimed AWS
// boundary. It returns only stable reported control-plane metadata; callers
// receive no credential material, Kubernetes tokens, or live workload objects.
package awssdk
