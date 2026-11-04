// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const manifestTriggerScope = "git-repo-checkout"

// stubManifestPort is the fan-out port plus the manifest-trigger reader, the
// way the Postgres store implements both.
type stubManifestPort struct {
	*stubFanOut
	// generations is served in order, one per read; the last one repeats.
	generations []ManifestDependencyGeneration
	readErr     error
	reads       int
}

func (p *stubManifestPort) ReadManifestDependencyGeneration(_ context.Context, scopeID, generationID string) (ManifestDependencyGeneration, error) {
	p.reads++
	if p.readErr != nil {
		return ManifestDependencyGeneration{}, p.readErr
	}
	index := min(p.reads-1, len(p.generations)-1)
	return p.generations[index], nil
}

// panicLoader fails the test if the marker branch loads any evidence.
type panicLoader struct{ t *testing.T }

func (l panicLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	l.t.Fatal("marker intent loaded evidence through the fact loader")
	return nil, nil
}

// failWriter fails the test if the marker branch writes findings.
type failWriter struct{ t *testing.T }

func (w failWriter) WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	w.t.Fatal("marker intent wrote findings")
	return SupplyChainImpactWriteResult{}, nil
}

// countingIssuer counts fencing tokens so a test can prove the marker pass
// burns none.
type countingIssuer struct{ issued int }

func (c *countingIssuer) NextSupplyChainImpactFencingToken(context.Context) (int64, error) {
	c.issued++
	return int64(c.issued), nil
}

func manifestHandler(t *testing.T, port SupplyChainImpactFanOut, inst *telemetry.Instruments) (SupplyChainImpactHandler, *countingIssuer) {
	t.Helper()
	issuer := &countingIssuer{}
	return SupplyChainImpactHandler{
		FencingTokenIssuer: issuer,
		FactLoader:         panicLoader{t: t},
		Writer:             failWriter{t: t},
		FanOut:             port,
		Instruments:        inst,
	}, issuer
}

func manifestIntent(attempt int, payload map[string]any) reducercontract.Intent {
	merged := map[string]any{PayloadKeyTrigger: TriggerManifestDependency}
	for key, value := range payload {
		merged[key] = value
	}
	return reducercontract.Intent{
		IntentID:     "intent-manifest",
		ScopeID:      manifestTriggerScope,
		GenerationID: "gen-new",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "manifest dependency evidence observed",
		AttemptCount: attempt,
		Payload:      merged,
	}
}

// depRow is one git dependency row on path pinning name at version.
func depRow(factID, path, name, version string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      "content_entity",
		StableFactKey: "entity:" + path + ":" + name,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"entity_type":   "Variable",
			"entity_name":   name,
			"repo_id":       "repository:r_checkout",
			"relative_path": path,
			"entity_metadata": map[string]any{
				"config_kind": "dependency", "package_manager": "npm", "value": version, "lockfile": true,
			},
		},
	}
}

// deltaRepositoryFact marks a generation as a delta that changed and deleted
// the given repository-relative paths.
func deltaRepositoryFact(changed, deleted []string) facts.Envelope {
	toAny := func(paths []string) []any {
		out := make([]any, 0, len(paths))
		for _, p := range paths {
			out = append(out, p)
		}
		return out
	}
	return facts.Envelope{
		FactID:   "repo-delta",
		FactKind: "repository",
		Payload: map[string]any{
			"delta_generation":             true,
			"path":                         "/repos/checkout",
			"delta_relative_paths":         toAny(changed),
			"delta_deleted_relative_paths": toAny(deleted),
		},
	}
}

func npmHolder(scope, name string) PackageHolderCandidate {
	return PackageHolderCandidate{
		ScopeID: scope, Ecosystem: "npm", PackageName: name,
		PURL: "pkg:npm/" + name, PackageID: "npm://registry.npmjs.org/" + name,
	}
}

// standardCandidates holds advisory evidence for lodash in two OSV scopes,
// for left-pad in one, and for an unrelated package in a fourth.
func standardCandidates() []PackageHolderCandidate {
	return []PackageHolderCandidate{
		npmHolder("vuln-intel://osv/a", "lodash"),
		npmHolder("vuln-intel://osv/b", "lodash"),
		npmHolder("vuln-intel://osv/c", "left-pad"),
		npmHolder("vuln-intel://osv/d", "unrelated"),
		npmHolder(manifestTriggerScope, "lodash"),
	}
}

