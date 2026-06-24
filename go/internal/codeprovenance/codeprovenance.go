// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeprovenance

// Method is a value from the closed code-edge resolution-provenance vocabulary
// defined by ADR #2222. It records how a CALLS/REFERENCES/USES_METACLASS edge's
// target entity was resolved, so an agent can distinguish a semantically proven
// edge from a name-match guess. Method is descriptive, not admissive: it never
// gates edge creation.
type Method = string

// Closed resolution-provenance vocabulary (ADR #2222), strongest to weakest.
// The set is closed: every resolver branch maps to exactly one value and every
// value maps to exactly one derived confidence in confidenceByMethod. Adding a
// resolver branch requires extending this vocabulary, the confidence table, and
// the accuracy goldens (#2226) together.
const (
	// MethodSCIP marks an edge resolved by SCIP semantic symbol analysis, with
	// both endpoints bound by symbol to file and line.
	MethodSCIP Method = "scip"
	// MethodDeclared marks an edge explicitly declared in source and bound by
	// the parser (for example a Python metaclass), with no heuristic resolution.
	MethodDeclared Method = "declared"
	// MethodSameFile marks an edge resolved inside the caller's file by
	// lexical-scope span or same-file unique name.
	MethodSameFile Method = "same_file"
	// MethodImportBinding marks an edge resolved by following an explicit
	// import, Go package-qualified import, or re-export barrel.
	MethodImportBinding Method = "import_binding"
	// MethodTypeInferred marks an edge resolved through receiver or return-type
	// inference, dynamic-alias inference, or constructor binding.
	MethodTypeInferred Method = "type_inferred"
	// MethodScopeUniqueName marks an edge resolved by a unique name within a
	// bounded directory or package scope, with no import binding.
	MethodScopeUniqueName Method = "scope_unique_name"
	// MethodCrossRepoExportPackage marks an edge resolved across repositories by
	// matching a Go package-qualified call to the single exported top-level
	// function with that name whose defining package import path equals the
	// caller's import path. The match is anchored on the defining module's
	// declared module path and the import path string, not a SCIP or semantic
	// binding, so it sits at the same tier as scope_unique_name.
	MethodCrossRepoExportPackage Method = "cross_repo_export_package"
	// MethodRepoUniqueName marks an edge resolved by a repository-wide
	// unique-name match with no scope or import evidence; the global fallback.
	MethodRepoUniqueName Method = "repo_unique_name"
	// MethodUnspecified marks an edge whose method was not recorded: a legacy
	// edge written before ADR #2222, or a future branch not yet classified.
	// Readers must tolerate it; writers must not emit it for any classified
	// branch.
	MethodUnspecified Method = "unspecified"
)

// LegacyConfidence is the historical fixed confidence stamped on every code
// edge before ADR #2222. It is the fallback confidence for MethodUnspecified so
// un-reprojected edges keep their prior value.
const LegacyConfidence = 0.95

// confidenceByMethod is the single source of truth for the ADR #2222 tier
// table. Confidence is a derivation of Method, not an independent signal, so it
// lives in exactly one place and cannot drift per call site.
var confidenceByMethod = map[Method]float64{
	MethodSCIP:                   0.99,
	MethodDeclared:               0.95,
	MethodSameFile:               0.95,
	MethodImportBinding:          0.90,
	MethodTypeInferred:           0.80,
	MethodScopeUniqueName:        0.70,
	MethodCrossRepoExportPackage: 0.70,
	MethodRepoUniqueName:         0.50,
}

// reasonByMethod gives an operator reading a raw edge a short, mechanism-level
// explanation in place of the previous single fixed reason string.
var reasonByMethod = map[Method]string{
	MethodSCIP:                   "Resolved by SCIP semantic symbol analysis",
	MethodDeclared:               "Relationship explicitly declared in source",
	MethodSameFile:               "Resolved within the caller's file by lexical scope or unique name",
	MethodImportBinding:          "Resolved by following an explicit import or package binding",
	MethodTypeInferred:           "Resolved by receiver or return-type inference",
	MethodScopeUniqueName:        "Resolved by a unique name within a directory or package scope",
	MethodCrossRepoExportPackage: "Resolved by matching a cross-repository Go package import path to a single exported function",
	MethodRepoUniqueName:         "Resolved by a repository-wide unique-name match",
	MethodUnspecified:            "Resolution method not recorded",
}

// Confidence returns the derived confidence for a resolution method per the ADR
// #2222 tier table. Unknown or unspecified methods fall back to LegacyConfidence
// so historical and unclassified edges keep their prior value rather than being
// silently demoted.
func Confidence(method Method) float64 {
	if confidence, ok := confidenceByMethod[method]; ok {
		return confidence
	}
	return LegacyConfidence
}

// Reason returns the mechanism-level reason string for a resolution method.
// Unknown methods fall back to the MethodUnspecified reason.
func Reason(method Method) string {
	if reason, ok := reasonByMethod[method]; ok {
		return reason
	}
	return reasonByMethod[MethodUnspecified]
}

// Valid reports whether method is part of the closed vocabulary, including
// MethodUnspecified.
func Valid(method Method) bool {
	if method == MethodUnspecified {
		return true
	}
	_, ok := confidenceByMethod[method]
	return ok
}

// Classified reports whether method names a concrete resolver branch, excluding
// MethodUnspecified. Emitters must produce a Classified method for every edge
// they write; the accuracy goldens (#2226) fail when an edge is left
// unspecified.
func Classified(method Method) bool {
	_, ok := confidenceByMethod[method]
	return ok
}
