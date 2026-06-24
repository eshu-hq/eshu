// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

// TraversalState is the conservative per-path / per-finding truth-state
// vocabulary for an exposure trace. It mirrors the outcome vocabulary used across
// the query surface so the values are wire-stable.
type TraversalState string

const (
	// TraversalExact is a fully resolved, unambiguous reachable path to a
	// recognized sink. (The finding's truth LABEL is still derived — exactness
	// here is structural reachability, not value-flow.)
	TraversalExact TraversalState = "exact"
	// TraversalPartial is a path/finding that resolved some, but not all, of the
	// traversal — e.g. it was depth-truncated.
	TraversalPartial TraversalState = "partial"
	// TraversalAmbiguous is a source or sink that resolved to more than one
	// candidate; the trace does not pick one silently.
	TraversalAmbiguous TraversalState = "ambiguous"
	// TraversalUnresolved is a source with no reachable sink under the current
	// materialized graph (e.g. the code-to-cloud bridge edge does not exist yet).
	TraversalUnresolved TraversalState = "unresolved"
)

// TruthLabelDerived is the truth label every exposure finding carries. Level 1
// reachability is symbol-level, never value-flow, so a finding is derived, never
// exact (#2704 non-goals).
const TruthLabelDerived = "derived"

// PathNode is one node on a traced exposure path.
type PathNode struct {
	// EntityID is the node's stable graph identity (id or uid).
	EntityID string
	// Name is the node's display name.
	Name string
	// Labels are the node's graph labels.
	Labels []string
}

// SinkHit is a recognized sink terminating a path.
type SinkHit struct {
	// Kind is the recognized sink kind.
	Kind SinkKind
	// DisplayName is the human-facing sink label.
	DisplayName string
	// Node is the sink terminal node.
	Node PathNode
}

// PathCandidate is one structural reachability candidate the graph traversal
// found: the ordered nodes from source to the sink terminal, the recognized
// sink, and the traversal depth.
type PathCandidate struct {
	// Nodes are the ordered nodes from the source handler to the sink terminal.
	Nodes []PathNode
	// Sink is the recognized sink terminating the path.
	Sink SinkHit
	// Depth is the number of edges traversed.
	Depth int
}

// ExposurePath is one assembled finding path: the candidate plus its computed
// severity, traversal state, and justifying reason.
type ExposurePath struct {
	// Nodes are the ordered path nodes from source to sink terminal.
	Nodes []PathNode
	// Sink is the recognized sink.
	Sink SinkHit
	// Depth is the traversal depth.
	Depth int
	// State is this path's traversal truth-state.
	State TraversalState
	// Severity is the combined severity for this path.
	Severity Severity
	// Reason justifies the severity honestly.
	Reason string
}

// Coverage records the honest bounds of a trace so an operator can see what was
// and was not covered.
type Coverage struct {
	// MaxDepth is the traversal bound applied.
	MaxDepth int
	// PathsFound is the number of reachable sink paths assembled.
	PathsFound int
	// Truncated is true when the bound cut the traversal short.
	Truncated bool
	// UnresolvedReason explains why no (or only partial) paths were found, e.g. a
	// missing bridge edge. Empty when paths were fully resolved.
	UnresolvedReason string
}

// ExposureFinding is the full result for one source handler.
type ExposureFinding struct {
	// Source is the source handler node.
	Source PathNode
	// SourceKind is the classified taint-source kind.
	SourceKind SourceKind
	// ExposureRank is the honest exposure ranking of the source.
	ExposureRank ExposureRank
	// TruthLabel is always TruthLabelDerived for Level 1.
	TruthLabel string
	// State is the overall traversal truth-state for the finding.
	State TraversalState
	// Paths are the assembled reachable sink paths (empty when unresolved).
	Paths []ExposurePath
	// Coverage records the honest traversal bounds.
	Coverage Coverage
}

// ExposureFindingInput is the data the assembler turns into a finding. It is
// plain data so the assembler stays pure and unit-testable without a graph: the
// query handler runs the bounded graph traversal and maps rows into these
// candidates.
type ExposureFindingInput struct {
	// Source is the resolved source handler node.
	Source PathNode
	// SourceKind is the classified taint-source kind.
	SourceKind SourceKind
	// ExposureRank is the honest exposure ranking of the source.
	ExposureRank ExposureRank
	// SinkSpecsByKind maps each recognized sink kind to its spec (for baseline
	// severity). The assembler reads severity from here, never invents it.
	SinkSpecsByKind map[SinkKind]SinkSpec
	// Candidates are the structural reachability candidates the traversal found.
	Candidates []PathCandidate
	// MaxDepth is the traversal bound that was applied.
	MaxDepth int
	// Truncated is true when the bound cut the traversal short.
	Truncated bool
	// UnresolvedReason is set by the handler when the traversal could not reach a
	// sink for a known reason (e.g. an unmaterialized bridge edge).
	UnresolvedReason string
}