func activeFullChange() ManifestDependencyGeneration {
	return ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current: ManifestDependencyView{Rows: []facts.Envelope{
			depRow("new-lodash", "package-lock.json", "lodash", "4.17.21"),
			depRow("new-react", "package-lock.json", "react", "18.0.0"),
		}},
		HasPrior: true,
		Prior: ManifestDependencyView{Rows: []facts.Envelope{
			depRow("old-lodash", "package-lock.json", "lodash", "4.17.20"),
			depRow("old-react", "package-lock.json", "react", "18.0.0"),
			depRow("old-left-pad", "package-lock.json", "left-pad", "1.3.0"),
		}},
	}
}

// TestManifestTriggerReopensHoldersOfChangedPackagesOnly is the #9874 reducer
// regression: a marker intent for a full generation that bumped lodash and
// removed left-pad reopens exactly the scopes holding advisory evidence for
// those two packages, never its own scope, and never loads evidence, issues
// a fencing token, or writes findings.
func TestManifestTriggerReopensHoldersOfChangedPackagesOnly(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, issuer := manifestHandler(t, port, nil)

	result, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"}
	if !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v (react is unchanged, unrelated is not named)", port.reopened, want)
	}
	if got := port.candidateEcosystems; !reflect.DeepEqual(got, [][]string{{"npm"}}) {
		t.Fatalf("holder read ecosystems = %v, want one read for [npm]", got)
	}
	if issuer.issued != 0 {
		t.Fatalf("fencing tokens issued = %d, want 0 for a marker pass", issuer.issued)
	}
	if got := port.origins[0]; got.SourceScopeID != manifestTriggerScope || got.SourceGenerationID != "gen-new" {
		t.Fatalf("origin = %+v, want the git scope generation", got)
	}
	if got := result.SubSignals["manifest_trigger_changed_packages"]; got != 2 {
		t.Fatalf("manifest_trigger_changed_packages = %v, want 2", got)
	}
}

