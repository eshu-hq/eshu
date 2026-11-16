// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"maps"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The Odù names the vulnerability collector's recording backs
// (eshu-hq/eshu#10734). They exist only in the cassette-backed catalog,
// CatalogAt and CatalogByNameAt.
const (
	// VulnCollectorOduName is the NVD product-path Odù: one recorded window, KEV
	// and EPSS, and a planted SBOM component with the exact CPE of the window's
	// first record.
	VulnCollectorOduName = "odu:vuln-collector-nvd"
	// VulnCollectorWildcardRangeOduName plants a component the window's wildcard
	// CPE only matches through its version range, which the exact compare misses.
	VulnCollectorWildcardRangeOduName = "odu:vuln-collector-nvd-wildcard-range"
	// VulnCollectorRejectedOduName plants a component for a rejected CVE.
	VulnCollectorRejectedOduName = "odu:vuln-collector-nvd-rejected"
	// VulnCollectorNewestCopyOduName selects the window that re-issues a CVE as
	// rejected, beside the window that first carried it.
	VulnCollectorNewestCopyOduName = "odu:vuln-collector-nvd-newest-copy"
	// VulnCollectorNoBaselineOduName plants a component for a CVE only an older
	// window carries.
	VulnCollectorNoBaselineOduName = "odu:vuln-collector-nvd-no-baseline"
)

// vulnCollectorCassetteRelPath is the recording of the real vulnerability
// collector's output. The collector satellite records it by running its own
// claim path against local fixture upstreams and writes it with
// `make ifa-cassette`; a sha256 sidecar and the satellite's drift gate pin it.
const vulnCollectorCassetteRelPath = "testdata/cassettes/vulnerabilityintelligence/vuln-collector-nvd.json"

// The five scopes the recording holds. The first window carries five CVEs, one
// of each shape the Odùs need (an exact CPE, a wildcard CPE with a version
// range, a rejected CVE, a vector-only CVSS record and a label-only one). The
// second window re-issues the first CVE as rejected. The old window carries one
// CVE the others do not, the stand-in for a baseline copy.
const (
	vulnWindow1Scope = "vuln-intel://nvd/_window/1793491200000000-1794096000000000"
	vulnWindow2Scope = "vuln-intel://nvd/_window/1794096000000000-1794700800000000"
	vulnWindowXScope = "vuln-intel://nvd/_window/1740787200000000-1741392000000000"
	vulnKEVScope     = "vuln-intel://cisa/kev"
	vulnEPSSScope    = "vuln-intel://first/epss"
)

// The CPEs the Odùs plant on their SBOM component. Each names a product one of
// the recorded CVEs affects: the exact CPE of the first window's first record,
// a version inside the wildcard record's range, the rejected record's product
// and the old window's product.
const (
	vulnWidgetCPE       = "cpe:2.3:a:ifa-vendor:widget:1.2.0:*:*:*:*:*:*:*"
	vulnGadgetCPE       = "cpe:2.3:a:ifa-vendor:gadget:1.2.0:*:*:*:*:*:*:*"
	vulnLegacyToolCPE   = "cpe:2.3:a:ifa-vendor:legacy-tool:3.1.0:*:*:*:*:*:*:*"
	vulnBaselineOnlyCPE = "cpe:2.3:a:ifa-vendor:baseline-only:2.0.0:*:*:*:*:*:*:*"
)

