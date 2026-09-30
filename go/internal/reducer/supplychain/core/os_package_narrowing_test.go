// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

func osPackageIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-7154-narrow",
		ScopeID:      "vuln-intel:debian:openssl",
		GenerationID: "generation-intel-7154",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	}
}

func installedOSPackage(scopeID, generationID, name, purl string) facts.Envelope {
	return facts.Envelope{
		FactID:       "os-" + scopeID + "-" + name,
		FactKind:     facts.VulnerabilityOSPackageFactKind,
		ScopeID:      scopeID,
		GenerationID: generationID,
		Payload: map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   name,
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   purl,
		},
	}
}

func TestSupplyChainImpactOSPackageAdvisoryTargetsDeriveLookupKeys(t *testing.T) {
	t.Parallel()

	musl := vulnerabilityAffectedPackageFactWithSource("a2", "CVE-2", "alpine", "ALPINE-1", "pkg:apk/alpine/musl", "apk", "Musl", "1", "2")
	musl.Payload["purl"] = "pkg:apk/alpine/musl@1.2.4"
	envelopes := []facts.Envelope{
		// package_id and purl forms both contribute their id; the vendor's
		// os:// identity is derived from purl vendor and name.
		vulnerabilityAffectedPackageFactWithSource("a1", "CVE-1", "debian", "DSA-1", "pkg:deb/debian/openssl", "deb", "OpenSSL", "1", "2"),
		// A different vendor source adds its own ecosystem and key.
		musl,
		// Not an affected_package fact: ignored.
		vulnerabilityCVEFact("c1", "CVE-3", 5),
	}
	ecosystems, packageIDs := supplyChainImpactOSPackageAdvisoryTargets(envelopes)
	if want := []string{"alpine", "debian"}; !slices.Equal(ecosystems, want) {
		t.Fatalf("ecosystems = %v, want %v", ecosystems, want)
	}
	for _, want := range []string{"pkg:deb/debian/openssl", "pkg:apk/alpine/musl"} {
		if !slices.Contains(packageIDs, want) {
			t.Fatalf("packageIDs = %v, want it to contain %q", packageIDs, want)
		}
	}
	// The os:// identity the matcher derives from purl vendor and lowercased
	// name is a key too, so it is requested even though no installed row can
	// carry it today.
	if !slices.Contains(packageIDs, "os://alpine/musl") {
		t.Fatalf("packageIDs = %v, want the derived os:// identity", packageIDs)
	}
	if !slices.IsSorted(packageIDs) {
		t.Fatalf("packageIDs = %v, want sorted (a stable read)", packageIDs)
	}
}

// TestSupplyChainImpactOSPackageStageSkipsTheReadWithoutLookupKeys pins that
// an intent with no OS affected package reads nothing and is not partial: the
// finding set depends only on installed rows matching one of its keys, so an
// empty key set is complete, not truncated.
func TestSupplyChainImpactOSPackageStageSkipsTheReadWithoutLookupKeys(t *testing.T) {
	t.Parallel()

	base := &scanScopedSupplyChainImpactFactLoader{factsByScope: map[string][]facts.Envelope{
		scanScopedFactLoaderKey(osPackageIntent().ScopeID, osPackageIntent().GenerationID): {
			vulnerabilityCVEFact("cve", "CVE-2026-1", 7.5),
			// An OS affected package with no id, purl or name has no lookup key:
			// nothing can be matched, so nothing is read.
			vulnerabilityAffectedPackageFactWithSource("aff", "CVE-2026-1", "debian", "DSA-1", "", "deb", "", "1.0", "1.1"),
		},
	}}
	loader := &limitHonoringOSPackageLoader{scanScopedSupplyChainImpactFactLoader: base, available: osPackageEnvelopes(50, "s", "g")}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader: loader, Writer: writer,
	}
	if _, err := handler.Handle(context.Background(), osPackageIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(loader.limits) != 0 {
		t.Fatalf("the OS-package reader was called %d times for an intent with no OS affected package", len(loader.limits))
	}
	if writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = true for an intent with nothing to read")
	}
}

