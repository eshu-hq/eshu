// Package ociregistry normalizes OCI container registry evidence before it
// enters the durable fact envelope.
//
// The package belongs to the collector boundary for the future oci_registry
// collector family. It owns repository and digest identity normalization plus
// reported-confidence envelope builders for repositories, mutable tag
// observations, manifests, image indexes, descriptors, referrers, and warnings,
// including computed-manifest-digest warnings when registry digest headers are
// absent. Builders validate boundary fields, keep tag evidence separate from
// digest identity, make FactID generation-specific, and redact unknown
// annotations or credential-bearing URLs. Provider adapters for Docker Hub,
// GHCR, ECR, JFrog, Harbor, Google Artifact Registry, and Azure Container
// Registry live in subpackages, and ociruntime calls those clients to produce
// collected generations. This root package does not call live registries,
// materialize graph truth, or answer queries.
package ociregistry
