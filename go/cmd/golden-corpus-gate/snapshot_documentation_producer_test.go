// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	clidocs "github.com/eshu-hq/eshu/go/internal/cli/docs"
)

// TestGoldenDocumentationPacketRequiresProducerProof guards the readback claim:
// a hand-authored packet without source citation and verifier provenance must
// fail the golden MCP shape, even when its identity and freshness fields exist.
func TestGoldenDocumentationPacketRequiresProducerProof(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["get_documentation_evidence_packet"]
	if !shape.Envelope {
		t.Fatal("packet MCP shape must preserve the semantic truth envelope")
	}
	for path, want := range map[string]any{
		"data.unified_evidence.provenance.source":    "documentation_verifier",
		"data.unified_evidence.provenance.basis":     "derived",
		"data.unified_evidence.citation.byte_offset": float64(6),
		"data.unified_evidence.citation.byte_length": float64(34),
		"data.bounded_excerpt.text":                  "GET /api/v0/documentation/findings",
		"data.current_truth.truth_level":             "exact",
		"truth.basis":                                "semantic_facts",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Errorf("required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
}

func TestGoldenDocumentationPacketShapeRejectsUnprovenReadback(t *testing.T) {
	t.Parallel()

	document := filepath.Join("..", "..", "..", "testdata", "golden", "documentation-positive.md")
	result, err := clidocs.Verify(context.Background(), clidocs.VerifyOptions{
		Path: document, Limit: 1, MaxDocumentBytes: 256 * 1024, ImageTruth: "local",
	}, clidocs.Deps{})
	if err != nil {
		t.Fatalf("verify public documentation fixture: %v", err)
	}
	if len(result.Verification.Findings) != 1 || len(result.Verification.EvidencePackets) != 1 {
		t.Fatalf("verifier emitted %d findings and %d packets, want one each",
			len(result.Verification.Findings), len(result.Verification.EvidencePackets))
	}
	finding := result.Verification.Findings[0]
	packet := result.Verification.EvidencePackets[0]
	if finding.Status != "valid" || finding.ClaimType != "http_endpoint" {
		t.Fatalf("verifier finding = %+v, want valid HTTP endpoint", finding)
	}

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["get_documentation_evidence_packet"]
	documentPayload, err := goldenProducerDocument(packet.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{
		"data.packet_id":                           packet.PacketID,
		"data.packet_version":                      packet.PacketVersion,
		"data.finding_id":                          finding.FindingID,
		"data.document.document_id":                finding.DocumentID,
		"data.document.canonical_uri":              documentPayload["canonical_uri"],
		"data.section.section_id":                  finding.SectionID,
		"data.unified_evidence.citation.entity_id": finding.DocumentID,
		"data.evidence_refs[].source_uri":          documentPayload["canonical_uri"],
	} {
		shape.RequiredJSONValues[key] = value
	}
	envelope := map[string]any{
		"data":  packet.Payload,
		"truth": map[string]any{"basis": "semantic_facts", "freshness": map[string]any{"state": "fresh"}},
		"error": nil,
	}
	assertShape := func(name string, candidate map[string]any, wantOK bool) {
		t.Helper()
		body, err := json.Marshal(candidate)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		got := EvaluateQueryShape(name, shape, body)
		if got.OK != wantOK || !got.Required {
			t.Errorf("%s: finding = %+v, want OK=%t and required", name, got, wantOK)
		}
	}
	assertShape("verifier packet", envelope, true)

	clone := func() map[string]any {
		t.Helper()
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var copied map[string]any
		if err := json.Unmarshal(body, &copied); err != nil {
			t.Fatal(err)
		}
		return copied
	}
	oldHandPacket := clone()
	delete(oldHandPacket["data"].(map[string]any), "unified_evidence")
	assertShape("old hand packet without provenance", oldHandPacket, false)
	wrongCitation := clone()
	wrongCitation["data"].(map[string]any)["unified_evidence"].(map[string]any)["citation"].(map[string]any)["byte_offset"] = float64(9)
	assertShape("wrong source byte window", wrongCitation, false)
	wrongVersion := clone()
	wrongVersion["data"].(map[string]any)["packet_version"] = "stale-version"
	assertShape("wrong packet version", wrongVersion, false)
	wrongTruth := clone()
	wrongTruth["truth"].(map[string]any)["basis"] = "assertion"
	assertShape("wrong response truth", wrongTruth, false)
}

func TestGoldenProducerDocument(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		payload map[string]any
		wantOK  bool
	}{
		{name: "missing"},
		{name: "null", payload: map[string]any{"document": nil}},
		{name: "scalar", payload: map[string]any{"document": "invalid"}},
		{name: "array", payload: map[string]any{"document": []any{}}},
		{name: "typed nil", payload: map[string]any{"document": map[string]any(nil)}},
		{name: "object", payload: map[string]any{"document": map[string]any{"canonical_uri": "fixture"}}, wantOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := goldenProducerDocument(test.payload)
			if (err == nil) != test.wantOK {
				t.Fatalf("goldenProducerDocument() error = %v, want success=%t", err, test.wantOK)
			}
			if test.wantOK && document["canonical_uri"] != "fixture" {
				t.Fatalf("document = %#v, want original object", document)
			}
		})
	}
}

// goldenProducerDocument checks the producer's object before tests bind its URI.
func goldenProducerDocument(payload map[string]any) (map[string]any, error) {
	document, ok := payload["document"].(map[string]any)
	if !ok || document == nil {
		return nil, fmt.Errorf("verifier packet document = %T, want non-nil object", payload["document"])
	}
	return document, nil
}