// osPackageUniverse builds an installed fleet: matching openssl installs plus
// unrelated packages, each in its own scan scope with a scanner analysis.
func osPackageUniverse(unrelated int) (all []facts.Envelope, byScope map[string][]facts.Envelope) {
	byScope = map[string][]facts.Envelope{}
	add := func(scope, gen, name, purl, digest string) {
		all = append(all, installedOSPackage(scope, gen, name, purl))
		ref := "registry.example/" + name + "@" + digest
		byScope[scanScopedFactLoaderKey(scope, gen)] = []facts.Envelope{scannerWorkerAnalysisFact(scope, gen, digest, ref)}
	}
	add("scan-openssl-a", "gen-a", "openssl", "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12", testScannerAnalysisImageDigest)
	for i := range unrelated {
		add(fmt.Sprintf("scan-other-%03d", i), fmt.Sprintf("gen-%03d", i), fmt.Sprintf("other-%03d", i),
			fmt.Sprintf("pkg:deb/debian/other-%03d@1.0?arch=amd64", i), fmt.Sprintf("sha256:%064d", i+1))
	}
	return all, byScope
}

// prefixNarrowedOSPackageLoader is the narrowed reader in miniature: it returns
// only installed rows whose purl prefix (trimmed, cut at '@') is one of the
// requested package ids, like the production SQL.
type prefixNarrowedOSPackageLoader struct {
	*scanScopedSupplyChainImpactFactLoader
	universe []facts.Envelope
	got      [][]string
	returned int
}

func (l *prefixNarrowedOSPackageLoader) ListOSPackageAdvisoryFactEnvelopes(
	_ context.Context, _ []string, packageIDs []string, _ int,
) ([]facts.Envelope, int, bool, error) {
	l.got = append(l.got, append([]string(nil), packageIDs...))
	var out []facts.Envelope
	for _, envelope := range l.universe {
		purl, _ := envelope.Payload["purl"].(string)
		prefix, _, _ := strings.Cut(strings.TrimSpace(purl), "@")
		if slices.Contains(packageIDs, prefix) {
			out = append(out, envelope)
		}
	}
	l.returned = len(out)
	return out, 0, false, nil
}

// TestSupplyChainImpactNarrowedOSPackageReadDerivesTheSameFindingsAsAFullDrain
// is the differential proof for the OS-package and scanner-analysis stages of
// #7154: reading only the installed packages an intent's affected packages can
// match yields an identical finding set to draining the whole fleet. Its
// active-evidence fake returns nothing for the digest and peer stages, so both
// arms load zero identities and this test says nothing about identity
// reconciliation; that population differs by design and is pinned by
// TestSupplyChainImpactNarrowedReadReconcilesAgainstMatchedScansAndSameOCIPeers.
func TestSupplyChainImpactNarrowedOSPackageReadDerivesTheSameFindingsAsAFullDrain(t *testing.T) {
	t.Parallel()

	intent := osPackageIntent()
	universe, byScope := osPackageUniverse(40)
	intelFacts := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve", "CVE-2026-7154", "debian", "DSA-2026-7154", 7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N", "HIGH", "2026-06-05T12:00:00Z"),
		vulnerabilityAffectedPackageFactWithSource("aff", "CVE-2026-7154", "debian", "DSA-2026-7154",
			"pkg:deb/debian/openssl", "deb", "openssl", "3.0.11-1~deb12u2", "3.0.11-1~deb12u3"),
	}
	newBase := func() *scanScopedSupplyChainImpactFactLoader {
		factsByScope := map[string][]facts.Envelope{scanScopedFactLoaderKey(intent.ScopeID, intent.GenerationID): intelFacts}
		for scope, scoped := range byScope {
			factsByScope[scope] = scoped
		}
		return &scanScopedSupplyChainImpactFactLoader{factsByScope: factsByScope}
	}
	run := func(loader interface {
		SupplyChainImpactHandlerLoader
	},
	) SupplyChainImpactWrite {
		writer := &recordingSupplyChainImpactWriter{}
		handler := SupplyChainImpactHandler{
			FactLoader: loader, Writer: writer,
		}
		if _, err := handler.Handle(context.Background(), intent); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		return writer.write
	}

	full := run(&limitHonoringOSPackageLoader{scanScopedSupplyChainImpactFactLoader: newBase(), available: universe})
	narrowedLoader := &prefixNarrowedOSPackageLoader{scanScopedSupplyChainImpactFactLoader: newBase(), universe: universe}
	narrowed := run(narrowedLoader)

	if len(full.Findings) == 0 {
		t.Fatal("the full drain derived no finding; the differential would prove nothing")
	}
	if !reflect.DeepEqual(full.Findings, narrowed.Findings) {
		t.Fatalf("narrowed read changed the findings:\nfull:     %#v\nnarrowed: %#v", full.Findings, narrowed.Findings)
	}
	if full.PartialEvidence || narrowed.PartialEvidence {
		t.Fatal("neither read should be partial")
	}
	if narrowedLoader.returned != 1 || len(universe) != 41 {
		t.Fatalf("narrowed read returned %d of %d installed packages, want only the matching one", narrowedLoader.returned, len(universe))
	}
	if !slices.Contains(narrowedLoader.got[0], "pkg:deb/debian/openssl") {
		t.Fatalf("the reader was not given the affected package's key: %v", narrowedLoader.got)
	}
}

