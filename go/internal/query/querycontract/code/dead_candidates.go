// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

import "strings"

// DeadCodeCandidateLabels names every graph label a dead-code candidate scan
// treats as a symbol worth checking.
//
// This is the ONLY declaration of the set. The scan in codequery/deadcode
// reads it directly, and the openAPI contract test in package query names this
// package directly and requires the advertised candidate_kind enum to equal the
// same set the scan reads. A second literal in root would compile and drift
// silently, changing either what the scan checks or what the contract
// advertises with nothing failing.
var DeadCodeCandidateLabels = []string{"Function", "Class", "Struct", "Interface", "Trait", "SqlFunction"}

// DeadCodeCandidateEntityType maps a candidate scan label to the content
// entity type it selects, reporting false for a label no candidate scan
// covers.
//
// It lives here beside DeadCodeCandidateLabels rather than in root because
// the code-family contract proofs in codequery name it directly, and a
// _test.go symbol in root is not importable across that package boundary
// (#6060). Root's reader calls it directly; #6597 deleted the old root wrapper.
func DeadCodeCandidateEntityType(label string) (string, bool) {
	switch label {
	case "Function", "Class", "Struct", "Interface", "Trait", "SqlFunction":
		return label, true
	default:
		return "", false
	}
}

// DeadCodeRootKindsFromMetadata reads the content-store dead_code_root_kinds
// classification off an entity's metadata map.
//
// This is the ONLY declaration of the projection. The exposure-path reader in
// query/impact names this package directly. It accepts both the
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
// promoted to testutil cannot name an unexported root type, so the type
// has to be reachable from outside package query before the double can move.
// codequery/deadcode re-exports it as an alias and codequery/aliases.go keeps
// an unexported one, so the handlers there name it without this import.
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
