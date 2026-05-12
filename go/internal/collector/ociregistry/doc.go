// Package ociregistry normalizes OCI container registry evidence before it
// enters the durable fact envelope.
//
// The package belongs to the collector boundary for the future oci_registry
// collector family. It owns repository and digest identity normalization plus
// reported-confidence envelope builders for repositories, mutable tag
// observations, manifests, image indexes, descriptors, referrers, and warnings.
// Builders validate boundary fields, keep tag evidence separate from digest
// identity, and redact unknown annotations or credential-bearing URLs. It does
// not call ECR, JFrog, or any live registry, and it does not materialize graph
// truth or answer queries.
package ociregistry
