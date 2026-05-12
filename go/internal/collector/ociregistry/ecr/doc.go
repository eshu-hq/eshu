// Package ecr adapts Amazon ECR registry coordinates to the provider-neutral
// OCI registry contract.
//
// The package owns ECR registry URI construction and the seam where an AWS
// GetAuthorizationToken result becomes Distribution basic auth credentials. It
// keeps AWS profile, region, target registry host, and STS policy outside the
// fact model so callers can wire runtime-specific credential behavior.
package ecr
