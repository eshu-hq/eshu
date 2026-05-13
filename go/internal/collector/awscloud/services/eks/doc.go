// Package eks turns Amazon EKS cluster, node group, add-on, and OIDC provider
// observations into AWS collector facts.
//
// The package owns scanner-side EKS models and fact-envelope selection for the
// AWS cloud collector. It preserves reported cluster, IRSA, node-role, add-on,
// subnet, and security-group evidence without calling AWS APIs directly or
// materializing canonical graph truth.
package eks
