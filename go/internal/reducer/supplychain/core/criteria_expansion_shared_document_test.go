// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Shared-SBOM-document and arrival-order proofs for the #11087 criteria
// bound (arbiter ruling on the 2026-11-21 #10972 comment). The universe has
// three sources: batch B (seed CVE S with a versioned product), batch KB
// (kernel CVEs sharing one wildcard product), and SBOM scope M (one document
// holding a component that bears S's product and a component that bears the
// kernel wildcard). B does not hold the shared kernel product but reaches
// M's kernel component through the shared document. The tests prove that B
// never follows that foreign component's product name back into kernel
// vulnerability records, and that the union of every source's own run still
// equals the full-universe oracle for each arrival order and after an
// EPSS/KEV change, comparing finding CONTENT and not only identity.

package core

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

const (
	sdScopeB  = "scope:vuln-batch-b"
	sdScopeKB = "scope:vuln-batch-kernel"
	sdScopeM  = "scope:sbom-m"
	sdScopeIM = "scope:image-m"

	sdCVES     = "CVE-2026-0801"
	sdCritS    = "cpe:2.3:a:seeded:gadget:2.0:*:*:*:*:*:*:*"
	sdWildcard = "cpe:2.3:o:linux:linux_kernel:*:*:*:*:*:*:*:*"
	sdDigestM  = "sha256:shared-document-m"
	sdRepoM    = "repo://example/m-service"
)

var sdKernelCVEs = []string{"CVE-2026-0811", "CVE-2026-0812", "CVE-2026-0813"}

// sdScoped stamps the source scope on every envelope.
func sdScoped(scope string, envelopes ...facts.Envelope) []facts.Envelope {
	for i := range envelopes {
		envelopes[i].ScopeID = scope
		envelopes[i].GenerationID = "generation:" + scope
	}
	return envelopes
}

// sdUniverse maps each source scope to its own facts. The image identity
// lives in its own scope, as in production: every pass reaches it by digest.
type sdUniverse map[string][]facts.Envelope

// buildSharedDocumentUniverse builds the sources. epssK1 is the EPSS
// probability on the first kernel CVE; kevS adds a KEV listing on S.
func buildSharedDocumentUniverse(epssK1 string, kevS bool) sdUniverse {
	u := sdUniverse{}
	b := sdScoped(sdScopeB,
		vulnerabilityCVEFact("cve-s", sdCVES, 9.1),
		vulnerabilityAffectedProductFact("prod-s", sdCVES, sdCritS, "match-s", true))
	if kevS {
		b = append(b, sdScoped(sdScopeB, vulnerabilityKEVFact("kev-s", sdCVES))...)
	}
	u[sdScopeB] = b
	var kb []facts.Envelope
	for i, cve := range sdKernelCVEs {
		n := string(rune('1' + i))
		kb = append(kb, sdScoped(sdScopeKB,
			vulnerabilityCVEFact("cve-k"+n, cve, 7.0),
			vulnerabilityAffectedProductFact("prod-k"+n, cve, sdWildcard, "match-k"+n, true))...)
	}
	kb = append(kb, sdScoped(sdScopeKB, vulnerabilityEPSSFact("epss-k1", sdKernelCVEs[0], epssK1, "0.50"))...)
	u[sdScopeKB] = kb
	u[sdScopeM] = sdScoped(sdScopeM,
		sbomComponentCPEImpactFact("comp-a", "doc-m", sdCritS),
		sbomComponentCPEImpactFact("comp-w", "doc-m", sdWildcard),
		sbomAttachmentImpactFact("att-m", "doc-m", sdDigestM))
	u[sdScopeIM] = sdScoped(sdScopeIM, containerImageIdentityImpactFact("img-m", sdDigestM, sdRepoM))
	return u
}

// present returns every envelope of the listed scopes. The image scope is an
// arriving source like the others: a pass that needs it names it.
func (u sdUniverse) present(scopes ...string) []facts.Envelope {
	var out []facts.Envelope
	for _, scope := range scopes {
		out = append(out, u[scope]...)
	}
	return out
}

