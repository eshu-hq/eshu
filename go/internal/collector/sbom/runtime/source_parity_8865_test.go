// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Package runtime parity pin for #8865 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree claimed-source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, collector kinds, and payload bytes as the recorded fixture
// (testdata/8865-parity-sbom-attestation.json, cut from a live run with the
// same fixed clock). The collector is claim-driven, so every scope is
// collected through ClaimedSource.NextClaimed with a fixed work item.
// Regenerate with UPDATE_8865_PARITY=1, then rerun normally to verify the
// committed bytes. Grant enforcement and durable commit/lease semantics are
// core-owned and out of scope (see EXTRACTION_PREP.md).

const parity8865FixturePath = "testdata/8865-parity-sbom-attestation.json"

const (
	parity8865InstanceID   = "sbom-parity-8865"
	parity8865FencingToken = int64(7)
)

var parity8865ObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// parity8865Provider serves one fixed document per scope so every collection
// is byte-deterministic under the fixed clock.
type parity8865Provider struct {
	docs map[string]Document
}

func (p *parity8865Provider) FetchDocument(_ context.Context, target TargetConfig) (Document, error) {
	doc, ok := p.docs[target.ScopeID]
	if !ok {
		return Document{}, fmt.Errorf("parity provider has no document for scope %q", target.ScopeID)
	}
	return doc, nil
}

type parity8865Case struct {
	target       TargetConfig
	generationID string
	body         []byte
	sourceURI    string
}

