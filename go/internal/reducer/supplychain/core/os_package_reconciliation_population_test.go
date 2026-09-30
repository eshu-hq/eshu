// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// identityServingLoader is the active-evidence reader in miniature for the
// reconciliation proofs. Like the production identity stream it returns a
// container_image_identity fact when the filter names its digest, its
// repository_id (the OCI repository) or any of its source_repository_ids; a fake
// that matched on fewer fields could pass for a reason production would not.
// Every other read returns nothing.
type identityServingLoader struct {
	*scanScopedSupplyChainImpactFactLoader
	identities []facts.Envelope
	universe   []facts.Envelope
	narrow     bool
}

func (l *identityServingLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context, filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	var out []facts.Envelope
	for _, identity := range l.identities {
		digest, _ := identity.Payload["digest"].(string)
		repository, _ := identity.Payload["repository_id"].(string)
		sources, _ := identity.Payload["source_repository_ids"].([]string)
		byDigest := slices.Contains(filter.SubjectDigests, digest)
		byRepository := slices.Contains(filter.RepositoryIDs, repository)
		bySource := slices.ContainsFunc(sources, func(source string) bool { return slices.Contains(filter.RepositoryIDs, source) })
		if byDigest || byRepository || bySource {
			out = append(out, identity)
		}
	}
	return out, false, nil
}

func (l *identityServingLoader) ListOSPackageAdvisoryFactEnvelopes(
	_ context.Context, _ []string, packageIDs []string, _ int,
) ([]facts.Envelope, int, bool, error) {
	var out []facts.Envelope
	for _, envelope := range l.universe {
		purl, _ := envelope.Payload["purl"].(string)
		prefix, _, _ := strings.Cut(strings.TrimSpace(purl), "@")
		if !l.narrow || slices.Contains(packageIDs, prefix) {
			out = append(out, envelope)
		}
	}
	return out, 0, false, nil
}

// TestSupplyChainImpactNarrowedReadReconcilesAgainstMatchedScansAndSameOCIPeers
// pins the reconciliation population #7154 changes, with the digest and peer
// stages live in both arms. Identity reconciliation compares the scanner digest
// with every identity in the evidence set whose single source repository is the
// finding's repository, however that identity was loaded. A full drain of the
// installed fleet loads identities for every scanned image; the narrowed read
// loads only those reachable from the matched scans plus same-OCI-repository
// peers (the peer stage keys on the identity's repository_id).
//
// Universe: the matched scan's image D1 (OCI app-a, source R); a same-OCI peer D2
// (OCI app-a, source R) with no installed package; and an unrelated scan's image
// D3 (OCI app-b, same source R). Full: mismatch lines for D2 and D3. Narrowed:
// D2 only. D2 is the positive control that reconciliation ran in the narrowed
// arm. Status, repository and digest are equal in both arms and neither is
// partial.
func TestSupplyChainImpactNarrowedReadReconcilesAgainstMatchedScansAndSameOCIPeers(t *testing.T) {
	t.Parallel()

	intent := osPackageIntent()
	const (
		sourceRepo = "repository:r_monorepo"
		ociA       = "oci-registry://registry.example/app-a"
		ociB       = "oci-registry://registry.example/app-b"
		peerDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000002"
	)
	universe, byScope := osPackageUniverse(1)
	unrelatedDigest := "sha256:" + strings.Repeat("0", 63) + "1" // osPackageUniverse's first unrelated scan
	identities := []facts.Envelope{
		containerImageIdentityImpactFact("id-d1", testScannerAnalysisImageDigest, ociA),
		containerImageIdentityImpactFact("id-d2", peerDigest, ociA),
		containerImageIdentityImpactFact("id-d3", unrelatedDigest, ociB),
	}
	for i := range identities {
		identities[i].Payload["source_repository_ids"] = []string{sourceRepo}
	}
	intel := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve", "CVE-2026-7154", "debian", "DSA-2026-7154", 7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N", "HIGH", "2026-06-05T12:00:00Z"),
		vulnerabilityAffectedPackageFactWithSource("aff", "CVE-2026-7154", "debian", "DSA-2026-7154",
			"pkg:deb/debian/openssl", "deb", "openssl", "3.0.11-1~deb12u2", "3.0.11-1~deb12u3"),
	}
	run := func(narrow bool) SupplyChainImpactWrite {
		factsByScope := map[string][]facts.Envelope{scanScopedFactLoaderKey(intent.ScopeID, intent.GenerationID): intel}
		for scope, scoped := range byScope {
			factsByScope[scope] = scoped
		}
		loader := &identityServingLoader{
			scanScopedSupplyChainImpactFactLoader: &scanScopedSupplyChainImpactFactLoader{factsByScope: factsByScope},
			identities:                            identities, universe: universe, narrow: narrow,
		}
		writer := &recordingSupplyChainImpactWriter{}
		if _, err := (SupplyChainImpactHandler{FactLoader: loader, Writer: writer}).Handle(context.Background(), intent); err != nil {
			t.Fatalf("Handle(narrow=%v) error = %v", narrow, err)
		}
		return writer.write
	}
	full, narrowed := run(false), run(true)

	if len(full.Findings) != 1 || len(narrowed.Findings) != 1 {
		t.Fatalf("findings full=%d narrowed=%d, want one each", len(full.Findings), len(narrowed.Findings))
	}
	fullFinding, narrowedFinding := full.Findings[0], narrowed.Findings[0]
	if fullFinding.Status != narrowedFinding.Status ||
		fullFinding.RepositoryID != narrowedFinding.RepositoryID ||
		fullFinding.SubjectDigest != narrowedFinding.SubjectDigest {
		t.Fatalf("narrowing changed the finding's status, repository or digest:\nfull:     %+v\nnarrowed: %+v", fullFinding, narrowedFinding)
	}
	if fullFinding.RepositoryID != sourceRepo {
		t.Fatalf("RepositoryID = %q, want the identities' source repository %q (reconciliation only runs when anchored)", fullFinding.RepositoryID, sourceRepo)
	}
	if full.PartialEvidence || narrowed.PartialEvidence {
		t.Fatal("neither arm should be partial")
	}

	mismatch := func(finding SupplyChainImpactFinding) []string {
		var lines []string
		for _, line := range finding.MissingEvidence {
			if strings.HasPrefix(line, "scanner_identity_digest_mismatch:") {
				lines = append(lines, line)
			}
		}
		return lines
	}
	d2 := "scanner_identity_digest_mismatch: scanner=" + testScannerAnalysisImageDigest + ", identity=" + peerDigest
	d3 := "scanner_identity_digest_mismatch: scanner=" + testScannerAnalysisImageDigest + ", identity=" + unrelatedDigest
	// The finding sorts its evidence lines, and D3 (...01) sorts before D2 (...02).
	if got, want := mismatch(fullFinding), []string{d3, d2}; !slices.Equal(got, want) {
		t.Fatalf("full-drain mismatch lines = %v, want %v", got, want)
	}
	if got, want := mismatch(narrowedFinding), []string{d2}; !slices.Equal(got, want) {
		t.Fatalf("narrowed mismatch lines = %v, want only the same-OCI peer %v: an unrelated scan's identity under a different OCI repository is no longer compared", got, want)
	}
}