// sdLoader serves the active-evidence probes over a universe through the
// production binding (branches.go): the CVE probes return every vulnerability
// row and signal of the listed CVEs, the alias probes return CVE records
// only, the affected_product_criteria probe binds the component-origin list
// alone, the component probes bind the union (criteria) or the document list,
// and the attachment and image probes answer the document and digest arms.
func sdLoader(all []facts.Envelope) func(SupplyChainImpactFactFilter) []facts.Envelope {
	set := func(lists ...[]string) map[string]struct{} {
		out := map[string]struct{}{}
		for _, list := range lists {
			for _, v := range list {
				out[v] = struct{}{}
			}
		}
		return out
	}
	return func(f SupplyChainImpactFactFilter) []facts.Envelope {
		cveOK := set(f.CVEIDs, f.AdvisoryIDs)
		aliasOK := set(f.AliasIDs)
		productOK := set(f.ProductCriteria)
		componentOK := set(f.ProductCriteria, f.SeedProductCriteria)
		docs := set(f.DocumentIDs)
		digests := set(f.SubjectDigests)
		repos := set(f.RepositoryIDs)
		var out []facts.Envelope
		for _, env := range all {
			switch env.FactKind {
			case chain.VulnerabilityCVEFactKind:
				id := supplyChainCVEID(env.Payload)
				_, byCVE := cveOK[id]
				_, byAlias := aliasOK[id]
				if byCVE || byAlias {
					out = append(out, env)
				}
			case chain.VulnerabilityAffectedProductFactKind:
				_, byCVE := cveOK[supplyChainCVEID(env.Payload)]
				_, byCriteria := productOK[payloadcore.PayloadStr(env.Payload, "criteria")]
				if byCVE || byCriteria {
					out = append(out, env)
				}
			case chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityKnownExploitedFactKind:
				if _, ok := cveOK[supplyChainCVEID(env.Payload)]; ok {
					out = append(out, env)
				}
			case chain.SBOMComponentFactKind:
				_, byCPE := componentOK[payloadcore.PayloadStr(env.Payload, "cpe")]
				_, byDoc := docs[payloadcore.PayloadStr(env.Payload, "document_id")]
				if byCPE || byDoc {
					out = append(out, env)
				}
			case reducercontract.SBOMAttestationAttachmentFactKind:
				_, byDoc := docs[payloadcore.PayloadStr(env.Payload, "document_id")]
				_, byDigest := digests[payloadcore.PayloadStr(env.Payload, "subject_digest")]
				if byDoc || byDigest {
					out = append(out, env)
				}
			case reducercontract.ContainerImageIdentityFactKind:
				_, byDigest := digests[payloadcore.PayloadStr(env.Payload, "digest")]
				_, byRepo := repos[payloadcore.PayloadStr(env.Payload, "repository_id")]
				if byDigest || byRepo {
					out = append(out, env)
				}
			}
		}
		return out
	}
}

// runSharedDocumentPass runs one source's own pass: its initial evidence is
// the envelopes of its scope, and the loop runs with that scope as the
// intent scope, exactly as loadSupplyChainImpactEvidence does.
func runSharedDocumentPass(t *testing.T, all []facts.Envelope, scope string, bound bool) ([]facts.Envelope, supplyChainImpactTruncation, supplyChainImpactCriteriaBoundOutcome, int) {
	t.Helper()
	var initial []facts.Envelope
	for _, env := range all {
		if env.ScopeID == scope {
			initial = append(initial, env)
		}
	}
	stub := &stubSupplyChainImpactFactLoader{activeForFilter: sdLoader(all)}
	h := SupplyChainImpactHandler{FactLoader: stub}
	if bound {
		h.CriteriaExpansionBound = &SupplyChainImpactCriteriaExpansionBound{}
	}
	loaded, truncation, outcome, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), initial, supplyChainImpactFilter(initial),
		newSupplyChainImpactEvidenceBudget(0), nil, scope)
	if err != nil {
		t.Fatalf("loadActiveSupplyChainImpactFactsUntilStable(%s) error = %v", scope, err)
	}
	assertRoundsWithinCap(t, bound, outcome, truncation)
	if truncation.partial() {
		t.Fatalf("pass %s (bound=%v) stopped on a truncation cause: %+v", scope, bound, truncation)
	}
	return loaded, truncation, outcome, len(stub.filters)
}

// sdFinding is the finding CONTENT the arrival-order proofs compare: score,
// severity, KEV and EPSS enrichment, priority, anchor, and evidence ids.
type sdFinding struct {
	CVE, Criteria, Status, SeverityLabel, EPSSProbability, PriorityBucket string
	ExploitedTier, Digest, Repository, Evidence                           string
	CVSS                                                                  float64
	KnownExploited                                                        bool
	PriorityScore                                                         int
}

