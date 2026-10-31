// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"strings"
	"testing"
	"time"

	factschema "github.com/eshu-hq/eshu-sdk/factschema"
	sbomv1 "github.com/eshu-hq/eshu-sdk/factschema/sbom/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"
)

const satelliteSBOMSubjectDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// These envelopes model satellite-emitted generated SBOMs after the #8878
// in-tree scanner removal: sbom.document and sbom.component payloads built
// directly through the SDK factschema sbom/v1 encoders, with no analyzer,
// claim input, or output validation in the loop. Claim validation is
// satellite and host-admission scope; the reducer owns only the attachment
// truth these envelopes feed.
func satelliteSBOMEnvelope(t *testing.T, factKind, stableKey string, payload map[string]any) facts.Envelope {
	t.Helper()

	schemaVersion, _ := chain.SBOMAttestationSchemaVersion(factKind)
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return facts.Envelope{
		FactID:           factKind + ":" + stableKey,
		ScopeID:          "satellite://repository/sbom-case",
		GenerationID:     "satellite:generation-1",
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    "scanner_worker",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       now,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: "scanner_worker",
			ScopeID:      "satellite://repository/sbom-case",
			GenerationID: "satellite:generation-1",
			FactKey:      stableKey,
		},
	}
}

func satelliteSBOMDocumentPayload(t *testing.T, document sbomv1.Document) map[string]any {
	t.Helper()

	payload, err := factschema.EncodeSBOMDocument(document)
	if err != nil {
		t.Fatalf("EncodeSBOMDocument() error = %v, want nil", err)
	}
	return payload
}

func satelliteSBOMComponentPayload(t *testing.T, component sbomv1.Component) map[string]any {
	t.Helper()

	payload, err := factschema.EncodeSBOMComponent(component)
	if err != nil {
		t.Fatalf("EncodeSBOMComponent() error = %v, want nil", err)
	}
	return payload
}

func satelliteSBOMWarningPayload(t *testing.T, warning sbomv1.Warning) map[string]any {
	t.Helper()

	payload, err := factschema.EncodeSBOMWarning(warning)
	if err != nil {
		t.Fatalf("EncodeSBOMWarning() error = %v, want nil", err)
	}
	return payload
}

