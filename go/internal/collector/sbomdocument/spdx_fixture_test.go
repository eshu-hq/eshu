// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSPDXFixtureBuildsReducerConsumableFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/spdx_image_subject.json")
	observedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	envelopes, err := SPDXFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "sbom://registry.example.com/library/example@sha256:1111",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-spdx",
		FencingToken:        43,
		ObservedAt:          observedAt,
		SourceURI:           "https://example.com/sboms/spdx_image_subject.json",
		SourceRecordID:      "https://example.com/spdx/example-1.2.3-7d83",
	})
	if err != nil {
		t.Fatalf("SPDXFixtureEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	docs := byKind[facts.SBOMDocumentFactKind]
	if len(docs) != 1 {
		t.Fatalf("document facts = %d, want 1", len(docs))
	}
	doc := docs[0]
	assertCommonEnvelope(t, doc, observedAt)
	assertPayload(t, doc.Payload, "format", string(FormatSPDX))
	assertPayload(t, doc.Payload, "spec_version", "SPDX-2.3")
	assertPayload(t, doc.Payload, "document_namespace", "https://example.com/spdx/example-1.2.3-7d83")
	assertPayload(t, doc.Payload, "parse_status", string(ParseStatusParsed))
	assertPayload(t, doc.Payload, "subject_digest", "sha256:1111111111111111111111111111111111111111111111111111111111111111")

	components := byKind[facts.SBOMComponentFactKind]
	// Expect 5 components: Image, lodash, lodash-dup, requests, alpine (noident dropped).
	if len(components) != 5 {
		t.Fatalf("component facts = %d, want 5", len(components))
	}
	componentByPURL := indexComponentsByPURL(components)
	requests, ok := componentByPURL["pkg:pypi/requests@2.31.0"]
	if !ok {
		t.Fatalf("expected requests component, got %#v", componentByPURL)
	}
	assertPayload(t, requests.Payload, "cpe", "cpe:2.3:a:python-requests:requests:2.31.0:*:*:*:*:*:*:*")

	deps := byKind[facts.SBOMDependencyRelationshipFactKind]
	// CONTAINS lodash, CONTAINS requests, DEPENDS_ON requests → 3 edges.
	if len(deps) != 3 {
		t.Fatalf("dependency facts = %d, want 3", len(deps))
	}

	refs := byKind[facts.SBOMExternalReferenceFactKind]
	if len(refs) == 0 {
		t.Fatalf("expected external reference facts")
	}

	warnings := byKind[facts.SBOMWarningFactKind]
	reasons := warningReasons(warnings)
	wantReasons := []string{
		string(WarningReasonDuplicateComponent),
		string(WarningReasonComponentMissingIdentity),
	}
	for _, want := range wantReasons {
		if !containsString(reasons, want) {
			t.Fatalf("warning reasons missing %q in %#v", want, reasons)
		}
	}
}

func TestSPDXFixtureMissingSubjectEmitsWarning(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/spdx_missing_subject.json")
	envelopes, err := SPDXFixtureEnvelopes(raw, validFixtureContext())
	if err != nil {
		t.Fatalf("SPDXFixtureEnvelopes() error = %v", err)
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

func TestSPDXFixtureMalformedEmitsUnparseableDocument(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/spdx_malformed.json")
	envelopes, err := SPDXFixtureEnvelopes(raw, validFixtureContext())
	if err != nil {
		t.Fatalf("SPDXFixtureEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)
	if len(byKind[facts.SBOMDocumentFactKind]) != 1 {
		t.Fatalf("malformed spdx should emit one document fact, got %d", len(byKind[facts.SBOMDocumentFactKind]))
	}
	if got := byKind[facts.SBOMDocumentFactKind][0].Payload["parse_status"]; got != string(ParseStatusMalformed) {
		t.Fatalf("parse_status = %v, want %q", got, ParseStatusMalformed)
	}
	if !containsString(warningReasons(byKind[facts.SBOMWarningFactKind]), string(WarningReasonMalformedDocument)) {
		t.Fatalf("expected malformed_document warning, got %#v", byKind[facts.SBOMWarningFactKind])
	}
}

func TestSPDXFixtureValidatesBoundary(t *testing.T) {
	t.Parallel()
	cases := map[string]FixtureContext{
		"missing scope":      {GenerationID: "g", CollectorInstanceID: "c"},
		"missing generation": {ScopeID: "s", CollectorInstanceID: "c"},
		"missing collector":  {ScopeID: "s", GenerationID: "g"},
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := SPDXFixtureEnvelopes([]byte("{}"), ctx); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}