// sdProject keys findings by identity (CVE, criteria, digest, repository)
// and keeps their full content. A duplicate identity inside one pass is a
// failure: a pass never emits one finding twice.
func sdProject(t *testing.T, findings []SupplyChainImpactFinding) map[string]sdFinding {
	t.Helper()
	out := map[string]sdFinding{}
	for _, f := range findings {
		evidence := append([]string(nil), f.EvidenceFactIDs...)
		sort.Strings(evidence)
		key := strings.Join([]string{f.CVEID, f.ProductCriteria, f.SubjectDigest, f.RepositoryID}, "|")
		if _, dup := out[key]; dup {
			t.Fatalf("pass emitted finding %q twice", key)
		}
		out[key] = sdFinding{
			CVE: f.CVEID, Criteria: f.ProductCriteria, Status: string(f.Status),
			SeverityLabel: f.SeverityLabel, EPSSProbability: f.EPSSProbability,
			PriorityBucket: f.PriorityBucket, ExploitedTier: f.ExploitedTier,
			Digest: f.SubjectDigest, Repository: f.RepositoryID,
			Evidence: strings.Join(evidence, ","), CVSS: f.CVSSScore,
			KnownExploited: f.KnownExploited, PriorityScore: f.PriorityScore,
		}
	}
	return out
}

func sdKeys(m map[string]sdFinding) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
}

// sdOracle is the complete-evidence truth: Build over every present fact.
func sdOracle(t *testing.T, u sdUniverse, scopes ...string) map[string]sdFinding {
	t.Helper()
	return sdProject(t, BuildSupplyChainImpactFindings(u.present(scopes...)))
}

// sdRun is one source's own pass followed by Build over what it loaded.
func sdRun(t *testing.T, u sdUniverse, scope string, bound bool, scopes ...string) map[string]sdFinding {
	t.Helper()
	loaded, _, _, loads := runSharedDocumentPass(t, u.present(scopes...), scope, bound)
	findings := sdProject(t, BuildSupplyChainImpactFindings(loaded))
	t.Logf("run %s bound=%v loads=%d findings=%d", scope, bound, loads, len(findings))
	return findings
}

// TestCriteriaExpansionBoundSharedDocumentDoesNotFollowForeignCriteria is the
// arbiter's parity fixture: batch B holds no kernel product, reaches M's
// kernel component through the shared document, and must load no kernel
// product row or kernel CVE record. The unbounded arm is the non-vacuous
// control: it follows the foreign component's wildcard into every kernel CVE.
func TestCriteriaExpansionBoundSharedDocumentDoesNotFollowForeignCriteria(t *testing.T) {
	t.Parallel()

	u := buildSharedDocumentUniverse("0.10", false)
	all := u.present(sdScopeB, sdScopeKB, sdScopeM, sdScopeIM)

	unboundedLoaded, _, _, unboundedLoads := runSharedDocumentPass(t, all, sdScopeB, false)
	unboundedIDs := loadedFactIDs(unboundedLoaded)
	if !containsID(unboundedIDs, "prod-k1") || !containsID(unboundedIDs, "cve-k3") {
		t.Fatalf("control: unbounded B must follow the foreign wildcard into kernel rows, got %v", unboundedIDs)
	}

	loaded, truncation, outcome, boundedLoads := runSharedDocumentPass(t, all, sdScopeB, true)
	ids := loadedFactIDs(loaded)
	for _, want := range []string{"cve-s", "prod-s", "comp-a", "comp-w", "att-m", "img-m"} {
		if !containsID(ids, want) {
			t.Fatalf("bounded B missing %q (hop B, document, anchor), got %v", want, ids)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "prod-k") || strings.HasPrefix(id, "cve-k") || id == "epss-k1" {
			t.Fatalf("bounded B loaded kernel row %q outside its seed set, got %v", id, ids)
		}
	}
	if truncation.partial() {
		t.Fatalf("bounded B must converge, truncation = %+v", truncation)
	}
	if outcome.SuppressedTransitiveCriteria < 1 {
		t.Fatalf("the foreign component's wildcard must be counted as suppressed, outcome = %+v", outcome)
	}
	if boundedLoads > unboundedLoads {
		t.Fatalf("bounded loads %d exceed unbounded loads %d", boundedLoads, unboundedLoads)
	}
	t.Logf("shared-document B: bounded loads=%d suppressed=%d, unbounded loads=%d", boundedLoads, outcome.SuppressedTransitiveCriteria, unboundedLoads)

	got := sdProject(t, BuildSupplyChainImpactFindings(loaded))
	if len(got) != 1 {
		t.Fatalf("B must emit exactly its own finding for %s, got %s", sdCVES, sdKeys(got))
	}
	for _, f := range got {
		if f.CVE != sdCVES {
			t.Fatalf("B emitted a finding for %s, want only %s", f.CVE, sdCVES)
		}
	}
}