// TestSatelliteSBOMFactsAdmittedByReducerAttachment proves that satellite
// generated-SBOM source facts flow only through the existing reducer-owned
// attachment path. The reducer classifies the document as attached_parse_only
// when a subject digest is present, but reports that evidence as parse-only
// and unanchored until an OCI referrer proves image attachment. Satellites
// must never short-circuit attachment truth on their own.
func TestSatelliteSBOMFactsAdmittedByReducerAttachment(t *testing.T) {
	t.Parallel()

	withSubject := []facts.Envelope{
		satelliteSBOMEnvelope(t, chain.SBOMDocumentFactKind, "doc-with-subject", satelliteSBOMDocumentPayload(t, sbomv1.Document{
			DocumentID:          "satellite-doc-with-subject",
			Format:              satelliteStrPtr("cyclonedx"),
			SpecVersion:         satelliteStrPtr("1.6"),
			SubjectDigest:       satelliteStrPtr(satelliteSBOMSubjectDigest),
			SubjectDigests:      []string{satelliteSBOMSubjectDigest},
			ParseStatus:         satelliteStrPtr("generated"),
			GeneratedByAnalyzer: satelliteStrPtr("sbom_generation"),
			ComponentCount:      satelliteIntPtr(2),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMComponentFactKind, "comp-foo", satelliteSBOMComponentPayload(t, sbomv1.Component{
			DocumentID:  "satellite-doc-with-subject",
			ComponentID: satelliteStrPtr("satellite-doc-with-subject#foo"),
			PURL:        satelliteStrPtr("pkg:npm/foo@1.2.3"),
			Name:        satelliteStrPtr("foo"),
			Version:     satelliteStrPtr("1.2.3"),
			Type:        satelliteStrPtr("library"),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMComponentFactKind, "comp-bar", satelliteSBOMComponentPayload(t, sbomv1.Component{
			DocumentID:  "satellite-doc-with-subject",
			ComponentID: satelliteStrPtr("satellite-doc-with-subject#bar"),
			PURL:        satelliteStrPtr("pkg:npm/bar@2.0.0"),
			Name:        satelliteStrPtr("bar"),
			Version:     satelliteStrPtr("2.0.0"),
			Type:        satelliteStrPtr("library"),
		})),
	}
	decisionsWithSubject := sbomattest.BuildSBOMAttestationAttachmentDecisions(withSubject)
	if got, want := len(decisionsWithSubject), 1; got != want {
		t.Fatalf("decisions = %d, want %d for one generator document", got, want)
	}
	withSubjectDecision := decisionsWithSubject[0]
	if got, want := withSubjectDecision.AttachmentStatus, sbomattest.SBOMAttachmentAttachedParseOnly; got != want {
		t.Fatalf("AttachmentStatus = %q, want %q (subject digest present, no verification evidence)", got, want)
	}
	if got, want := withSubjectDecision.SubjectDigest, satelliteSBOMSubjectDigest; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}
	if withSubjectDecision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for unanchored parse-only", withSubjectDecision.CanonicalWrites)
	}
	if got, want := withSubjectDecision.AttachmentScope, "parse_only_unanchored"; got != want {
		t.Fatalf("AttachmentScope = %q, want %q", got, want)
	}
	if got, want := len(withSubjectDecision.MissingEvidence), 2; got != want {
		t.Fatalf("MissingEvidence = %#v, want two missing anchor reasons", withSubjectDecision.MissingEvidence)
	}
	if got, want := withSubjectDecision.ComponentCount, 2; got != want {
		t.Fatalf("ComponentCount = %d, want %d generated components attached", got, want)
	}
	if got, want := withSubjectDecision.Format, "cyclonedx"; got != want {
		t.Fatalf("Format = %q, want %q (satellite output stays CycloneDX-compatible)", got, want)
	}

	missingSubject := []facts.Envelope{
		satelliteSBOMEnvelope(t, chain.SBOMDocumentFactKind, "doc-missing-subject", satelliteSBOMDocumentPayload(t, sbomv1.Document{
			DocumentID:          "satellite-doc-missing-subject",
			Format:              satelliteStrPtr("cyclonedx"),
			SpecVersion:         satelliteStrPtr("1.6"),
			ParseStatus:         satelliteStrPtr("generated"),
			GeneratedByAnalyzer: satelliteStrPtr("sbom_generation"),
			ComponentCount:      satelliteIntPtr(1),
			WarningCount:        satelliteIntPtr(1),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMComponentFactKind, "comp-baz", satelliteSBOMComponentPayload(t, sbomv1.Component{
			DocumentID:  "satellite-doc-missing-subject",
			ComponentID: satelliteStrPtr("satellite-doc-missing-subject#baz"),
			PURL:        satelliteStrPtr("pkg:pypi/baz@9.9.9"),
			Name:        satelliteStrPtr("baz"),
			Version:     satelliteStrPtr("9.9.9"),
			Type:        satelliteStrPtr("library"),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMWarningFactKind, "warn-missing-subject", satelliteSBOMWarningPayload(t, sbomv1.Warning{
			DocumentID: satelliteStrPtr("satellite-doc-missing-subject"),
			Reason:     satelliteStrPtr("missing_subject"),
			Summary:    satelliteStrPtr("sbom generator produced no subject digest"),
		})),
	}
	decisionsMissingSubject := sbomattest.BuildSBOMAttestationAttachmentDecisions(missingSubject)
	if got, want := len(decisionsMissingSubject), 1; got != want {
		t.Fatalf("missing-subject decisions = %d, want %d", got, want)
	}
	missingDecision := decisionsMissingSubject[0]
	if got, want := missingDecision.AttachmentStatus, sbomattest.SBOMAttachmentUnknownSubject; got != want {
		t.Fatalf("missing-subject AttachmentStatus = %q, want %q", got, want)
	}
	if missingDecision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for unknown-subject", missingDecision.CanonicalWrites)
	}
	if len(missingDecision.WarningSummaries) == 0 {
		t.Fatal("WarningSummaries empty, want missing_subject warning surfaced through reducer attachment")
	}

	withLockfileEvidence := []facts.Envelope{
		satelliteSBOMEnvelope(t, chain.SBOMDocumentFactKind, "doc-lockfile", satelliteSBOMDocumentPayload(t, sbomv1.Document{
			DocumentID:          "satellite-doc-lockfile",
			Format:              satelliteStrPtr("cyclonedx"),
			SpecVersion:         satelliteStrPtr("1.6"),
			SubjectDigest:       satelliteStrPtr(satelliteSBOMSubjectDigest),
			SubjectDigests:      []string{satelliteSBOMSubjectDigest},
			ParseStatus:         satelliteStrPtr("generated"),
			GeneratedByAnalyzer: satelliteStrPtr("sbom_generation"),
			ComponentCount:      satelliteIntPtr(1),
			WarningCount:        satelliteIntPtr(1),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMComponentFactKind, "comp-symfony", satelliteSBOMComponentPayload(t, sbomv1.Component{
			DocumentID:       "satellite-doc-lockfile",
			ComponentID:      satelliteStrPtr("satellite-doc-lockfile#symfony"),
			PURL:             satelliteStrPtr("pkg:composer/symfony/console@v7.0.0"),
			Name:             satelliteStrPtr("symfony/console"),
			Version:          satelliteStrPtr("v7.0.0"),
			Type:             satelliteStrPtr("library"),
			Ecosystem:        satelliteStrPtr("composer"),
			EvidenceSource:   satelliteStrPtr("repository_lockfile"),
			LockfilePath:     satelliteStrPtr("services/api/composer.lock"),
			DependencyScope:  satelliteStrPtr("packages"),
			DependencyType:   satelliteStrPtr("runtime"),
			ExtractionReason: satelliteStrPtr("lockfile_exact_version"),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMWarningFactKind, "warn-lockfile", satelliteSBOMWarningPayload(t, sbomv1.Warning{
			DocumentID:       satelliteStrPtr("satellite-doc-lockfile"),
			Reason:           satelliteStrPtr("lockfile_malformed"),
			Summary:          satelliteStrPtr("services/api/packages.lock.json could not be parsed as nuget lockfile evidence: unexpected EOF"),
			Ecosystem:        satelliteStrPtr("nuget"),
			EvidenceSource:   satelliteStrPtr("repository_lockfile"),
			LockfilePath:     satelliteStrPtr("services/api/packages.lock.json"),
			ExtractionReason: satelliteStrPtr("lockfile_malformed"),
		})),
	}
	decisionsWithLockfileEvidence := sbomattest.BuildSBOMAttestationAttachmentDecisions(withLockfileEvidence)
	if got, want := len(decisionsWithLockfileEvidence), 1; got != want {
		t.Fatalf("lockfile-evidence decisions = %d, want %d", got, want)
	}
	lockfileDecision := decisionsWithLockfileEvidence[0]
	if got, want := lockfileDecision.AttachmentStatus, sbomattest.SBOMAttachmentAttachedParseOnly; got != want {
		t.Fatalf("lockfile-evidence AttachmentStatus = %q, want %q", got, want)
	}
	if got, want := len(lockfileDecision.ComponentEvidence), 1; got != want {
		t.Fatalf("ComponentEvidence len = %d, want %d", got, want)
	}
	componentEvidence := lockfileDecision.ComponentEvidence[0]
	if got, want := componentEvidence["ecosystem"], "composer"; got != want {
		t.Fatalf("ComponentEvidence ecosystem = %q, want %q", got, want)
	}
	if got, want := componentEvidence["lockfile_path"], "services/api/composer.lock"; got != want {
		t.Fatalf("ComponentEvidence lockfile_path = %q, want %q", got, want)
	}
	if got, want := componentEvidence["dependency_scope"], "packages"; got != want {
		t.Fatalf("ComponentEvidence dependency_scope = %q, want %q", got, want)
	}
	if got, want := componentEvidence["extraction_reason"], "lockfile_exact_version"; got != want {
		t.Fatalf("ComponentEvidence extraction_reason = %q, want %q", got, want)
	}
	if len(lockfileDecision.WarningSummaries) != 1 {
		t.Fatalf("WarningSummaries = %#v, want one malformed lockfile summary", lockfileDecision.WarningSummaries)
	}
}

func TestSatelliteSBOMAggregatedWarningCountFlowsThroughReducerAttachment(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		satelliteSBOMEnvelope(t, chain.SBOMDocumentFactKind, "doc-aggregate", satelliteSBOMDocumentPayload(t, sbomv1.Document{
			DocumentID:          "satellite-doc-aggregate",
			Format:              satelliteStrPtr("cyclonedx"),
			SpecVersion:         satelliteStrPtr("1.6"),
			SubjectDigest:       satelliteStrPtr(satelliteSBOMSubjectDigest),
			SubjectDigests:      []string{satelliteSBOMSubjectDigest},
			ParseStatus:         satelliteStrPtr("generated"),
			GeneratedByAnalyzer: satelliteStrPtr("sbom_generation"),
			ComponentCount:      satelliteIntPtr(1),
			WarningCount:        satelliteIntPtr(1),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMComponentFactKind, "comp-kept", satelliteSBOMComponentPayload(t, sbomv1.Component{
			DocumentID:  "satellite-doc-aggregate",
			ComponentID: satelliteStrPtr("satellite-doc-aggregate#kept"),
			PURL:        satelliteStrPtr("pkg:npm/kept@1.0.0"),
			Name:        satelliteStrPtr("kept"),
			Version:     satelliteStrPtr("1.0.0"),
			Type:        satelliteStrPtr("library"),
		})),
		satelliteSBOMEnvelope(t, chain.SBOMWarningFactKind, "warn-aggregate", satelliteSBOMWarningPayload(t, sbomv1.Warning{
			DocumentID:      satelliteStrPtr("satellite-doc-aggregate"),
			Reason:          satelliteStrPtr("component_missing_identity"),
			Summary:         satelliteStrPtr("25 components missing purl and name+version identity (samples: component[1], component[2], component[3], component[4], component[5])"),
			OccurrenceCount: satelliteIntPtr(25),
		})),
	}
	decisions := sbomattest.BuildSBOMAttestationAttachmentDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("decisions = %d, want %d for one generator document", got, want)
	}
	decision := decisions[0]
	if got, want := len(decision.WarningSummaries), 1; got != want {
		t.Fatalf("WarningSummaries = %#v, want one aggregate warning", decision.WarningSummaries)
	}
	if got, want := decision.WarningSummaryCount, 25; got != want {
		t.Fatalf("WarningSummaryCount = %d, want %d", got, want)
	}
	if summary := decision.WarningSummaries[0]; !strings.Contains(summary, "25 components missing") {
		t.Fatalf("WarningSummaries[0] = %q, want aggregate count", summary)
	}
}

func satelliteStrPtr(value string) *string {
	return &value
}

func satelliteIntPtr(value int) *int {
	return &value
}