// severityRank orders the closed severity vocabulary for escalation math; higher
// is more severe.
func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// severityByRank is the inverse of severityRank, clamped to the vocabulary.
func severityByRank(rank int) Severity {
	switch {
	case rank >= 4:
		return SeverityCritical
	case rank == 3:
		return SeverityHigh
	case rank == 2:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// CombinePathSeverity computes a single path's severity from the source exposure
// rank and the sink, honestly. An internet-exposed handler reaching a privileged
// IAM action is critical. A network-reachable (internet reachability unproven)
// source is never escalated to critical — exposure is not proven. An internal
// source keeps the sink baseline. authObserved reports whether an authentication
// gate was observed on the path; Level 1 does not model auth, so callers pass
// false and the reason states the limitation rather than asserting a confirmed
// missing-auth finding.
func CombinePathSeverity(rank ExposureRank, sink SinkSpec, authObserved bool) (Severity, string) {
	base := sink.BaselineSeverity
	switch rank {
	case ExposureInternetExposed:
		// An internet-exposed handler reaching a privileged IAM action is the
		// signature critical finding the capability exists to surface.
		if sink.Kind == SinkIAMPrivilegedAction && !authObserved {
			return SeverityCritical, "internet-exposed handler transitively reaches a privileged IAM action; no authentication gate is modeled in Level 1, so this is a derived upper-bound severity, not a confirmed missing-auth finding"
		}
		escalated := severityByRank(severityRank(base) + 1)
		return escalated, "internet-exposed handler reaches a " + sink.DisplayName + " sink"
	case ExposureNetworkReachable:
		// Internet reachability is unproven, so the exposure context caps severity
		// below critical even when the sink baseline is critical: we will not claim
		// a critical exposure we cannot prove is internet-facing.
		capped := capSeverity(base, SeverityHigh)
		return capped, "network-reachable handler reaches a " + sink.DisplayName + " sink; internet reachability is unproven, so severity is capped at high"
	default:
		// A non-network (internal) source caps lower still; the sink may be severe
		// in isolation, but it is not an external exposure path.
		capped := capSeverity(base, SeverityMedium)
		return capped, "internal source reaches a " + sink.DisplayName + " sink; not an external exposure path, so severity is capped at medium"
	}
}

// capSeverity returns the lower of two severities, so an exposure rank can cap a
// sink's intrinsic severity when external reachability is weaker or unproven.
func capSeverity(a, capAt Severity) Severity {
	if severityRank(a) <= severityRank(capAt) {
		return a
	}
	return capAt
}

// BuildExposureFinding assembles an ExposureFinding from plain traversal data. It
// never fabricates a path or severity: with no candidates it returns an
// unresolved finding carrying the honest reason; with candidates it computes each
// path's severity from the supplied sink specs. The finding is always labeled
// derived. The overall state is unresolved (no paths), partial (paths but
// truncated, or any path partial), or exact (fully resolved paths).
func BuildExposureFinding(in ExposureFindingInput) ExposureFinding {
	finding := ExposureFinding{
		Source:       in.Source,
		SourceKind:   in.SourceKind,
		ExposureRank: in.ExposureRank,
		TruthLabel:   TruthLabelDerived,
		Coverage: Coverage{
			MaxDepth:         in.MaxDepth,
			Truncated:        in.Truncated,
			UnresolvedReason: in.UnresolvedReason,
		},
	}

	for _, candidate := range in.Candidates {
		spec, ok := in.SinkSpecsByKind[candidate.Sink.Kind]
		if !ok {
			// A candidate whose sink kind we cannot resolve to a spec is not
			// rendered: we will not assign a fabricated severity.
			continue
		}
		severity, reason := CombinePathSeverity(in.ExposureRank, spec, false)
		state := TraversalExact
		if in.Truncated {
			state = TraversalPartial
		}
		finding.Paths = append(finding.Paths, ExposurePath{
			Nodes:    candidate.Nodes,
			Sink:     candidate.Sink,
			Depth:    candidate.Depth,
			State:    state,
			Severity: severity,
			Reason:   reason,
		})
	}

	finding.Coverage.PathsFound = len(finding.Paths)
	switch {
	case len(finding.Paths) == 0:
		finding.State = TraversalUnresolved
		if finding.Coverage.UnresolvedReason == "" {
			finding.Coverage.UnresolvedReason = "no reachable cloud sink found within the traversal bound"
		}
	case in.Truncated:
		finding.State = TraversalPartial
	default:
		finding.State = TraversalExact
	}
	return finding
}