// SupplyChainImpactHandlerLoader is the loader surface the handler needs; it
// exists so the differential test can run one function over two loader types.
type SupplyChainImpactHandlerLoader interface {
	ListFacts(context.Context, string, string) ([]facts.Envelope, error)
	ListFactsByKind(context.Context, string, string, []string) ([]facts.Envelope, error)
}

// TestSupplyChainImpactOSPackageBudgetSpentMakesThePassPartial pins F3 of the
// #7154 review: a budget spent inside the OS-package stage marks the pass
// partial, so it upserts and retracts nothing, and the later expansion stages
// do not run. Deleting the line in loadSupplyChainImpactExpansionEvidence that
// folds the exhausted budget into the truncation must turn this red.
func TestSupplyChainImpactOSPackageBudgetSpentMakesThePassPartial(t *testing.T) {
	t.Parallel()

	intent := osPackageIntent()
	universe, byScope := osPackageUniverse(20)
	factsByScope := map[string][]facts.Envelope{
		scanScopedFactLoaderKey(intent.ScopeID, intent.GenerationID): {
			vulnerabilityCVEFact("cve", "CVE-2026-7154", 7.5),
			vulnerabilityAffectedPackageFactWithSource("aff", "CVE-2026-7154", "debian", "DSA-1",
				"pkg:deb/debian/openssl", "deb", "openssl", "3.0.11-1~deb12u2", "3.0.11-1~deb12u3"),
		},
	}
	for scope, scoped := range byScope {
		factsByScope[scope] = scoped
	}
	base := &scanScopedSupplyChainImpactFactLoader{factsByScope: factsByScope}
	loader := &limitHonoringOSPackageLoader{scanScopedSupplyChainImpactFactLoader: base, available: universe}
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	capture := &captureWriteWriter{inner: PostgresSupplyChainImpactWriter{DB: beginner}}
	handler := SupplyChainImpactHandler{
		FactLoader: loader, Writer: capture,
		EvidenceBudget: 3,
	}
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !capture.write.PartialEvidence {
		t.Fatal("PartialEvidence = false after the evidence budget was spent in the OS-package stage")
	}
	if retractionIssued(beginner.state.all) {
		t.Fatal("a pass that spent its budget issued the retraction")
	}
	if got := result.SubSignals["evidence_truncated_budget"]; got != 1 {
		t.Fatalf("SubSignals[evidence_truncated_budget] = %v, want 1", got)
	}
	for key := range base.kindCalls {
		if strings.HasPrefix(key, "scan-") {
			t.Fatalf("scan scope %q queried after the budget was spent", key)
		}
	}
	if len(loader.limits) != 1 || loader.limits[0] != 3 {
		t.Fatalf("reader limits = %v, want one call with the remaining budget 3", loader.limits)
	}
}

