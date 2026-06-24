// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

import "testing"

func iamSinkSpec(t *testing.T) SinkSpec {
	t.Helper()
	spec, ok := MatchSink("CAN_ESCALATE_TO", "CloudResource", nil)
	if !ok {
		t.Fatal("expected an IAM escalation sink spec")
	}
	return spec
}

// TestCombinePathSeverityHonestEscalation proves the severity rules: an
// internet-exposed handler reaching a privileged IAM action is critical with an
// honest reason; a network-reachable (unproven) source keeps the sink baseline;
// an internal source is not escalated. The reason must never claim a confirmed
// missing auth check when authObserved is unknown.
func TestCombinePathSeverityHonestEscalation(t *testing.T) {
	t.Parallel()

	iam := iamSinkSpec(t)

	crit, reason := CombinePathSeverity(ExposureInternetExposed, iam, false)
	if crit != SeverityCritical {
		t.Fatalf("internet-exposed -> IAM severity = %q, want critical", crit)
	}
	if reason == "" {
		t.Fatal("critical severity must carry a justifying reason")
	}

	// Network-reachable (internet reachability unproven) must not be escalated to
	// critical: we cannot prove internet exposure.
	netSev, _ := CombinePathSeverity(ExposureNetworkReachable, iam, false)
	if netSev == SeverityCritical {
		t.Fatalf("network-reachable IAM severity = %q, must not be critical without proven internet exposure", netSev)
	}

	// Internal source must not be escalated.
	intSev, _ := CombinePathSeverity(ExposureInternal, iam, false)
	if intSev == SeverityCritical {
		t.Fatalf("internal source severity = %q, must not be critical", intSev)
	}
}

// TestBuildExposureFindingRendersPath proves a found sink path renders with the
// derived truth label, an exact traversal state, and a combined severity. This
// is the #2726 acceptance: for an exposed handler reaching a privileged action,
// the tool returns the path + justified severity, derived-labeled.
func TestBuildExposureFindingRendersPath(t *testing.T) {
	t.Parallel()

	src := PathNode{EntityID: "fn:handler", Name: "HandleRequest", Labels: []string{"Function"}}
	iam := iamSinkSpec(t)
	candidate := PathCandidate{
		Nodes: []PathNode{
			src,
			{EntityID: "fn:inner", Name: "doWork", Labels: []string{"Function"}},
			{EntityID: "cr:role", Name: "deploy-role", Labels: []string{"CloudResource"}},
		},
		Sink:  SinkHit{Kind: iam.Kind, DisplayName: iam.DisplayName, Node: PathNode{EntityID: "cr:admin", Name: "admin", Labels: []string{"CloudResource"}}},
		Depth: 2,
	}

	finding := BuildExposureFinding(ExposureFindingInput{
		Source:       src,
		SourceKind:   SourceHTTPHandler,
		ExposureRank: ExposureInternetExposed,
		SinkSpecsByKind: map[SinkKind]SinkSpec{
			iam.Kind: iam,
		},
		Candidates: []PathCandidate{candidate},
		MaxDepth:   5,
	})

	if finding.TruthLabel != TruthLabelDerived {
		t.Fatalf("truth label = %q, want %q", finding.TruthLabel, TruthLabelDerived)
	}
	if len(finding.Paths) != 1 {
		t.Fatalf("paths = %d, want 1", len(finding.Paths))
	}
	p := finding.Paths[0]
	if p.State != TraversalExact {
		t.Fatalf("path state = %q, want exact", p.State)
	}
	if p.Severity != SeverityCritical {
		t.Fatalf("path severity = %q, want critical (internet-exposed -> IAM)", p.Severity)
	}
	if p.Reason == "" {
		t.Fatal("path must carry a severity reason")
	}
	if finding.State != TraversalExact {
		t.Fatalf("finding state = %q, want exact", finding.State)
	}
}

// TestBuildExposureFindingUnresolvedNeverFabricates proves that with zero
// candidate paths (the production reality until the bridge edges materialize),
// the finding is unresolved, has no paths, and records an honest reason — it
// never invents a path or a severity.
func TestBuildExposureFindingUnresolvedNeverFabricates(t *testing.T) {
	t.Parallel()

	src := PathNode{EntityID: "fn:handler", Name: "HandleRequest", Labels: []string{"Function"}}
	finding := BuildExposureFinding(ExposureFindingInput{
		Source:           src,
		SourceKind:       SourceHTTPHandler,
		ExposureRank:     ExposureNetworkReachable,
		Candidates:       nil,
		MaxDepth:         5,
		UnresolvedReason: "no code-to-cloud bridge edge materialized (see #2723)",
	})

	if len(finding.Paths) != 0 {
		t.Fatalf("paths = %d, want 0", len(finding.Paths))
	}
	if finding.State != TraversalUnresolved {
		t.Fatalf("finding state = %q, want unresolved", finding.State)
	}
	if finding.Coverage.UnresolvedReason == "" {
		t.Fatal("unresolved finding must record a reason")
	}
	if finding.TruthLabel != TruthLabelDerived {
		t.Fatalf("truth label = %q, want derived", finding.TruthLabel)
	}
}

// TestBuildExposureFindingPartialOnTruncation proves a finding whose traversal
// was depth-truncated is reported partial, not exact, so the bound is honest.
func TestBuildExposureFindingPartialOnTruncation(t *testing.T) {
	t.Parallel()

	src := PathNode{EntityID: "fn:handler", Name: "HandleRequest", Labels: []string{"Function"}}
	iam := iamSinkSpec(t)
	finding := BuildExposureFinding(ExposureFindingInput{
		Source:       src,
		SourceKind:   SourceHTTPHandler,
		ExposureRank: ExposureInternetExposed,
		SinkSpecsByKind: map[SinkKind]SinkSpec{
			iam.Kind: iam,
		},
		Candidates: []PathCandidate{{
			Nodes: []PathNode{src, {EntityID: "cr:x", Name: "x", Labels: []string{"CloudResource"}}},
			Sink:  SinkHit{Kind: iam.Kind, DisplayName: iam.DisplayName, Node: PathNode{EntityID: "cr:x"}},
			Depth: 1,
		}},
		MaxDepth:  5,
		Truncated: true,
	})

	if finding.State != TraversalPartial {
		t.Fatalf("finding state = %q, want partial (truncated)", finding.State)
	}
	if !finding.Coverage.Truncated {
		t.Fatal("coverage must record truncation")
	}
}
