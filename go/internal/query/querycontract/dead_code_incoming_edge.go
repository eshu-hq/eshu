// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

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
	// keeps a symbol alive and never makes one dead -- it makes the answer
	// unknown.
	HiddenConsumer bool
}
