// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Image-scope arrival proofs for the #11087 own-scope restriction (arbiter
// ruling on the 2026-11-21 review P1). An image or OCI pass reaches its SBOM
// components only through the attestation attachment, so every component is
// foreign by scope; without the own-digest rule an image arriving last
// emitted none of the findings its own digest anchors. The ownership rule
// under test: a component bears component-origin criteria when its scope is
// the pass's own, or its document is attached to a subject digest the pass's
// own scope holds (suppression rows never root a document).
//
// Image identity and CI-run correlation arrival re-run every impact consumer
// (go/internal/reducer/crossscope/dependencies.go: DomainSupplyChainImpact
// lists DomainContainerImageIdentity and DomainCICDRunCorrelation as producer
// domains; go/internal/storage/postgres/scope/completion/fanout.go fans the
// completion out to every active consumer scope). Attachment arrival has no
// such edge, so fixtures a, b, c, e and h prove the findings come from the
// image scope's OWN run, and fixture d models the fan-out explicitly.

package core

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

const sdScopeRepo = "scope:repo-m"

// clone copies the universe's slices so a variant never mutates a shared one.
func (u sdUniverse) clone() sdUniverse {
	out := sdUniverse{}
	for scope, envelopes := range u {
		out[scope] = append([]facts.Envelope(nil), envelopes...)
	}
	return out
}

// moveAttachment moves the attestation attachment between source scopes.
func (u sdUniverse) moveAttachment(from, to string) sdUniverse {
	out := u.clone()
	var keep []facts.Envelope
	for _, env := range out[from] {
		if env.FactKind == reducercontract.SBOMAttestationAttachmentFactKind {
			env.ScopeID = to
			env.GenerationID = "generation:" + to
			out[to] = append(out[to], env)
			continue
		}
		keep = append(keep, env)
	}
	out[from] = keep
	return out
}

// withoutAttachment drops the attachment entirely (the pre-arrival universe).
func (u sdUniverse) withoutAttachment() sdUniverse {
	out := u.clone()
	for scope, envelopes := range out {
		var keep []facts.Envelope
		for _, env := range envelopes {
			if env.FactKind != reducercontract.SBOMAttestationAttachmentFactKind {
				keep = append(keep, env)
			}
		}
		out[scope] = keep
	}
	return out
}

// withBatchSuppression adds a suppression row to batch B that names the
// image digest in its scope map (and an unrelated CVE, so it changes no
// finding).
func (u sdUniverse) withBatchSuppression() sdUniverse {
	out := u.clone()
	out[sdScopeB] = append(out[sdScopeB], sdScoped(sdScopeB, vulnerabilitySuppressionFactEnvelope(
		"supp-b", "vex", "not exploitable", "owner", "2026-05-01T00:00:00Z", "",
		map[string]any{"cve_id": "CVE-2099-0001", "subject_digest": sdDigestM}))...)
	return out
}

// withRepoScope adds a repository scope whose evidence names the image's
// repository, so its pass reaches the digest through the image identity row.
func (u sdUniverse) withRepoScope() sdUniverse {
	out := u.clone()
	out[sdScopeRepo] = sdScoped(sdScopeRepo, packageConsumptionFactWithRange("cons-m", "pkg:npm/m-app", sdRepoM, "1.0.0"))
	return out
}

// sdStep is one arrival: the universe at that moment, the scopes that arrive,
// and the scopes whose own run executes.
type sdStep struct {
	u      sdUniverse
	arrive []string
	run    []string
}

// sdSequence plays the steps and returns the latest findings per source run
// plus the oracle of the final step. Every run is checked against the oracle
// of its own step (content-equal subset), so a run never invents or alters a
// finding.
func sdSequence(t *testing.T, steps []sdStep, bound bool) (map[string]map[string]sdFinding, map[string]sdFinding) {
	t.Helper()
	var present []string
	latest := map[string]map[string]sdFinding{}
	var oracle map[string]sdFinding
	for _, step := range steps {
		present = append(present, step.arrive...)
		oracle = sdOracle(t, step.u, present...)
		current := map[string]map[string]sdFinding{}
		for _, scope := range step.run {
			latest[scope] = sdRun(t, step.u, scope, bound, present...)
			current[scope] = latest[scope]
		}
		assertSubsetOfOracle(t, current, oracle)
	}
	return latest, oracle
}

