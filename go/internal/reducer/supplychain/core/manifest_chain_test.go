// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// chainView is a generation view with the given id and rows.
func chainView(id string, rows ...facts.Envelope) ManifestDependencyView {
	return ManifestDependencyView{GenerationID: id, Rows: rows}
}

// runChain runs one marker pass over generation and returns the port.
func runChain(t *testing.T, generation ManifestDependencyGeneration) (*stubManifestPort, map[string]float64) {
	t.Helper()
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	result, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return port, result.SubSignals
}

// TestManifestChainPinAddedInAnUnstampedGenerationFansOut is ruling test 1:
// W stamped, N activated but never completed, N+1 current; left-pad was
// added in N and is still in N+1, so it fans out.
func TestManifestChainPinAddedInAnUnstampedGenerationFansOut(t *testing.T) {
	t.Parallel()

	port, _ := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline:    chainView("gen-w", depRow("w-react", "package-lock.json", "react", "18.0.0")),
		Chain: []ManifestDependencyView{chainView("gen-n",
			depRow("n-react", "package-lock.json", "react", "18.0.0"),
			depRow("n-left-pad", "package-lock.json", "left-pad", "1.3.0"))},
		Current: chainView("gen-n1",
			depRow("c-react", "package-lock.json", "react", "18.0.0"),
			depRow("c-left-pad", "package-lock.json", "left-pad", "1.3.0")),
	})
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestChainPinAddedAndRemovedBetweenStampsFansOut is ruling test 2,
// the one that separates the chain diff from a prior-only diff: left-pad was
// added in N (never completed, so its holders never saw it) and removed in
// N+1. W and N+1 agree, yet a holder pass that ran while N was active may have
// written a left-pad finding, so left-pad must fan out.
func TestManifestChainPinAddedAndRemovedBetweenStampsFansOut(t *testing.T) {
	t.Parallel()

	port, _ := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline:    chainView("gen-w", depRow("w-react", "package-lock.json", "react", "18.0.0")),
		Chain: []ManifestDependencyView{chainView("gen-n",
			depRow("n-react", "package-lock.json", "react", "18.0.0"),
			depRow("n-left-pad", "package-lock.json", "left-pad", "1.3.0"))},
		Current: chainView("gen-n1", depRow("c-react", "package-lock.json", "react", "18.0.0")),
	})
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestChainRevertToBaseFansOut is ruling test 6: N, a delta, added
// left-pad on its touched path; N+1, a delta that did not touch that path,
// reverts to the base content. Comparing N+1 on its own touched paths would
// miss it; the full-view diff does not.
func TestManifestChainRevertToBaseFansOut(t *testing.T) {
	t.Parallel()

	base := []facts.Envelope{depRow("b-react", "web/package-lock.json", "react", "18.0.0")}
	port, _ := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline:    chainView("gen-w", base...),
		Chain: []ManifestDependencyView{{
			GenerationID: "gen-n",
			Rows: []facts.Envelope{
				deltaRepositoryFact([]string{"web/package-lock.json"}, nil),
				depRow("n-react", "web/package-lock.json", "react", "18.0.0"),
				depRow("n-left-pad", "web/package-lock.json", "left-pad", "1.3.0"),
			},
			Base: base,
		}},
		Current: ManifestDependencyView{
			GenerationID: "gen-n1",
			Rows:         []facts.Envelope{deltaRepositoryFact([]string{"README.md"}, nil)},
			Base:         base,
		},
	})
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestChainDeltaViewsAreBaseOverlaidOnBothSides is ruling test 7: a
// delta intermediate and a delta current that both left the lockfile alone
// read as their base, so nothing changed and nothing fans out.
func TestManifestChainDeltaViewsAreBaseOverlaidOnBothSides(t *testing.T) {
	t.Parallel()

	base := []facts.Envelope{depRow("b-lodash", "package-lock.json", "lodash", "4.17.21")}
	port, signals := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline:    chainView("gen-w", base...),
		Chain: []ManifestDependencyView{{
			GenerationID: "gen-n", Rows: []facts.Envelope{deltaRepositoryFact([]string{"README.md"}, nil)}, Base: base,
		}},
		Current: ManifestDependencyView{
			GenerationID: "gen-n1", Rows: []facts.Envelope{deltaRepositoryFact([]string{"main.go"}, nil)}, Base: base,
		},
	})
	if len(port.reopened) != 0 || signals["manifest_trigger_changed_packages"] != 0 {
		t.Fatalf("reopened = %v, changed = %v; want nothing for base-identical deltas", port.reopened, signals["manifest_trigger_changed_packages"])
	}
	if want := []string{"gen-w"}; !reflect.DeepEqual(port.stamps, want) {
		t.Fatalf("stamps = %v, want the no-change exit stamped with baseline %v", port.stamps, want)
	}
}