// parity8865Plan rebuilds the exact inputs the fixture was cut from, so
// stable keys and payload bytes reproduce deterministically.
func parity8865Plan(t *testing.T) []parity8865Case {
	t.Helper()

	slsa := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.test/library/parity",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": {
			"runDetails": {
				"builder": {"id": "https://github.com/actions/runner/v1"}
			}
		}
	}`)
	return []parity8865Case{
		{
			target: TargetConfig{
				ScopeID:        "sbom-attestation://parity/cyclonedx-configured",
				SourceType:     SourceTypeConfigured,
				ArtifactKind:   ArtifactKindSBOM,
				DocumentFormat: DocumentFormatCycloneDX,
				DocumentURL:    "https://user:secret@sbom.example.test/private/demo-cyclonedx.json?token=secret#frag",
			},
			generationID: "sbom-attestation:parity-gen-8865-1",
			body:         readFixture(t, "../document/testdata/cyclonedx_image_subject.json"),
			sourceURI:    "https://user:secret@sbom.example.test/private/demo-cyclonedx.json?token=secret#frag",
		},
		{
			target: TargetConfig{
				ScopeID:        "sbom-attestation://parity/spdx-referrer",
				SourceType:     SourceTypeOCIReferrer,
				ArtifactKind:   ArtifactKindSBOM,
				DocumentFormat: DocumentFormatSPDX,
				Provider:       "oci",
				Registry:       "https://registry.example.test",
				Repository:     "library/parity",
				SubjectDigest:  testSubjectDigest,
				ReferrerDigest: testReferrerDigest,
			},
			generationID: "sbom-attestation:parity-gen-8865-2",
			body:         readFixture(t, "../document/testdata/spdx_image_subject.json"),
		},
		{
			target: TargetConfig{
				ScopeID:            "sbom-attestation://parity/slsa-attestation",
				SourceType:         SourceTypeOCIReferrer,
				ArtifactKind:       ArtifactKindAttestation,
				DocumentFormat:     DocumentFormatInToto,
				Provider:           "oci",
				Registry:           "https://registry.example.test",
				Repository:         "library/parity",
				SubjectDigest:      testSubjectDigest,
				ReferrerDigest:     testReferrerDigest,
				VerificationResult: "passed",
				VerificationPolicy: "cosign-keyless",
			},
			generationID: "sbom-attestation:parity-gen-8865-3",
			body:         slsa,
		},
		{
			target: TargetConfig{
				ScopeID:        "sbom-attestation://parity/malformed",
				SourceType:     SourceTypeConfigured,
				ArtifactKind:   ArtifactKindSBOM,
				DocumentFormat: DocumentFormatCycloneDX,
				DocumentURL:    "https://sbom.example.test/private/malformed.json",
			},
			generationID: "sbom-attestation:parity-gen-8865-4",
			body:         readFixture(t, "../document/testdata/cyclonedx_malformed.json"),
			sourceURI:    "https://sbom.example.test/private/malformed.json",
		},
	}
}

// parity8865Source builds one claimed source over the plan with the fixed
// clock, mirroring service.go minus the ECR provider.
func parity8865Source(t *testing.T, plan []parity8865Case) *ClaimedSource {
	t.Helper()

	targets := make([]TargetConfig, 0, len(plan))
	docs := make(map[string]Document, len(plan))
	for _, c := range plan {
		targets = append(targets, c.target)
		docs[c.target.ScopeID] = Document{
			Body:       c.body,
			SourceURI:  c.sourceURI,
			ObservedAt: parity8865ObservedAt,
		}
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: parity8865InstanceID,
		Targets:             targets,
		Provider:            &parity8865Provider{docs: docs},
		Now:                 func() time.Time { return parity8865ObservedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	return source
}

func parity8865Collect(t *testing.T, source *ClaimedSource, c parity8865Case) collector.CollectedGeneration {
	t.Helper()

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorSBOMAttestation,
		CollectorInstanceID: parity8865InstanceID,
		ScopeID:             c.target.ScopeID,
		GenerationID:        c.generationID,
		CurrentFencingToken: parity8865FencingToken,
	})
	if err != nil {
		t.Fatalf("NextClaimed(%q) error = %v", c.target.ScopeID, err)
	}
	if !ok {
		t.Fatalf("NextClaimed(%q) ok = false, want true", c.target.ScopeID)
	}
	return collected
}

func parity8865Drain(collected collector.CollectedGeneration) []facts.Envelope {
	var envelopes []facts.Envelope
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	slices.SortFunc(envelopes, func(a, b facts.Envelope) int {
		return strings.Compare(a.StableFactKey, b.StableFactKey)
	})
	return envelopes
}

func parity8865KindMultiset(envelopes []facts.Envelope) map[string]int {
	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func parity8865StableKeys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	slices.Sort(keys)
	return keys
}

func parity8865CanonicalJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

// parity8865AttrKey binds the host-enforced boundary attributes to the stable
// key. Payload bytes compare separately, so counts survive the int/float64
// JSON round-trip instead of failing on Go type identity.
func parity8865AttrKey(envelope facts.Envelope) string {
	return strings.Join([]string{
		envelope.StableFactKey,
		envelope.FactKind,
		envelope.SchemaVersion,
		envelope.CollectorKind,
		string(envelope.SourceConfidence),
		envelope.SourceRef.SourceURI,
		envelope.SourceRef.SourceRecordID,
	}, "\x00")
}

type parity8865RecordedEnvelope struct {
	FactKind         string         `json:"fact_kind"`
	StableKey        string         `json:"stable_key"`
	SchemaVersion    string         `json:"schema_version"`
	CollectorKind    string         `json:"collector_kind"`
	FencingToken     int64          `json:"fencing_token"`
	SourceConfidence string         `json:"source_confidence"`
	SourceURI        string         `json:"source_uri"`
	SourceRecordID   string         `json:"source_record_id"`
	Payload          map[string]any `json:"payload"`
}

type parity8865RecordedScope struct {
	ScopeID      string                       `json:"scope_id"`
	GenerationID string                       `json:"generation_id"`
	Envelopes    []parity8865RecordedEnvelope `json:"envelopes"`
}

type parity8865Fixture struct {
	Scopes []parity8865RecordedScope `json:"scopes"`
}

func parity8865Record(collected collector.CollectedGeneration) parity8865RecordedScope {
	envelopes := parity8865Drain(collected)
	recorded := make([]parity8865RecordedEnvelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		recorded = append(recorded, parity8865RecordedEnvelope{
			FactKind:         envelope.FactKind,
			StableKey:        envelope.StableFactKey,
			SchemaVersion:    envelope.SchemaVersion,
			CollectorKind:    envelope.CollectorKind,
			FencingToken:     envelope.FencingToken,
			SourceConfidence: string(envelope.SourceConfidence),
			SourceURI:        envelope.SourceRef.SourceURI,
			SourceRecordID:   envelope.SourceRef.SourceRecordID,
			Payload:          envelope.Payload,
		})
	}
	return parity8865RecordedScope{
		ScopeID:      collected.Scope.ScopeID,
		GenerationID: collected.Generation.GenerationID,
		Envelopes:    recorded,
	}
}

func parity8865LoadFixture(t *testing.T) parity8865Fixture {
	t.Helper()

	raw, err := os.ReadFile(parity8865FixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v (regenerate with UPDATE_8865_PARITY=1)", parity8865FixturePath, err)
	}
	var fixture parity8865Fixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode fixture %s: %v", parity8865FixturePath, err)
	}
	return fixture
}

// TestSBOM8865ParityLiveAgreesWithFixture is the AC3 source-parity pin:
// live collection and fixture agree per scope on IDs, kinds, stable keys,
// boundary attributes, and payload bytes.
func TestSBOM8865ParityLiveAgreesWithFixture(t *testing.T) {
	t.Parallel()

	if os.Getenv("UPDATE_8865_PARITY") != "" {
		plan := parity8865Plan(t)
		source := parity8865Source(t, plan)
		fixture := parity8865Fixture{}
		for _, c := range plan {
			fixture.Scopes = append(fixture.Scopes, parity8865Record(parity8865Collect(t, source, c)))
		}
		raw, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if err := os.WriteFile(parity8865FixturePath, append(raw, '\n'), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("rewrote %s from live output; rerun without UPDATE_8865_PARITY=1", parity8865FixturePath)
		return
	}

	fixture := parity8865LoadFixture(t)
	plan := parity8865Plan(t)
	if len(fixture.Scopes) != len(plan) {
		t.Fatalf("fixture scopes = %d, want %d", len(fixture.Scopes), len(plan))
	}

	source := parity8865Source(t, plan)
	for i, c := range plan {
		expected := fixture.Scopes[i]
		if expected.ScopeID != c.target.ScopeID {
			t.Fatalf("fixture scope[%d] = %q, want %q", i, expected.ScopeID, c.target.ScopeID)
		}
		collected := parity8865Collect(t, source, c)
		if collected.Scope.ScopeID != expected.ScopeID {
			t.Fatalf("live ScopeID = %q, want %q", collected.Scope.ScopeID, expected.ScopeID)
		}
		if collected.Generation.GenerationID != expected.GenerationID {
			t.Fatalf("live GenerationID = %q, want %q", collected.Generation.GenerationID, expected.GenerationID)
		}
		envelopes := parity8865Drain(collected)
		if len(envelopes) != len(expected.Envelopes) {
			t.Fatalf("live envelopes = %d, want %d for scope %q", len(envelopes), len(expected.Envelopes), expected.ScopeID)
		}
		wantKinds := map[string]int{}
		wantKeys := make([]string, 0, len(expected.Envelopes))
		for _, recorded := range expected.Envelopes {
			wantKinds[recorded.FactKind]++
			wantKeys = append(wantKeys, recorded.StableKey)
		}
		slices.Sort(wantKeys)
		if got := parity8865KindMultiset(envelopes); !maps.Equal(got, wantKinds) {
			t.Fatalf("live kinds = %v, want %v for scope %q", got, wantKinds, expected.ScopeID)
		}
		if got := parity8865StableKeys(envelopes); !slices.Equal(got, wantKeys) {
			t.Fatalf("live stable keys differ from fixture for scope %q", expected.ScopeID)
		}
		for j, envelope := range envelopes {
			recorded := expected.Envelopes[j]
			wantAttr := strings.Join([]string{
				recorded.StableKey, recorded.FactKind, recorded.SchemaVersion,
				recorded.CollectorKind, recorded.SourceConfidence,
				recorded.SourceURI, recorded.SourceRecordID,
			}, "\x00")
			if got := parity8865AttrKey(envelope); got != wantAttr {
				t.Fatalf("live boundary attributes differ from fixture for scope %q key %q", expected.ScopeID, envelope.StableFactKey)
			}
			if envelope.FencingToken != parity8865FencingToken {
				t.Fatalf("FencingToken = %d, want %d", envelope.FencingToken, parity8865FencingToken)
			}
			if envelope.SchemaVersion != chain.SBOMAttestationSchemaVersionV1 {
				t.Fatalf("SchemaVersion = %q, want %q for kind %q", envelope.SchemaVersion, chain.SBOMAttestationSchemaVersionV1, envelope.FactKind)
			}
			if got, want := parity8865CanonicalJSON(t, envelope.Payload), parity8865CanonicalJSON(t, recorded.Payload); !bytes.Equal(got, want) {
				t.Fatalf("live payload bytes differ from fixture for scope %q kind %q key %q", expected.ScopeID, envelope.FactKind, envelope.StableFactKey)
			}
		}
	}

	union := map[string]bool{}
	for _, s := range fixture.Scopes {
		for _, envelope := range s.Envelopes {
			union[envelope.FactKind] = true
		}
	}
	for _, kind := range chain.SBOMAttestationFactKinds() {
		if !union[kind] {
			t.Fatalf("fixture covers no %q fact; parity pack must span all 8 live kinds", kind)
		}
	}
}

// TestSBOM8865ParityDuplicateDeliveryIsStable pins duplicate safety: two
// identical claims per scope emit identical stable-key sets and payload
// bytes, so a redelivered generation commits idempotently at the core
// boundary. Every plan scope is pinned, since ROLLBACK_DRAFT.md step 5 cites
// this test for general redelivery safety.
func TestSBOM8865ParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	plan := parity8865Plan(t)
	source := parity8865Source(t, plan)
	for _, c := range plan {
		first := parity8865Drain(parity8865Collect(t, source, c))
		second := parity8865Drain(parity8865Collect(t, source, c))
		if !slices.Equal(parity8865StableKeys(first), parity8865StableKeys(second)) {
			t.Fatalf("duplicate claims emit different stable keys for scope %q; redelivery would not be idempotent", c.target.ScopeID)
		}
		for i := range first {
			if got, want := parity8865CanonicalJSON(t, first[i].Payload), parity8865CanonicalJSON(t, second[i].Payload); !bytes.Equal(got, want) {
				t.Fatalf("duplicate claims emit different payload bytes for scope %q key %q", c.target.ScopeID, first[i].StableFactKey)
			}
		}
	}
}

// TestSBOM8865ParityHostileInputsPin pins the hostile half: malformed
// documents, credential redaction, blank statement status, no OCI facts on
// any scope, the record-ID channel, and the document/attestation
// collector-kind boundary.
func TestSBOM8865ParityHostileInputsPin(t *testing.T) {
	t.Parallel()

	plan := parity8865Plan(t)
	source := parity8865Source(t, plan)

	assertNoOCIReferrerFacts := func(scope string, envelopes []facts.Envelope) {
		t.Helper()
		for i := range envelopes {
			if envelopes[i].FactKind == chain.OCIImageReferrerFactKind {
				t.Fatalf("runtime emitted OCI referrer fact %q in scope %s; OCI collector owns those facts", envelopes[i].FactID, scope)
			}
		}
	}

	malformed := parity8865Drain(parity8865Collect(t, source, plan[3]))
	var doc, warning *facts.Envelope
	for i := range malformed {
		switch malformed[i].FactKind {
		case chain.SBOMDocumentFactKind:
			doc = &malformed[i]
		case chain.SBOMWarningFactKind:
			warning = &malformed[i]
		}
	}
	assertNoOCIReferrerFacts("malformed", malformed)
	if doc == nil {
		t.Fatal("malformed scope emitted no sbom.document envelope")
	}
	if warning == nil {
		t.Fatal("malformed scope emitted no sbom.warning envelope")
	}
	if got := payloadString(doc.Payload, "parse_status"); got != "malformed" {
		t.Fatalf("parse_status = %q, want malformed", got)
	}
	if got := payloadString(warning.Payload, "reason"); got != "malformed_document" {
		t.Fatalf("warning reason = %q, want malformed_document", got)
	}

	configured := parity8865Drain(parity8865Collect(t, source, plan[0]))
	assertNoOCIReferrerFacts("configured", configured)
	for _, envelope := range configured {
		for _, locator := range []string{envelope.SourceRef.SourceURI, payloadString(envelope.Payload, "source_uri")} {
			if strings.Contains(locator, "secret") || strings.Contains(locator, "token=") {
				t.Fatalf("source locator leaked sensitive material: %q", locator)
			}
		}
		if envelope.CollectorKind != "sbom_document" {
			t.Fatalf("document-path CollectorKind = %q, want sbom_document", envelope.CollectorKind)
		}
		// OPEN CUTOVER #4047: SourceRecordID is carried verbatim —
		// source.go:141 builds it raw from DocumentURL while source.go:140
		// redacts SourceURI via safeSourceURI. The sbom.document envelope
		// proves the channel: its record ID is the raw credential-bearing
		// URL. The extracted producer must redact-or-refuse
		// credential-bearing record IDs; in-tree bytes stay frozen for
		// cutover comparability. This pin fails if the channel changes
		// shape or goes blank without updating the cutover item.
		if envelope.FactKind == chain.SBOMDocumentFactKind {
			recordID := envelope.SourceRef.SourceRecordID
			if !strings.Contains(recordID, "user:secret") || !strings.Contains(recordID, "token=") {
				t.Fatalf("sbom.document SourceRecordID = %q, want verbatim credential-bearing carry-through until cutover closes it", recordID)
			}
		}
	}

	referrer := parity8865Drain(parity8865Collect(t, source, plan[1]))
	assertNoOCIReferrerFacts("spdx-referrer", referrer)

	attested := parity8865Drain(parity8865Collect(t, source, plan[2]))
	assertNoOCIReferrerFacts("attested", attested)
	for _, envelope := range attested {
		if envelope.CollectorKind != string(scope.CollectorSBOMAttestation) {
			t.Fatalf("attestation-path CollectorKind = %q, want %q", envelope.CollectorKind, scope.CollectorSBOMAttestation)
		}
		if envelope.FactKind == chain.AttestationStatementFactKind {
			if got := payloadString(envelope.Payload, "verification_status"); got != "" {
				t.Fatalf("statement verification_status = %q, want parser-owned blank status", got)
			}
		}
	}
}

// TestSBOM8865ParityInTreeEmissionCarriesEnforcementKeys pins the delegation
// delta's in-tree half: every emitted envelope carries a non-blank
// collector kind and the fencing token, the keys future grant enforcement
// will key on. Fail-closed behavior of the extracted producer is NOT pinned
// here (no grant mechanism exists in-tree); it is an open cutover item.
func TestSBOM8865ParityInTreeEmissionCarriesEnforcementKeys(t *testing.T) {
	t.Parallel()

	plan := parity8865Plan(t)
	source := parity8865Source(t, plan)
	for _, c := range plan {
		for _, envelope := range parity8865Drain(parity8865Collect(t, source, c)) {
			if envelope.CollectorKind == "" {
				t.Fatalf("CollectorKind is blank for kind %q; grant enforcement has nothing to key on", envelope.FactKind)
			}
			if envelope.FencingToken != parity8865FencingToken {
				t.Fatalf("FencingToken = %d, want %d", envelope.FencingToken, parity8865FencingToken)
			}
		}
	}
}