// The invented SBOM side the planted facts describe: one image in one owned
// repository whose SBOM lists the vulnerable component. None of it is a real
// registry, repository or digest.
const (
	plantedScopeID       = "sbom://ifa/widget-service"
	plantedGenerationID  = "ifa-planted-generation"
	plantedDocumentID    = "ifa:sbom-document:widget-service@1.0.0"
	plantedRepositoryID  = "repo://ifa/widget-service"
	plantedImageRef      = "registry.example.test/ifa/widget-service:1.0.0"
	plantedSubjectDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// cassetteBackedSpecs returns the Odùs built from the vulnerability collector's
// recording. Expected outcomes come from running the reducer's pure impact
// builder over each Odù's facts, and vuln_collector_odu_test.go asserts them.
func cassetteBackedSpecs() []cassetteOduSpec {
	return []cassetteOduSpec{
		{
			name: VulnCollectorOduName, cassette: vulnCollectorCassetteRelPath,
			scopes:  []string{vulnWindow1Scope, vulnKEVScope, vulnEPSSScope},
			planted: plantedSBOMFacts(vulnWidgetCPE),
			detail:  "the collector's recorded NVD window, CISA KEV and FIRST EPSS plus a planted SBOM component with the exact CPE of the first record: one product-path finding with KEV, EPSS, CWE and severity attached",
		},
		{
			name: VulnCollectorWildcardRangeOduName, cassette: vulnCollectorCassetteRelPath,
			scopes:  []string{vulnWindow1Scope},
			planted: plantedSBOMFacts(vulnGadgetCPE),
			detail:  "the recorded NVD window plus a component inside a wildcard CPE's version range: the exact compare misses it, so no finding",
		},
		{
			name: VulnCollectorRejectedOduName, cassette: vulnCollectorCassetteRelPath,
			scopes:  []string{vulnWindow1Scope},
			planted: plantedSBOMFacts(vulnLegacyToolCPE),
			detail:  "the recorded NVD window plus a component for a rejected CVE: a finding that carries an advisory retraction",
		},
		{
			name: VulnCollectorNewestCopyOduName, cassette: vulnCollectorCassetteRelPath,
			scopes:  []string{vulnWindow1Scope, vulnWindow2Scope},
			planted: plantedSBOMFacts(vulnWidgetCPE),
			detail:  "the window that first carries a CVE and the window that re-issues it as rejected: the newest copy should retract it, and finding F1 pins what the reducer does with NVD's zone-less timestamps today",
		},
		{
			name: VulnCollectorNoBaselineOduName, cassette: vulnCollectorCassetteRelPath,
			scopes:  []string{vulnWindow1Scope},
			planted: plantedSBOMFacts(vulnBaselineOnlyCPE),
			detail:  "the recorded NVD window plus a component for a CVE only an older window carries: no finding until that window is selected",
		},
	}
}

// plantedSBOMFacts returns the SBOM-side facts the NVD product path needs to
// reach an owned image: one sbom.component per CPE, the attachment that ties
// the SBOM document to the image digest, and the image identity that names the
// owning repository. The collector under test does not emit them, so the Odù
// plants them. The component starts from the fixturepack payload, so it
// satisfies the sbom.component schema.
func plantedSBOMFacts(cpes ...string) []facts.Envelope {
	valid, ok := fixturepack.ValidPayload(chain.SBOMComponentFactKind)
	if !ok {
		panic(fmt.Sprintf("ifa: vuln collector odu: fixturepack has no valid payload example for %q", chain.SBOMComponentFactKind))
	}
	out := make([]facts.Envelope, 0, len(cpes)+2)
	for _, cpe := range cpes {
		product, version := cpeProductVersion(cpe)
		payload := maps.Clone(valid)
		payload["document_id"] = plantedDocumentID
		payload["component_id"] = "ifa:sbom-component:" + product + "@" + version
		payload["purl"] = "pkg:generic/" + product + "@" + version
		payload["package_id"] = "generic:" + product
		payload["cpe"] = cpe
		payload["name"] = product
		payload["version"] = version
		out = append(out, plantedEnvelope(chain.SBOMComponentFactKind, cpe, payload))
	}
	return append(out,
		plantedEnvelope(reducercontract.SBOMAttestationAttachmentFactKind, plantedDocumentID, map[string]any{
			"document_id":       plantedDocumentID,
			"subject_digest":    plantedSubjectDigest,
			"attachment_status": "attached_verified",
			"canonical_writes":  1,
		}),
		plantedEnvelope(reducercontract.ContainerImageIdentityFactKind, plantedSubjectDigest, map[string]any{
			"digest":           plantedSubjectDigest,
			"repository_id":    plantedRepositoryID,
			"image_ref":        plantedImageRef,
			"identity_format":  containerimage.ContainerImageIdentityFormatImageRef,
			"canonical_writes": 1,
		}),
	)
}

// plantedEnvelope wraps one planted payload. Its fact id derives from the kind
// and key, so a planted fact has the same id on every run.
func plantedEnvelope(kind, key string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:        facts.StableID("IfaPlantedFact", map[string]any{"fact_kind": kind, "stable_key": key}),
		ScopeID:       plantedScopeID,
		GenerationID:  plantedGenerationID,
		FactKind:      kind,
		StableFactKey: key,
		Payload:       payload,
	}
}

// cpeProductVersion returns the product and version of a CPE 2.3 formatted
// string. Every caller passes a constant of this file, so a malformed one is a
// programming error.
func cpeProductVersion(cpe string) (product, version string) {
	parts := strings.Split(cpe, ":")
	if len(parts) < 6 || parts[0] != "cpe" || parts[1] != "2.3" {
		panic(fmt.Sprintf("ifa: vuln collector odu: %q is not a CPE 2.3 string", cpe))
	}
	return parts[4], parts[5]
}