// TestCriteriaExpansionBoundArrivalOrderFleetParity proves the union of each
// source's own run equals the oracle for every arrival order. A source's run
// executes when its scope arrives, so the vuln-first order relies on M's own
// run (hop A over M's own components) for the kernel findings, and the
// SBOM-first order relies on each batch's own run (hop B over its own seeds).
func TestCriteriaExpansionBoundArrivalOrderFleetParity(t *testing.T) {
	t.Parallel()

	orders := []struct {
		name  string
		steps [][]string
	}{
		{"vuln-first", [][]string{{sdScopeB, sdScopeKB, sdScopeIM}, {sdScopeM}}},
		{"sbom-first", [][]string{{sdScopeM, sdScopeIM}, {sdScopeB, sdScopeKB}}},
		{"all-at-once", [][]string{{sdScopeB, sdScopeKB, sdScopeM, sdScopeIM}}},
	}
	u := buildSharedDocumentUniverse("0.10", true)
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()
			var present []string
			latest := map[string]map[string]sdFinding{}
			for _, step := range order.steps {
				present = append(present, step...)
				for _, scope := range step {
					latest[scope] = sdRun(t, u, scope, true, present...)
				}
				assertSubsetOfOracle(t, latest, sdOracle(t, u, present...))
			}
			fleet := map[string]sdFinding{}
			for _, findings := range latest {
				for key, f := range findings {
					if prior, ok := fleet[key]; ok && prior != f {
						t.Fatalf("copies of %s disagree on content: %+v vs %+v", key, prior, f)
					}
					fleet[key] = f
				}
			}
			oracle := sdOracle(t, u, present...)
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
		})
	}
}

// assertSubsetOfOracle checks that no run emits a finding the full-evidence
// oracle does not hold, or holds with different content.
func assertSubsetOfOracle(t *testing.T, runs map[string]map[string]sdFinding, oracle map[string]sdFinding) {
	t.Helper()
	for scope, findings := range runs {
		for key, f := range findings {
			if want, ok := oracle[key]; !ok || want != f {
				t.Fatalf("run %s emitted %s = %+v, oracle = %+v (present=%v)", scope, key, f, want, ok)
			}
		}
	}
}

// TestCriteriaExpansionBoundEnrichmentChangeContent changes EPSS on a kernel
// CVE and adds a KEV listing on the seed after both sources settled. The
// sources that own the changed facts re-run and must emit the new content;
// the copies the SBOM and image scopes hold stay exactly as stale as they
// would be without the bound until those scopes re-run, and once they re-run
// the fleet equals the new oracle (fixture i of the 2026-11-21 P1 ruling).
func TestCriteriaExpansionBoundEnrichmentChangeContent(t *testing.T) {
	t.Parallel()

	scopes := []string{sdScopeB, sdScopeKB, sdScopeM, sdScopeIM}
	before := buildSharedDocumentUniverse("0.10", false)
	after := buildSharedDocumentUniverse("0.90", true)
	oracleAfter := sdOracle(t, after, scopes...)
	for key, f := range oracleAfter {
		if f.CVE == sdCVES && !f.KnownExploited {
			t.Fatalf("oracle must carry the KEV flag on %s: non-vacuous check, got %+v", key, f)
		}
	}

	// The seed's own run carries the KEV change; the kernel batch's own run
	// carries the EPSS change. Both come from their own CVE arm.
	for _, scope := range []string{sdScopeB, sdScopeKB} {
		run := sdRun(t, after, scope, true, scopes...)
		if len(run) == 0 {
			t.Fatalf("own run %s emitted no findings: non-vacuous check", scope)
		}
		for key, f := range run {
			if f != oracleAfter[key] {
				t.Fatalf("re-run %s content differs from oracle for %s:\nrun=%+v\noracle=%+v", scope, key, f, oracleAfter[key])
			}
		}
	}

	// A source that holds a consumer copy (the SBOM scope M, and the image
	// scope as a running source) keeps the pre-change content identical with
	// and without the bound, so the bound adds no staleness; its re-run then
	// matches the new oracle.
	for _, scope := range []string{sdScopeM, sdScopeIM} {
		staleBound := sdRun(t, before, scope, true, scopes...)
		staleUnbounded := sdRun(t, before, scope, false, scopes...)
		if sdKeys(staleBound) != sdKeys(staleUnbounded) {
			t.Fatalf("%s identities differ with and without the bound: %s vs %s", scope, sdKeys(staleBound), sdKeys(staleUnbounded))
		}
		for key, f := range staleBound {
			if staleUnbounded[key] != f {
				t.Fatalf("%s copy of %s differs with and without the bound:\nbound=%+v\nunbounded=%+v", scope, key, f, staleUnbounded[key])
			}
		}
		rerun := sdRun(t, after, scope, true, scopes...)
		if sdKeys(rerun) != sdKeys(oracleAfter) {
			t.Fatalf("%s re-run identities differ from oracle: %s vs %s", scope, sdKeys(rerun), sdKeys(oracleAfter))
		}
		for key, want := range oracleAfter {
			if rerun[key] != want {
				t.Fatalf("%s re-run content differs for %s:\nrun=%+v\noracle=%+v", scope, key, rerun[key], want)
			}
		}
	}
}