// TestSupplyChainImpactChunkedStagesStopWhenTheBudgetIsSpent pins F3's other
// half: the scanner-analysis, resolved-digest and peer-identity stages stop
// loading once the budget is spent instead of reading every remaining chunk.
func TestSupplyChainImpactChunkedStagesStopWhenTheBudgetIsSpent(t *testing.T) {
	t.Parallel()

	t.Run("resolved digests", func(t *testing.T) {
		t.Parallel()
		loader := &stubSupplyChainImpactFactLoader{
			activeForFilter: func(f SupplyChainImpactFactFilter) []facts.Envelope {
				return []facts.Envelope{{FactID: "identity-" + f.SubjectDigests[0], FactKind: reducercontract.ContainerImageIdentityFactKind}}
			},
		}
		handler := SupplyChainImpactHandler{FactLoader: loader}
		analyses := make([]facts.Envelope, 0, supplyChainImpactFilterChunkSize*3)
		for i := range supplyChainImpactFilterChunkSize * 3 {
			analyses = append(analyses, facts.Envelope{
				FactID: fmt.Sprintf("a-%d", i), FactKind: facts.ScannerWorkerAnalysisFactKind,
				Payload: map[string]any{"image_digest": fmt.Sprintf("sha256:%064d", i)},
			})
		}
		budget := newSupplyChainImpactEvidenceBudget(1)
		if _, _, err := handler.loadSupplyChainImpactResolvedDigestEvidenceFacts(context.Background(), analyses, budget); err != nil {
			t.Fatalf("error = %v", err)
		}
		if !budget.exhausted() {
			t.Fatal("budget not exhausted")
		}
		if got := len(loader.filters); got != 2 {
			t.Fatalf("digest chunk reads = %d, want 2 (the crossing chunk is kept, none after it)", got)
		}
	})

	t.Run("peer repositories", func(t *testing.T) {
		t.Parallel()
		loader := &stubSupplyChainImpactFactLoader{
			activeForFilter: func(f SupplyChainImpactFactFilter) []facts.Envelope {
				return []facts.Envelope{{FactID: "peer-" + f.RepositoryIDs[0], FactKind: reducercontract.ContainerImageIdentityFactKind}}
			},
		}
		handler := SupplyChainImpactHandler{FactLoader: loader}
		identities := make([]facts.Envelope, 0, supplyChainImpactFilterChunkSize*3)
		for i := range supplyChainImpactFilterChunkSize * 3 {
			identities = append(identities, facts.Envelope{
				FactID: fmt.Sprintf("i-%d", i), FactKind: reducercontract.ContainerImageIdentityFactKind,
				Payload: map[string]any{"repository_id": fmt.Sprintf("repo-%04d", i)},
			})
		}
		budget := newSupplyChainImpactEvidenceBudget(1)
		if _, _, err := handler.loadSupplyChainImpactPeerIdentityFacts(context.Background(), identities, budget); err != nil {
			t.Fatalf("error = %v", err)
		}
		if !budget.exhausted() {
			t.Fatal("budget not exhausted")
		}
		if got := len(loader.filters); got != 2 {
			t.Fatalf("peer chunk reads = %d, want 2", got)
		}
	})

	t.Run("scanner scopes", func(t *testing.T) {
		t.Parallel()
		factsByScope := map[string][]facts.Envelope{}
		envelopes := make([]facts.Envelope, 0, 10)
		for i := range 10 {
			scope, gen := fmt.Sprintf("scan-%d", i), fmt.Sprintf("gen-%d", i)
			factsByScope[scanScopedFactLoaderKey(scope, gen)] = []facts.Envelope{scannerWorkerAnalysisFact(scope, gen, fmt.Sprintf("sha256:%064d", i), "ref")}
			envelopes = append(envelopes, facts.Envelope{FactID: "os-" + scope, FactKind: facts.VulnerabilityOSPackageFactKind, ScopeID: scope, GenerationID: gen})
		}
		loader := &scanScopedSupplyChainImpactFactLoader{factsByScope: factsByScope}
		budget := newSupplyChainImpactEvidenceBudget(3)
		if _, err := (SupplyChainImpactHandler{FactLoader: loader}).loadSupplyChainImpactScannerAnalysisScopeFacts(context.Background(), envelopes, budget); err != nil {
			t.Fatalf("error = %v", err)
		}
		if !budget.exhausted() {
			t.Fatal("budget not exhausted")
		}
		if got := len(loader.kindCalls); got != 4 {
			t.Fatalf("scan scopes read = %d, want 4 (the crossing scope is kept, none after it)", got)
		}
	})
}
