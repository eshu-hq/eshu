// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

// DeadCodeCandidateLabels names every graph label a dead-code candidate scan
// treats as a symbol worth checking.
//
// This is the ONLY declaration of the set. Root package query's P0 shim
// (family_code_shim.go) aliases it so the code-family scan files that have not
// moved yet keep compiling unchanged; the openAPI contract test names this
// package directly so the advertised candidate_kind enum stays pinned to the
// same set the scan reads. A second literal in root would compile and drift
// silently, changing either what the scan checks or what the contract
// advertises with nothing failing.
var DeadCodeCandidateLabels = []string{"Function", "Class", "Struct", "Interface", "Trait", "SqlFunction"}

// DeadCodeRootKindsFromMetadata reads the content-store dead_code_root_kinds
// classification off an entity's metadata map.
//
// This is the ONLY declaration of the projection. Root package query's P0 shim
// (family_code_shim.go) forwards to it so the code-family dead-code root
// readers that have not moved yet keep compiling unchanged; the staying
// exposure-path reader names this package directly. It accepts both the
// []string and the []any a driver hands back for a list value, copies on the
// []string path so callers cannot mutate shared state through the result, and
// reports nil for a missing key, a nil map, or any other type.
func DeadCodeRootKindsFromMetadata(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["dead_code_root_kinds"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

// DeadCodeIncomingEdge is the strongest incoming reachability edge observed for
// a dead-code candidate. MaxConfidence is the maximum codeprovenance.Confidence
// across the candidate's incoming CALLS/REFERENCES/INHERITS/USES_METACLASS
// edges; Method names the resolution method behind that strongest edge.
//
// It lives here rather than in root because it appears in the signature of a
// ContentStore read that a shared test double must satisfy (#6060). A double
// promoted to querytestutil cannot name an unexported root type, so the type
// has to be reachable from outside package query before the double can move.
// Root keeps an unexported alias, so its callers are unchanged.
type DeadCodeIncomingEdge struct {
	// MaxConfidence is the highest codeprovenance.Confidence across the
	// candidate's incoming edges.
	MaxConfidence float64
	// Method names the resolution method behind the strongest edge.
	Method string
	// HiddenConsumer reports that at least one incoming edge came from a
	// repository outside the caller's grant. It is deliberately not a
	// confidence: an edge the caller may not see is not evidence, so it never
	// keeps a symbol alive and never makes one dead. It makes the answer
	// unknown only while the confidence beside it stays weak; a granted edge
	// above the weakest tier settles the candidate as reachable first.
	HiddenConsumer bool
}
