// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const (
	// IDImage identifies the image-unpacking stdio component.
	IDImage = "eshu.scanner.image"
	// IDSBOM identifies the SBOM-generation stdio component.
	IDSBOM = "eshu.scanner.sbom"
	// IDOSRuntime identifies the OS-package-extraction stdio component.
	IDOSRuntime = "eshu.scanner.osruntime"
	// Version is the first per-analyzer component manifest version.
	Version = "0.1.0"
	// CollectorSDKProtocolV1Alpha3 is the paged collector wire contract
	// these components speak. It mirrors collector.ProtocolVersionV1Alpha3
	// in sdk/go/collector: the value must stay identical because the host
	// admits the protocol by string and the pinned eshu-sdk/collector
	// module predates the constant.
	CollectorSDKProtocolV1Alpha3 = "collector-sdk/v1alpha3"
)

// Scanner-Publish-SHA anchors the pins below to the merge-head publish
// that produced them (docker-publish run 41237451240, merge a371d1e21f;
// each digest's build attestation verified via the attestations API
// before pinning). The verify-scanner-digest-pins gate
// (scripts/verify-scanner-digest-pins.sh, blocking tier pre-pr row
// scanner-digest-pins) resolves each pin against the immutable
// sha-<publish-sha> tag, re-verifies the attestation, and fails when
// scanner sources advance past this anchor. A republish therefore lands
// in two changes: the publish itself, then a reviewed follow-up PR
// bumping this SHA and the pins together (the pattern #9494 uses) —
// the publish workflow never auto-updates this file.
// Scanner-Publish-SHA: a371d1e21f6ba013925ea5d263d8de6c6b54d428
//
// Pin semantics (#8878): these pins are platform-side admission truth, not
// seed-time comparisons. Seed checks only that --image-ref has the
// ghcr.io/eshu-hq/scanner-<name>@sha256:<64 hex> shape for its own
// component and stamps the value into the seeded manifest, because the pin
// changes the image digest and a binary can never satisfy equality against
// its own image (fixed-point impossibility). Equality is enforced later by
// VerifySeededArtifact, which the extension worker loader and the
// coordinator activation planner run over the installed manifest before any
// claim or execution: a drifted seed image is denied there. The re-pin PR
// therefore bumps this SHA, the pins, and the chart seed.image entries
// together (the activation contract test pins the mirrored in-repo chart
// values; live-chart sync rides the re-pin PR process with admission as the
// fail-closed backstop).
var artifactDigests = map[string]string{
	IDImage:     "450f81b8195120a59decc55fe85ab648d974371269ed1f1602e3b8dff736b6ce",
	IDSBOM:      "76d36e278eac680e3508fd17e26bc4ad2324a1a64438bec083c5b534e1fe834f",
	IDOSRuntime: "04eea122b2843569e07c695b2d6a5795c5fe1f2a6f92d43d77b3bc469949a697",
}

// AnalyzerFor resolves a component ID back to its analyzer. Unknown IDs fail
// closed so a binary never executes another analyzer's profile.
func AnalyzerFor(id string) (scannerworker.AnalyzerKind, error) {
	switch id {
	case IDImage:
		return scannerworker.AnalyzerImageUnpacking, nil
	case IDSBOM:
		return scannerworker.AnalyzerSBOMGeneration, nil
	case IDOSRuntime:
		return scannerworker.AnalyzerOSPackageExtraction, nil
	default:
		return "", fmt.Errorf("unknown scanner component %q", id)
	}
}

// family declares one emitted fact kind with its schema version.
type family struct {
	kind    string
	version string
}

// emittedKinds lists every fact kind one analyzer can emit, including
// degraded-path warnings (unsupported targets, malformed lockfiles, partial
// snapshots). The host rejects undeclared kinds at admission, so this list
// must cover every envelope constructor the analyzer reaches, not only the
// clean-path kinds a representative probe observes.
func emittedKinds(kind scannerworker.AnalyzerKind) ([]family, error) {
	switch kind {
	case scannerworker.AnalyzerImageUnpacking:
		return []family{
			{kind: facts.ScannerWorkerAnalysisFactKind, version: facts.ScannerWorkerSchemaVersionV1},
			{kind: facts.ScannerWorkerWarningFactKind, version: facts.ScannerWorkerSchemaVersionV1},
			{kind: chain.VulnerabilityOSPackageFactKind, version: chain.VulnerabilityIntelligenceSchemaVersionV1},
			{kind: chain.VulnerabilityWarningFactKind, version: chain.VulnerabilityIntelligenceSchemaVersionV1},
		}, nil
	case scannerworker.AnalyzerSBOMGeneration:
		return []family{
			{kind: chain.SBOMDocumentFactKind, version: chain.SBOMAttestationSchemaVersionV1},
			{kind: chain.SBOMComponentFactKind, version: chain.SBOMAttestationSchemaVersionV1},
			{kind: chain.SBOMWarningFactKind, version: chain.SBOMAttestationSchemaVersionV1},
		}, nil
	case scannerworker.AnalyzerOSPackageExtraction:
		return []family{
			{kind: chain.VulnerabilityOSPackageFactKind, version: chain.VulnerabilityIntelligenceSchemaVersionV1},
			{kind: chain.VulnerabilityWarningFactKind, version: chain.VulnerabilityIntelligenceSchemaVersionV1},
		}, nil
	default:
		return nil, fmt.Errorf("no component manifest for analyzer %q", kind)
	}
}