// TestManifestTriggerFirstIndexReopensHoldersOfEveryPin covers a generation
// with no prior: every pin is new.
func TestManifestTriggerFirstIndexReopensHoldersOfEveryPin(t *testing.T) {
	t.Parallel()

	generation := activeFullChange()
	generation.HasPrior = false
	generation.Prior = ManifestDependencyView{}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestTriggerDeltaComparesTouchedPathsAgainstThePriorView proves a
// delta generation is judged on its touched paths only: an edited lockfile
// that dropped left-pad reopens left-pad's holder (a removal with no row in
// the delta), and an untouched manifest's pins are not reported as removed.
func TestManifestTriggerDeltaComparesTouchedPathsAgainstThePriorView(t *testing.T) {
	t.Parallel()

	generation := ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current: ManifestDependencyView{Rows: []facts.Envelope{
			deltaRepositoryFact([]string{"web/package-lock.json"}, nil),
			depRow("d-react", "web/package-lock.json", "react", "18.0.0"),
		}},
		HasPrior: true,
		Prior: ManifestDependencyView{Rows: []facts.Envelope{
			depRow("p-react", "web/package-lock.json", "react", "18.0.0"),
			depRow("p-left-pad", "web/package-lock.json", "left-pad", "1.3.0"),
			depRow("p-lodash", "api/package-lock.json", "lodash", "4.17.20"),
		}},
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v (lodash's file was not touched)", port.reopened, want)
	}
}

// TestManifestTriggerTombstonedRowCountsAsAChange proves a tombstoned
// dependency row in the generation resolves to a changed package: the delta
// removed left-pad by tombstone, and its holder is reopened even though the
// prior view was pruned to nothing for that path.
func TestManifestTriggerTombstonedRowCountsAsAChange(t *testing.T) {
	t.Parallel()

	removed := depRow("d-left-pad", "web/package-lock.json", "left-pad", "1.3.0")
	removed.IsTombstone = true
	generation := ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current: ManifestDependencyView{Rows: []facts.Envelope{
			deltaRepositoryFact([]string{"web/package-lock.json"}, nil),
			removed,
		}},
		HasPrior: false,
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestTriggerPriorDeltaIsOverlaidOnItsBase proves a prior delta
// generation is read as its base overlaid with its own rows, so a pin only
// the base held is still seen as removed.
func TestManifestTriggerPriorDeltaIsOverlaidOnItsBase(t *testing.T) {
	t.Parallel()

	generation := ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current: ManifestDependencyView{Rows: []facts.Envelope{
			depRow("n-react", "package-lock.json", "react", "18.0.0"),
		}},
		HasPrior: true,
		Prior: ManifestDependencyView{
			Rows: []facts.Envelope{
				deltaRepositoryFact([]string{"README.md"}, nil),
			},
			Base: []facts.Envelope{
				depRow("b-react", "package-lock.json", "react", "18.0.0"),
				depRow("b-left-pad", "package-lock.json", "left-pad", "1.3.0"),
			},
		},
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestTriggerKnownLimitPriorDeltaWithMissingBase pins the known
// correctness gap the lane lead asked to keep visible (#9874 condition b):
// when the prior generation is a delta whose base full generation is missing
// or pruned, the prior view holds only that delta's own rows, so a pin that
// only the base held looks never-present and its removal is NOT fanned out.
// The finding for left-pad then stays until left-pad's advisory scope reruns.
// The pass reports the gap on manifest_trigger_prior_base_missing. If this
// test starts failing because left-pad's holder IS reopened, the gap was
// closed: update the evidence note and this test together.
func TestManifestTriggerKnownLimitPriorDeltaWithMissingBase(t *testing.T) {
	t.Parallel()

	generation := ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current: ManifestDependencyView{Rows: []facts.Envelope{
			depRow("n-react", "package-lock.json", "react", "18.0.0"),
		}},
		HasPrior: true,
		Prior: ManifestDependencyView{
			Rows:        []facts.Envelope{deltaRepositoryFact([]string{"README.md"}, nil)},
			BaseMissing: true,
		},
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	result, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	for _, scope := range port.reopened {
		if scope == "vuln-intel://osv/c" {
			t.Fatalf("left-pad's holder was reopened; the known gap is closed, update the evidence note")
		}
	}
	if got := result.SubSignals["manifest_trigger_prior_base_missing"]; got != 1 {
		t.Fatalf("manifest_trigger_prior_base_missing = %v, want 1", got)
	}
}

// TestManifestTriggerDefersUntilActivationThenFansOut is the deferral-then-
// success case: a pass that runs before its generation activates fails with a
// retryable, counting failure class (so the attempt budget bounds it) and
// reopens nothing; the retry after activation fans out.
func TestManifestTriggerDefersUntilActivationThenFansOut(t *testing.T) {
	t.Parallel()

	pending := ManifestDependencyGeneration{Activation: ManifestGenerationPending}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{pending, activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)

	_, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err == nil {
		t.Fatal("Handle() error = nil, want a deferral before activation")
	}
	var retryable interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("deferral error %v is not retryable", err)
	}
	if got := retryable.FailureClass(); got != SupplyChainImpactManifestTriggerDeferredFailureClass {
		t.Fatalf("FailureClass() = %q, want %q", got, SupplyChainImpactManifestTriggerDeferredFailureClass)
	}
	if len(port.reopened) != 0 || len(port.candidateEcosystems) != 0 {
		t.Fatalf("deferred pass touched holders: reopened=%v reads=%v", port.reopened, port.candidateEcosystems)
	}

	if _, err := h.Handle(context.Background(), manifestIntent(2, nil)); err != nil {
		t.Fatalf("Handle() after activation error = %v", err)
	}
	if want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestTriggerSupersededGenerationIsANoOp proves a marker pass whose
// generation a newer one already replaced succeeds without reading holders:
// the newer generation's own pass owns the change.
func TestManifestTriggerSupersededGenerationIsANoOp(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{{Activation: ManifestGenerationSuperseded}},
	}
	h, _ := manifestHandler(t, port, nil)
	result, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded || len(port.reopened) != 0 || len(port.candidateEcosystems) != 0 {
		t.Fatalf("superseded pass: status=%q reopened=%v reads=%v", result.Status, port.reopened, port.candidateEcosystems)
	}
}
