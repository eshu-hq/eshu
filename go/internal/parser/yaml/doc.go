// Package yaml extracts YAML-family parser payloads without depending on the
// parent parser dispatch package.
//
// Parse reads one YAML source file and emits the payload buckets consumed by
// the parent parser and content materializer: Kubernetes resources, Argo CD
// applications, Crossplane resources, Kustomize overlays, Helm chart metadata,
// Helm values metadata, and CloudFormation/SAM template rows. DecodeDocuments
// and SanitizeTemplating remain available for parent compatibility paths that
// decode YAML-side metadata. Argo CD Application rows preserve the legacy
// singular source fields while adding positional source tuple fields that keep
// repo, path, revision, and root values aligned by source index. The package
// keeps output deterministic by sorting emitted buckets and by routing decoded
// CloudFormation documents through the shared CloudFormation parser contract.
package yaml
