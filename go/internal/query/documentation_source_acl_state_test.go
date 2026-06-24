// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"testing"
)

// TestContentReaderDocumentationFindingsSurfacesBoundedSourceACLState proves the
// bounded source_acl_state on a finding's acl_summary is surfaced verbatim as a
// distinct top-level access-posture axis (#2164/#1901), without dropping the row
// or mutating its existing fields. A finding with no bounded ACL claim surfaces
// no source_acl_state. The fail-closed permission filter is unchanged: both rows
// are returned because both pass the binary read-visibility predicate; ACL state
// is additive informational metadata, not (yet) an access decision.
func TestContentReaderDocumentationFindingsSurfacesBoundedSourceACLState(t *testing.T) {
	t.Parallel()

	denied := []byte(`{
		"finding_id": "finding:denied-acl",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"summary": "readable finding whose source ACL read was denied",
		"freshness_state": "fresh",
		"permissions": {"viewer_can_read_source": true},
		"acl_summary": {"source_acl_state": "denied"}
	}`)
	noClaim := []byte(`{
		"finding_id": "finding:no-acl",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"summary": "finding with no ACL claim",
		"permissions": {"viewer_can_read_source": true}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{denied}, {noClaim}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{Limit: 50})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if gotLen, want := len(got.Findings), 2; gotLen != want {
		t.Fatalf("len(Findings) = %d, want %d (ACL state is additive, no row dropped); findings = %#v", gotLen, want, got.Findings)
	}
	if state := got.Findings[0]["source_acl_state"]; state != "denied" {
		t.Fatalf("findings[0].source_acl_state = %#v, want %q (verbatim, distinct axis)", state, "denied")
	}
	// ACL state is distinct from freshness: a fresh finding can still be denied.
	if fresh := got.Findings[0]["freshness_state"]; fresh != "fresh" {
		t.Fatalf("findings[0].freshness_state = %#v, want %q (independent axis)", fresh, "fresh")
	}
	if _, present := got.Findings[1]["source_acl_state"]; present {
		t.Fatalf("findings[1] surfaced source_acl_state with no ACL claim: %#v", got.Findings[1])
	}
}

// TestContentReaderDocumentationEvidencePacketSurfacesBoundedSourceACLState
// proves the evidence-packet readback lifts the bounded source_acl_state from
// the packet's acl_summary to a distinct top-level field (#2164/#1901). This
// represents partial/stale ACL — which the existing binary Denied flag cannot —
// while leaving the binary visibility decision and all other fields unchanged.
func TestContentReaderDocumentationEvidencePacketSurfacesBoundedSourceACLState(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "packet:partial",
		"finding_id": "finding:partial",
		"permissions": {"viewer_can_read_source": true},
		"states": {"freshness_state": "fresh"},
		"acl_summary": {"source_acl_state": "partial"}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacketWithFilter(
		t.Context(),
		documentationEvidencePacketFilter{FindingID: "finding:partial"},
	)
	if err != nil {
		t.Fatalf("documentationEvidencePacketWithFilter() error = %v, want nil", err)
	}
	if !got.Available || got.Denied {
		t.Fatalf("packet Available=%v Denied=%v, want available and not denied", got.Available, got.Denied)
	}
	if state := got.Packet["source_acl_state"]; state != "partial" {
		t.Fatalf("packet source_acl_state = %#v, want %q (distinct from binary Denied)", state, "partial")
	}
}

// TestContentReaderDocumentationEvidencePacketOmitsAbsentSourceACLState proves a
// packet that carries no bounded ACL claim surfaces no source_acl_state field
// (absence means "no ACL claim").
func TestContentReaderDocumentationEvidencePacketOmitsAbsentSourceACLState(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "packet:plain",
		"finding_id": "finding:plain",
		"permissions": {"viewer_can_read_source": true}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacketWithFilter(
		t.Context(),
		documentationEvidencePacketFilter{FindingID: "finding:plain"},
	)
	if err != nil {
		t.Fatalf("documentationEvidencePacketWithFilter() error = %v, want nil", err)
	}
	if _, present := got.Packet["source_acl_state"]; present {
		t.Fatalf("packet surfaced source_acl_state with no ACL claim: %#v", got.Packet)
	}
}
