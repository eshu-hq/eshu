// Package relationships extracts Terraform, Helm, Kustomize, Argo CD, and
// related deployment evidence before reducer admission.
//
// The package describes evidence rather than inventing deployment truth:
// extractors emit candidate references, template parameters, and
// first-party reference signals that the reducer later admits or rejects.
// Ambiguous signals must remain ambiguous in the output of this package
// until a stronger contract admits them. Extractors should be
// deterministic over the same input bytes so repeated runs over a snapshot
// converge.
package relationships