// componentID returns the component identity for one analyzer.
func componentID(kind scannerworker.AnalyzerKind) (string, error) {
	switch kind {
	case scannerworker.AnalyzerImageUnpacking:
		return IDImage, nil
	case scannerworker.AnalyzerSBOMGeneration:
		return IDSBOM, nil
	case scannerworker.AnalyzerOSPackageExtraction:
		return IDOSRuntime, nil
	default:
		return "", fmt.Errorf("no component identity for analyzer %q", kind)
	}
}

// ManifestFor returns the v1alpha3 process-adapter manifest for one scanner
// analyzer. Reducer-lane analyzers are rejected: the binaries only execute
// the scanner lane.
func ManifestFor(kind scannerworker.AnalyzerKind) (component.Manifest, error) {
	lane, ok := scannerworker.AnalyzerLane(kind)
	if !ok {
		return component.Manifest{}, fmt.Errorf("unknown analyzer %q", kind)
	}
	if lane != scannerworker.LaneScannerWorker {
		return component.Manifest{}, fmt.Errorf("analyzer %q belongs to %q, not a component lane", kind, lane)
	}
	id, err := componentID(kind)
	if err != nil {
		return component.Manifest{}, err
	}
	digest, ok := artifactDigests[id]
	if !ok {
		return component.Manifest{}, fmt.Errorf("no published image digest pinned for scanner component %q", id)
	}
	kinds, err := emittedKinds(kind)
	if err != nil {
		return component.Manifest{}, err
	}
	families := make([]component.FactFamily, 0, len(kinds))
	for _, family := range kinds {
		families = append(families, component.FactFamily{
			Kind:             family.kind,
			SchemaVersions:   []string{family.version},
			SourceConfidence: []string{"reported"},
		})
	}
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        id,
			Name:      fmt.Sprintf("Scanner %s component", kind),
			Publisher: "eshu-hq",
			Version:   Version,
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"scanner_worker"},
			Runtime: component.RuntimeContract{
				SDKProtocol: CollectorSDKProtocolV1Alpha3,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "linux/arm64",
				// Pinned to the published image digest from
				// artifactDigests; unknown ids fail closed above.
				Image: "ghcr.io/eshu-hq/scanner-" + strings.TrimPrefix(id, "eshu.scanner.") + "@sha256:" + digest,
			}},
			EmittedFacts: families,
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: []string{"source_evidence_only:no_graph_truth"}},
			},
			Telemetry: component.Telemetry{MetricsPrefix: "eshu_component_scanner_" + strings.TrimPrefix(id, "eshu.scanner.")},
		},
	}, nil
}

// RequiredGrants returns the live producer grants one manifest needs: one
// grant per declared fact family, pinned to the manifest id and version.
// Production activations issue these through `eshu component grant`, which
// assigns the bounded lifetime activation policy chooses (there are no
// non-expiring grants, and expired grants deny admission fail-closed); the
// constructor defines the required SET the host admission enforces, with a
// far-future expiry so fixtures stay live. Never mint production grants
// from this constructor output: set lifetimes and renew through the
// activation grant flow.
func RequiredGrants(manifest component.Manifest) []component.ProducerGrant {
	grants := make([]component.ProducerGrant, 0, len(manifest.Spec.EmittedFacts))
	for _, family := range manifest.Spec.EmittedFacts {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     manifest.Metadata.ID,
			Version:        manifest.Metadata.Version,
			Kind:           family.Kind,
			SchemaVersions: append([]string(nil), family.SchemaVersions...),
			Scope:          "scanner_worker",
			ExpiresAt:      time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	return grants
}