// TestManifestChainDegradedFansOutEveryCurrentPin is ruling test 8: over the
// depth cap or with an unreadable baseline the diff degrades to "no prior",
// so every current pin fans out and the pass says so.
func TestManifestChainDegradedFansOutEveryCurrentPin(t *testing.T) {
	t.Parallel()

	port, signals := runChain(t, ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Degraded:   true,
		Current: chainView("gen-n1",
			depRow("c-lodash", "package-lock.json", "lodash", "4.17.21"),
			depRow("c-left-pad", "package-lock.json", "left-pad", "1.3.0")),
	})
	if want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
	if signals["manifest_trigger_baseline_degraded"] != 1 {
		t.Fatalf("manifest_trigger_baseline_degraded = %v, want 1", signals["manifest_trigger_baseline_degraded"])
	}
}

// TestManifestChainPrunedIntermediateIsReported proves a skipped pruned
// intermediate is visible on the pass.
func TestManifestChainPrunedIntermediateIsReported(t *testing.T) {
	t.Parallel()

	_, signals := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline:    chainView("gen-w", depRow("w-react", "package-lock.json", "react", "18.0.0")),
		ChainPruned: true,
		Current:     chainView("gen-n1", depRow("c-react", "package-lock.json", "react", "18.0.0")),
	})
	if signals["manifest_trigger_chain_pruned"] != 1 {
		t.Fatalf("manifest_trigger_chain_pruned = %v, want 1", signals["manifest_trigger_chain_pruned"])
	}
}

// TestManifestChainStampOnlyWhenNoHolderIsLeft is ruling test 4: a first
// chunk that leaves holders on the continuation list writes no completion
// stamp; the claim that drains the list writes it, once.
func TestManifestChainStampOnlyWhenNoHolderIsLeft(t *testing.T) {
	t.Parallel()

	var candidates []PackageHolderCandidate
	for i := range fanOutHolderCap + 3 {
		candidates = append(candidates, npmHolder(fmt.Sprintf("vuln-intel://osv/h%03d", i), "lodash"))
	}
	generation := activeFullChange()
	generation.Baseline.GenerationID = "gen-w"
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: candidates, payload: map[string]any{}},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, port.payload)); err != nil {
		t.Fatalf("first claim error = %v", err)
	}
	if len(port.stamps) != 0 {
		t.Fatalf("first chunk with holders left stamped %v; want no stamp", port.stamps)
	}
	if _, err := h.Handle(context.Background(), manifestIntent(1, port.payload)); err != nil {
		t.Fatalf("continuation claim error = %v", err)
	}
	if len(port.stamps) != 1 {
		t.Fatalf("stamps after the drain = %v, want exactly one", port.stamps)
	}
}

// TestManifestChainSupersededAndDeferredPassesWriteNoStamp is ruling test 5.
func TestManifestChainSupersededAndDeferredPassesWriteNoStamp(t *testing.T) {
	t.Parallel()

	for _, activation := range []ManifestGenerationActivation{ManifestGenerationSuperseded, ManifestGenerationPending} {
		generation := activeFullChange()
		generation.Activation = activation
		port := &stubManifestPort{
			stubFanOut:  &stubFanOut{candidates: standardCandidates()},
			generations: []ManifestDependencyGeneration{generation},
		}
		h, _ := manifestHandler(t, port, nil)
		_, _ = h.Handle(context.Background(), manifestIntent(1, nil))
		if len(port.stamps) != 0 {
			t.Fatalf("%s pass stamped %v; want no stamp", activation, port.stamps)
		}
	}
}

// TestManifestChainRedeliveryAfterAStampRederives is ruling test 9: an item
// already stamped and redelivered re-derives, reopens the same holders, and
// re-stamps with the same baseline.
func TestManifestChainRedeliveryAfterAStampRederives(t *testing.T) {
	t.Parallel()

	generation := activeFullChange()
	generation.Baseline.GenerationID = "gen-w"
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates(), payload: map[string]any{}},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	for range 2 {
		if _, err := h.Handle(context.Background(), manifestIntent(1, port.payload)); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}
	if port.reads != 2 {
		t.Fatalf("generation reads = %d, want 2 (a stamped redelivery re-derives)", port.reads)
	}
	if want := []string{"gen-w", "gen-w"}; !reflect.DeepEqual(port.stamps, want) {
		t.Fatalf("stamps = %v, want %v", port.stamps, want)
	}
	if want := 6; len(port.reopened) != want {
		t.Fatalf("reopens = %d, want %d (three holders twice)", len(port.reopened), want)
	}
}

// TestManifestChainZeroRowFullFansOutThePriorsPackages is the handler half of
// ruling test 15: a full generation with no dependency row (every manifest
// deleted) after a baseline that had rows reopens the holders of the
// baseline's packages, so their findings retract.
func TestManifestChainZeroRowFullFansOutThePriorsPackages(t *testing.T) {
	t.Parallel()

	port, _ := runChain(t, ManifestDependencyGeneration{
		Activation:  ManifestGenerationActive,
		HasBaseline: true,
		Baseline: chainView("gen-w",
			depRow("w-lodash", "package-lock.json", "lodash", "4.17.21"),
			depRow("w-left-pad", "package-lock.json", "left-pad", "1.3.0")),
		Current: chainView("gen-n1", facts.Envelope{FactID: "repo-n1", FactKind: "repository"}),
	})
	if want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}
