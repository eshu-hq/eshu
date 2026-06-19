package sbomdocument

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCycloneDXFixtureBuildsReducerConsumableFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cyclonedx_image_subject.json")
	observedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	envelopes, err := CycloneDXFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "sbom://registry.example.com/library/example@sha256:1111",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-cyclonedx",
		FencingToken:        42,
		ObservedAt:          observedAt,
		SourceURI:           "https://example.com/sboms/cyclonedx_image_subject.json",
		SourceRecordID:      "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
	})
	if err != nil {
		t.Fatalf("CycloneDXFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)

	docs := byKind[facts.SBOMDocumentFactKind]
	if len(docs) != 1 {
		t.Fatalf("document facts = %d, want 1", len(docs))
	}
	doc := docs[0]
	assertCommonEnvelope(t, doc, observedAt)
	assertPayload(t, doc.Payload, "format", string(FormatCycloneDX))
	assertPayload(t, doc.Payload, "source_format", string(SourceFormatJSON))
	assertPayload(t, doc.Payload, "spec_version", "1.5")
	assertPayload(t, doc.Payload, "subject_digest", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	assertPayload(t, doc.Payload, "parse_status", string(ParseStatusParsed))
	assertPayload(t, doc.Payload, "verification_status", "")
	assertPayload(t, doc.Payload, "verification_policy", "")
	assertPayload(t, doc.Payload, "serial_number", "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79")
	assertPayload(t, doc.Payload, "document_name", "registry.example.com/library/example")
	if got := doc.Payload["component_count"]; got != 5 {
		t.Fatalf("component_count = %v, want 5 (subject + 4 listed)", got)
	}

	components := byKind[facts.SBOMComponentFactKind]
	if len(components) != 5 {
		t.Fatalf("component facts = %d, want 5 (subject + 3 unique + 1 duplicate)", len(components))
	}
	componentByPURL := indexComponentsByPURL(components)
	lodash, ok := componentByPURL["pkg:npm/lodash@4.17.21"]
	if !ok {
		t.Fatalf("expected lodash component fact, got %#v", componentByPURL)
	}
	assertPayload(t, lodash.Payload, "name", "lodash")
	assertPayload(t, lodash.Payload, "version", "4.17.21")
	// package_id carries the canonical package identity so the component
	// correlates with vulnerability and package-registry facts on the same key.
	assertPayload(t, lodash.Payload, "package_id", "npm://registry.npmjs.org/lodash")
	assertPayload(t, lodash.Payload, "type", "library")
	assertPayload(t, lodash.Payload, "supplier_name", "OpenJS Foundation")
	assertPayload(t, lodash.Payload, "supplier_url", "https://openjsf.org/")
	if dup := lodash.Payload["is_duplicate"].(bool); dup {
		t.Fatalf("canonical lodash should not be marked duplicate")
	}
	if hashes := payloadHashes(lodash.Payload); len(hashes) == 0 {
		t.Fatalf("expected lodash hashes, got 0")
	}
	if licenses := payloadLicenses(lodash.Payload); len(licenses) == 0 || licenses[0]["id"] != "MIT" {
		t.Fatalf("expected lodash MIT license, got %#v", licenses)
	}

	// dependency edges
	deps := byKind[facts.SBOMDependencyRelationshipFactKind]
	if len(deps) != 3 {
		t.Fatalf("dependency facts = %d, want 3", len(deps))
	}

	// external references for lodash vcs
	refs := byKind[facts.SBOMExternalReferenceFactKind]
	if len(refs) == 0 {
		t.Fatalf("expected at least one external reference fact")
	}

	// warnings: duplicate lodash + unsupported vulnerabilities + component-missing-identity
	warnings := byKind[facts.SBOMWarningFactKind]
	reasons := warningReasons(warnings)
	expect := []string{
		string(WarningReasonDuplicateComponent),
		string(WarningReasonUnsupportedField),
		string(WarningReasonComponentMissingIdentity),
	}
	for _, want := range expect {
		if !containsString(reasons, want) {
			t.Fatalf("warning reasons missing %q in %#v", want, reasons)
		}
	}

	// every fact must carry the collector boundary
	for _, envelope := range envelopes {
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
		}
		if envelope.ScopeID != "sbom://registry.example.com/library/example@sha256:1111" {
			t.Fatalf("ScopeID = %q, want fixture scope", envelope.ScopeID)
		}
		if envelope.GenerationID != "gen-1" {
			t.Fatalf("GenerationID = %q, want fixture generation", envelope.GenerationID)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
		}
		if envelope.FactID == "" || envelope.StableFactKey == "" {
			t.Fatalf("fact identifiers must not be blank: %#v", envelope)
		}
	}
}

func TestCycloneDXFixtureMissingSubjectEmitsWarning(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cyclonedx_missing_subject.json")
	envelopes, err := CycloneDXFixtureEnvelopes(raw, validFixtureContext())
	if err != nil {
		t.Fatalf("CycloneDXFixtureEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)
	doc := byKind[facts.SBOMDocumentFactKind][0]
	if got := doc.Payload["subject_digest"]; got != "" {
		t.Fatalf("subject_digest = %q, want empty for missing-subject document", got)
	}
	if !containsString(warningReasons(byKind[facts.SBOMWarningFactKind]), string(WarningReasonMissingSubject)) {
		t.Fatalf("expected missing_subject warning, got %#v", byKind[facts.SBOMWarningFactKind])
	}
}

func TestCycloneDXFixtureMalformedEmitsUnparseableDocument(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cyclonedx_malformed.json")
	envelopes, err := CycloneDXFixtureEnvelopes(raw, validFixtureContext())
	if err != nil {
		t.Fatalf("CycloneDXFixtureEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)
	if len(byKind[facts.SBOMDocumentFactKind]) != 1 {
		t.Fatalf("malformed document should emit one sbom.document fact, got %d", len(byKind[facts.SBOMDocumentFactKind]))
	}
	doc := byKind[facts.SBOMDocumentFactKind][0]
	if got := doc.Payload["parse_status"]; got != string(ParseStatusMalformed) {
		t.Fatalf("parse_status = %v, want %q", got, ParseStatusMalformed)
	}
	if got := doc.Payload["subject_digest"]; got != "" {
		t.Fatalf("subject_digest = %q, want empty for malformed document", got)
	}
	if !containsString(warningReasons(byKind[facts.SBOMWarningFactKind]), string(WarningReasonMalformedDocument)) {
		t.Fatalf("expected malformed_document warning, got %#v", byKind[facts.SBOMWarningFactKind])
	}
}

func TestCycloneDXFixtureValidatesBoundary(t *testing.T) {
	t.Parallel()
	cases := map[string]FixtureContext{
		"missing scope":      {GenerationID: "g", CollectorInstanceID: "c"},
		"missing generation": {ScopeID: "s", CollectorInstanceID: "c"},
		"missing collector":  {ScopeID: "s", GenerationID: "g"},
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := CycloneDXFixtureEnvelopes([]byte("{}"), ctx); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return raw
}

func envelopesByKind(envelopes []facts.Envelope) map[string][]facts.Envelope {
	out := map[string][]facts.Envelope{}
	for _, envelope := range envelopes {
		out[envelope.FactKind] = append(out[envelope.FactKind], envelope)
	}
	return out
}

func assertCommonEnvelope(t *testing.T, envelope facts.Envelope, observedAt time.Time) {
	t.Helper()
	if envelope.FactKind != facts.SBOMDocumentFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.SBOMDocumentFactKind)
	}
	if envelope.SchemaVersion != facts.SBOMAttestationSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.SBOMAttestationSchemaVersionV1)
	}
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %v, want %v", envelope.ObservedAt, observedAt)
	}
}

func assertPayload(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()
	got, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing key %q", key)
	}
	if got != want {
		t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
	}
}

func indexComponentsByPURL(components []facts.Envelope) map[string]facts.Envelope {
	out := map[string]facts.Envelope{}
	for _, c := range components {
		purl, _ := c.Payload["purl"].(string)
		if purl == "" || c.Payload["is_duplicate"].(bool) {
			continue
		}
		out[purl] = c
	}
	return out
}

func payloadHashes(payload map[string]any) []map[string]string {
	switch typed := payload["hashes"].(type) {
	case []map[string]string:
		return typed
	default:
		return nil
	}
}

func payloadLicenses(payload map[string]any) []map[string]string {
	switch typed := payload["licenses"].(type) {
	case []map[string]string:
		return typed
	default:
		return nil
	}
}

func warningReasons(warnings []facts.Envelope) []string {
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, strings.TrimSpace(toString(w.Payload["reason"])))
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func validFixtureContext() FixtureContext {
	return FixtureContext{
		ScopeID:             "sbom://test",
		GenerationID:        "gen-test",
		CollectorInstanceID: "fixture",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
	}
}
