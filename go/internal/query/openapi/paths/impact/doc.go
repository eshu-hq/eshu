// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact holds the OpenAPI 3.0 path fragments for the change- and
// blast-radius-impact routes (Issue #6060, lane C): the exported Routes,
// Contract, Rest and Exposure, which openapi.Spec concatenates, plus the
// unexported deploymentConfigInfluence, which only this package reads.
//
// deploymentConfigInfluence is folded into Routes by a source-level `+` in
// routes.go rather than joined again in openapi/spec.go; do not add a
// second, separate concatenation of it there or the route doubles in the
// published document. Routes and deploymentConfigInfluence both import
// K8sResourceLimits from this package's k8s_resource_limits.go; Routes also uses
// schema.ImpactRuntimeTopologyLimits and schema.EvidenceBoundaries.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package impact
