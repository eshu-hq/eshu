// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestServiceDocumentationEvidenceLoaderScopesByServiceAndDurableIdentity(t *testing.T) {
	t.Parallel()

	// One bounded query per service; columns are the durable identity plus
	// observable fields, never service_id/fact_id/generation_id.
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{
			// source_system, source_record_id, document_id, fact_kind, source_uri, observation_hash, source_acl_state
			{"confluence", "section:deploy", "doc:runbook", "documentation_entity_mention", "https://wiki/runbook", "hash-1", "partial"},
			{"git_markdown", "docs/readme.md#intro", "doc:readme", "documentation_claim_candidate", "", "", ""},
		}},
		{rows: [][]any{
			{"confluence", "section:x", "doc:other", "semantic.documentation_observation", "", "hash-2", "denied"},
		}},
	}}

	loader := NewServiceDocumentationEvidenceLoader(queryer)
	byService, err := loader.GetDocumentationEvidenceForServices(context.Background(), []string{"svc-a", "svc-b"})
	if err != nil {
		t.Fatalf("GetDocumentationEvidenceForServices() error = %v, want nil", err)
	}

	if len(byService["svc-a"]) != 2 {
		t.Fatalf("svc-a docs records = %d, want 2", len(byService["svc-a"]))
	}
	if len(byService["svc-b"]) != 1 {
		t.Fatalf("svc-b docs records = %d, want 1", len(byService["svc-b"]))
	}
	first := byService["svc-a"][0]
	if first.SourceSystem != "confluence" || first.SourceRecordID != "section:deploy" || first.DocumentID != "doc:runbook" {
		t.Fatalf("svc-a first record missing durable identity: %#v", first)
	}
	// The bounded source_acl_state is read verbatim from the fact's acl_summary
	// and threaded into the reducer record. A fact with no ACL summary scans the
	// empty string (no ACL claim), never an invented default.
	if first.SourceACLState != "partial" {
		t.Fatalf("svc-a first record source_acl_state = %q, want %q", first.SourceACLState, "partial")
	}
	if got := byService["svc-a"][1].SourceACLState; got != "" {
		t.Fatalf("svc-a second record without ACL summary must scan empty source_acl_state, got %q", got)
	}
	if got := byService["svc-b"][0].SourceACLState; got != "denied" {
		t.Fatalf("svc-b record source_acl_state = %q, want %q", got, "denied")
	}

	// The query must gate on the active generation, the documentation fact kinds,
	// non-tombstone rows, and the per-service ref, and must NOT key on fact_id or
	// generation_id.
	query := queryer.queries[0]
	for _, want := range []string{
		"fact_records",
		"is_tombstone = FALSE",
		"status = 'active'",
		"documentation_entity_mention",
		"documentation_claim_candidate",
		"semantic.documentation_observation",
		// document_id is read from the top-level field or the nested
		// source.document_id (semantic observations), so semantic observations stay
		// keyable on their durable document id.
		"fact.payload->'source'->>'document_id'",
		// source_acl_state is read verbatim from the fact's acl_summary so the
		// reducer can project the bounded access-posture observation.
		"fact.payload->'acl_summary'->>'source_acl_state'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("documentation evidence query missing %q:\n%s", want, query)
		}
	}
}

func TestServiceDocumentationEvidenceLoaderEmptyServicesIsNoOp(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	loader := NewServiceDocumentationEvidenceLoader(queryer)
	byService, err := loader.GetDocumentationEvidenceForServices(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetDocumentationEvidenceForServices() error = %v, want nil", err)
	}
	if len(byService) != 0 {
		t.Fatalf("expected no records for empty service set, got %d", len(byService))
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("expected no query for empty service set, ran %d", len(queryer.queries))
	}
}

var _ reducer.ServiceScopedDocumentationEvidenceLoader = ServiceDocumentationEvidenceLoader{}
