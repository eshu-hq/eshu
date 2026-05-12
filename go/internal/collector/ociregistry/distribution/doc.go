// Package distribution contains the OCI Distribution HTTP client used by
// provider adapters.
//
// The package implements bounded calls for the registry API surface Eshu needs
// before graph promotion: slash-preserving ping/challenge validation,
// repository-scoped token requests, tag listing, manifest or index retrieval,
// and referrer listing with repository paths escaped one segment at a time. It
// does not know about ECR, JFrog, Docker Hub, GHCR, or any provider-specific
// repository discovery contract; provider packages build base URLs and
// credentials, then delegate OCI wire calls here.
package distribution