// assertFleetMatchesOracle proves the union of each source's latest own run
// equals the oracle on identity and content, with no copy disagreeing.
func assertFleetMatchesOracle(t *testing.T, latest map[string]map[string]sdFinding, oracle map[string]sdFinding) {
	t.Helper()
	fleet := map[string]sdFinding{}
	for _, findings := range latest {
		for key, f := range findings {
			if prior, ok := fleet[key]; ok && prior != f {
				t.Fatalf("copies of %s disagree on content: %+v vs %+v", key, prior, f)
			}
			fleet[key] = f
		}
	}
	if len(oracle) != 1+len(sdKernelCVEs) {
		t.Fatalf("oracle must hold S plus %d kernel findings, got %d: %s", len(sdKernelCVEs), len(oracle), sdKeys(oracle))
	}
	if sdKeys(fleet) != sdKeys(oracle) {
		t.Fatalf("fleet findings differ from oracle:\nfleet=%s\noracle=%s", sdKeys(fleet), sdKeys(oracle))
	}
	for key, want := range oracle {
		if fleet[key] != want {
			t.Fatalf("fleet content differs for %s:\nfleet=%+v\noracle=%+v", key, fleet[key], want)
		}
	}
}

// TestCriteriaExpansionBoundImageArrivalFleetParity is the image-scope
// arrival matrix of the P1 ruling (fixtures a to e and h). Each case runs
// with the bound on and off, compares finding content with the oracle, keeps
// loads within the round cap, and never stops on a truncation cause.
func TestCriteriaExpansionBoundImageArrivalFleetParity(t *testing.T) {
	t.Parallel()

	base := buildSharedDocumentUniverse("0.10", true)
	attInImage := base.moveAttachment(sdScopeM, sdScopeIM)
	earlyImage := attInImage.withoutAttachment()
	withRepo := base.withRepoScope()
	sources := []string{sdScopeB, sdScopeKB, sdScopeM}
	allSources := append(append([]string(nil), sources...), sdScopeIM)

	cases := []struct {
		name  string
		steps []sdStep
	}{
		{"a image-last attachment in sbom scope, only the image scope runs", []sdStep{
			{u: base, arrive: sources, run: sources},
			{u: base, arrive: []string{sdScopeIM}, run: []string{sdScopeIM}},
		}},
		{"b image-last attachment in image scope, only the image scope runs", []sdStep{
			{u: attInImage, arrive: sources, run: sources},
			{u: attInImage, arrive: []string{sdScopeIM}, run: []string{sdScopeIM}},
		}},
		{"c late attachment in image scope, only the image scope re-runs", []sdStep{
			{u: earlyImage, arrive: allSources, run: allSources},
			{u: attInImage, run: []string{sdScopeIM}},
		}},
		{"d image-last with the catalog fan-out re-running every consumer", []sdStep{
			{u: attInImage, arrive: sources, run: sources},
			{u: attInImage, arrive: []string{sdScopeIM}, run: allSources},
		}},
		{"e vuln-last: sbom, then image with attachment, then the batches", []sdStep{
			{u: attInImage, arrive: []string{sdScopeM}, run: []string{sdScopeM}},
			{u: attInImage, arrive: []string{sdScopeIM}, run: []string{sdScopeIM}},
			{u: attInImage, arrive: []string{sdScopeB, sdScopeKB}, run: []string{sdScopeB, sdScopeKB}},
		}},
		// h: the image arrives and runs, then the repository scope arrives
		// last and reaches the digest through the image identity row (a
		// foreign row for it). The image scope's own run carries the findings
		// (own-digest rule, no fan-out needed); the repository run's own
		// output is logged per arm and must stay a content-equal subset.
		{"h repository scope last reaches the digest through the image identity row", []sdStep{
			{u: withRepo, arrive: sources, run: sources},
			{u: withRepo, arrive: []string{sdScopeIM}, run: []string{sdScopeIM}},
			{u: withRepo, arrive: []string{sdScopeRepo}, run: []string{sdScopeRepo}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, bound := range []bool{true, false} {
				name := "unbounded"
				if bound {
					name = "bounded"
				}
				t.Run(name, func(t *testing.T) {
					latest, oracle := sdSequence(t, tc.steps, bound)
					assertFleetMatchesOracle(t, latest, oracle)
				})
			}
		})
	}
}

// TestCriteriaExpansionBoundBatchWithImageScopeAttachmentStaysBounded is
// fixture f: an attachment in the image scope must not make the document own
// for batch B, which holds no digest of its own. B loads the foreign
// component (non-vacuous: comp-w) but none of the kernel rows.
func TestCriteriaExpansionBoundBatchWithImageScopeAttachmentStaysBounded(t *testing.T) {
	t.Parallel()

	u := buildSharedDocumentUniverse("0.10", false).moveAttachment(sdScopeM, sdScopeIM)
	assertBatchLoadsNoKernelRows(t, u.present(sdScopeB, sdScopeKB, sdScopeM, sdScopeIM))
}

// TestCriteriaExpansionBoundSuppressionDigestDoesNotRootDocument is fixture
// g: a suppression row in B's scope names the image digest, which the filter
// follows to the attachment and document (non-vacuous: att-m and comp-w
// load), but a suppression row never roots a document, so B stays bounded.
func TestCriteriaExpansionBoundSuppressionDigestDoesNotRootDocument(t *testing.T) {
	t.Parallel()

	u := buildSharedDocumentUniverse("0.10", false).withBatchSuppression()
	assertBatchLoadsNoKernelRows(t, u.present(sdScopeB, sdScopeKB, sdScopeM, sdScopeIM))
}

func assertBatchLoadsNoKernelRows(t *testing.T, all []facts.Envelope) {
	t.Helper()
	loaded, _, outcome, loads := runSharedDocumentPass(t, all, sdScopeB, true)
	ids := loadedFactIDs(loaded)
	for _, want := range []string{"cve-s", "prod-s", "comp-a", "comp-w", "att-m"} {
		if !containsID(ids, want) {
			t.Fatalf("batch B missing %q (the foreign document must be reached), got %v", want, ids)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "prod-k") || strings.HasPrefix(id, "cve-k") || id == "epss-k1" {
			t.Fatalf("batch B loaded kernel row %q though the document is not its own, got %v", id, ids)
		}
	}
	if outcome.SuppressedTransitiveCriteria < 1 {
		t.Fatalf("the foreign component's wildcard must be counted as suppressed, outcome = %+v", outcome)
	}
	t.Logf("batch B loads=%d suppressed=%d", loads, outcome.SuppressedTransitiveCriteria)
}

// TestCriteriaBoundOwnershipDigestsMatchFilter pins the lean ownership digest
// helper to the digest keys of supplyChainImpactFilter, so a digest key added
// to the filter cannot silently stop counting as own. Suppression rows are
// the one intentional difference.
func TestCriteriaBoundOwnershipDigestsMatchFilter(t *testing.T) {
	t.Parallel()

	payload := func() map[string]any {
		return map[string]any{
			"subject_digest": "d-subject", "referrer_digest": "d-referrer", "digest": "d-digest",
			"image_digest": "d-image", "resolved_digest": "d-resolved", "artifact_digest": "d-artifact",
		}
	}
	kinds := []string{
		reducercontract.SBOMAttestationAttachmentFactKind, reducercontract.ContainerImageIdentityFactKind,
		facts.ScannerWorkerAnalysisFactKind, chain.OCIImageManifestFactKind, chain.OCIImageIndexFactKind,
		chain.OCIImageTagObservationFactKind, chain.OCIImageReferrerFactKind, cicdrun.CICDRunCorrelationFactKind,
	}
	for _, kind := range kinds {
		env := facts.Envelope{FactID: "f-" + kind, FactKind: kind, Payload: payload()}
		got := ownershipDigests(env)
		var kept []string
		for _, d := range got {
			if d != "" {
				kept = append(kept, d)
			}
		}
		sort.Strings(kept)
		want := supplyChainImpactFilter([]facts.Envelope{env}).SubjectDigests
		if strings.Join(kept, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: ownership digests %v differ from the filter's %v", kind, kept, want)
		}
		if len(kept) == 0 {
			t.Fatalf("%s: the drift check is vacuous, no digest read", kind)
		}
	}
	suppression := facts.Envelope{
		FactID: "s", FactKind: chain.VulnerabilitySuppressionFactKind,
		Payload: map[string]any{"scope": map[string]any{"subject_digest": "d-supp"}},
	}
	if got := ownershipDigests(suppression); len(got) != 0 {
		t.Fatalf("suppression rows must never root a document, got %v", got)
	}
	if len(supplyChainImpactFilter([]facts.Envelope{suppression}).SubjectDigests) == 0 {
		t.Fatal("control: the filter reads the suppression digest, so excluding it is deliberate")
	}
}
