// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildDocumentationRowMapRoutesEntityTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "uid:func",
		"scope_id":         "scope-1",
		"document_id":      "doc-1",
		"section_id":       "sec-1",
		"heading_text":     "Runbook",
		"target_kind":      "entity",
		"mention_kind":     "code_symbol",
	}
	cypher, rowMap, ok := buildDocumentationRowMap(payload, "reducer/documentation")
	if !ok {
		t.Fatal("buildDocumentationRowMap ok = false, want true")
	}
	if cypher != batchCanonicalDocumentationEntityEdgeCypher {
		t.Errorf("expected entity edge template, got %q", cypher)
	}
	if !strings.Contains(cypher, "rel:DOCUMENTS") || !strings.Contains(cypher, "MERGE (section:DocumentationSection") {
		t.Errorf("template missing DOCUMENTS edge / section node: %q", cypher)
	}
	if rowMap["scope_id"] != "scope-1" || rowMap["heading_text"] != "Runbook" {
		t.Errorf("rowMap identity fields not carried: %#v", rowMap)
	}
}

func TestBuildDocumentationRowMapRoutesWorkloadTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "wl-1",
		"scope_id":         "scope-1",
		"target_kind":      "workload",
	}
	cypher, _, ok := buildDocumentationRowMap(payload, "reducer/documentation")
	if !ok {
		t.Fatal("buildDocumentationRowMap ok = false, want true")
	}
	if cypher != batchCanonicalDocumentationWorkloadEdgeCypher {
		t.Errorf("expected workload edge template, got %q", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:Workload {id: row.target_entity_id})") {
		t.Errorf("workload template does not match Workload by id: %q", cypher)
	}
}

// TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel is the
// regression guard for issue #5994's SqlTable fix. A documentation entity
// mention whose candidate resolves to a SQL table (target_kind "table") is
// NOT distinguished from a code-entity target by buildDocumentationRowMap's
// switch (only "workload" and "service" are distinct cases; everything else,
// including "table", falls through to the default branch and is routed to
// batchCanonicalDocumentationEntityEdgeCypher). Before the fix, that
// template's MATCH clause omitted SqlTable from its label alternation even
// though SqlTable nodes are uid-keyed exactly like the labels that were
// present (canonical.go:163's SQL relationship writer MATCHes SqlTable by
// uid the same way): a "table"-kind mention's DOCUMENTS edge write silently
// no-opped against a live backend. The template leads with that MATCH, and a
// MATCH yielding no rows for an UNWIND row eliminates that row before any
// later clause runs -- so neither the DocumentationSection MERGE nor the
// DOCUMENTS MERGE executed for it. No node from that row, no relationship, no
// error -- a section with other resolving mentions still gets its node from
// those, which is this fixture's shape: all three rows share one section uid,
// so pre-fix the section existed via the Function and Class rows. This test
// was RED before the fix (commit a3347e898) and is GREEN after it (SqlTable
// added to the label alternation); it stays as a permanent regression guard,
// not a scratch artifact.
//
// target_entity_id uses the real production uid convention for a SqlTable
// node -- the content_entity fact's own entity_id, used verbatim, because
// SqlTable is not in projector.canonicalNamePathLineEntityLabels
// (canonical_entity_identity.go:12-57) so canonicalGraphEntityID's
// else-branch returns the incoming id unchanged. Mirrors
// sql_relationship_odu.go:93's sqlFamilyContentEntity convention
// ("content-entity:sql-tbl-users"), not the invented "sqltable:..." shape an
// earlier fixture used.
func TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "content-entity:sql-tbl-payments",
		"target_kind":      "table",
	}
	cypher, _, ok := buildDocumentationRowMap(payload, "reducer/documentation")
	if !ok {
		t.Fatal("buildDocumentationRowMap ok = false, want true")
	}
	if !strings.Contains(cypher, "SqlTable") {
		t.Fatalf("target_kind=\"table\" routed to a cypher template whose MATCH clause does not mention SqlTable: %q; "+
			"a documentation mention resolving to a SQL table cannot materialize a DOCUMENTS edge against a live backend", cypher)
	}
}

func TestBuildDocumentationRowMapDropsServiceTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "svc-1",
		"target_kind":      "service",
	}
	if _, _, ok := buildDocumentationRowMap(payload, "reducer/documentation"); ok {
		t.Error("service target should be dropped (no Service node), got ok=true")
	}
}

func TestBuildDocumentationRowMapRequiresSectionAndTarget(t *testing.T) {
	if _, _, ok := buildDocumentationRowMap(map[string]any{"target_entity_id": "uid:func"}, "src"); ok {
		t.Error("missing section_uid should be rejected")
	}
	if _, _, ok := buildDocumentationRowMap(map[string]any{"section_uid": "docsec:1"}, "src"); ok {
		t.Error("missing target_entity_id should be rejected")
	}
}

func TestRetractDocumentationEdgesIsScopeScoped(t *testing.T) {
	stmt := BuildRetractDocumentationEdges([]string{"scope-1"}, "reducer/documentation")
	if !strings.Contains(stmt.Cypher, "rel:DOCUMENTS") {
		t.Errorf("retract does not target DOCUMENTS: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		t.Errorf("retract is not scope-scoped: %q", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["scope_ids"]; !ok {
		t.Error("retract missing scope_ids parameter")
	}
}

func TestBuildRetractDocumentationEdgesByDocumentID(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractDocumentationEdgesByDocumentID(
		[]string{"scope-1"},
		[]string{"doc:git:repo-123:README.md"},
		"reducer/documentation",
	)
	if !strings.Contains(stmt.Cypher, "rel:DOCUMENTS") {
		t.Fatalf("cypher = %q, want DOCUMENTS cleanup", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		t.Fatalf("cypher = %q, want scope filter", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.document_id IN $document_ids") {
		t.Fatalf("cypher = %q, want document_id filter", stmt.Cypher)
	}
	scopeIDs, ok := stmt.Parameters["scope_ids"].([]string)
	if !ok {
		t.Fatalf("scope_ids parameter type = %T, want []string", stmt.Parameters["scope_ids"])
	}
	if !reflect.DeepEqual(scopeIDs, []string{"scope-1"}) {
		t.Fatalf("scope_ids = %#v, want [scope-1]", scopeIDs)
	}
	documentIDs, ok := stmt.Parameters["document_ids"].([]string)
	if !ok {
		t.Fatalf("document_ids parameter type = %T, want []string", stmt.Parameters["document_ids"])
	}
	wantDocumentIDs := []string{"doc:git:repo-123:README.md"}
	if !reflect.DeepEqual(documentIDs, wantDocumentIDs) {
		t.Fatalf("document_ids = %#v, want %#v", documentIDs, wantDocumentIDs)
	}
}

func TestBuildRetractDocumentationEdgesBySectionUID(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractDocumentationEdgesBySectionUID(
		[]string{"scope-1"},
		[]string{"docsection:doc:git:repo-123:README.md|sec-overview"},
		"reducer/documentation",
	)
	if !strings.Contains(stmt.Cypher, "rel:DOCUMENTS") {
		t.Fatalf("cypher = %q, want DOCUMENTS cleanup", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		t.Fatalf("cypher = %q, want scope filter", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.uid IN $section_uids") {
		t.Fatalf("cypher = %q, want section uid filter", stmt.Cypher)
	}
	sectionUIDs, ok := stmt.Parameters["section_uids"].([]string)
	if !ok {
		t.Fatalf("section_uids parameter type = %T, want []string", stmt.Parameters["section_uids"])
	}
	wantSectionUIDs := []string{"docsection:doc:git:repo-123:README.md|sec-overview"}
	if !reflect.DeepEqual(sectionUIDs, wantSectionUIDs) {
		t.Fatalf("section_uids = %#v, want %#v", sectionUIDs, wantSectionUIDs)
	}
}

func TestEdgeWriterRetractEdgesDocumentationDeltaUsesDocumentScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "scope-1",
			Payload: map[string]any{
				"scope_id":         "scope-1",
				"delta_projection": true,
				"document_ids":     []string{"doc:git:repo-123:README.md"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDocumentationEdges, rows, "reducer/documentation")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "section.document_id IN $document_ids") {
		t.Fatalf("delta retract cypher = %q, want document_id filter", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		scopeIDs, ok := stmt.Parameters["scope_ids"].([]string)
		if !ok || strings.Join(scopeIDs, ",") != "scope-1" {
			t.Fatalf("scope_ids = %#v, want [scope-1]", stmt.Parameters["scope_ids"])
		}
	}
	documentIDs, ok := stmt.Parameters["document_ids"].([]string)
	if !ok {
		t.Fatalf("document_ids parameter type = %T, want []string", stmt.Parameters["document_ids"])
	}
	if got, want := strings.Join(documentIDs, ","), "doc:git:repo-123:README.md"; got != want {
		t.Fatalf("document_ids = %q, want %q", got, want)
	}
}

func TestEdgeWriterRetractEdgesDocumentationDeltaUsesSectionScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "scope-1",
			Payload: map[string]any{
				"scope_id":         "scope-1",
				"delta_projection": true,
				"section_uids":     []string{"docsection:doc:git:repo-123:README.md|sec-overview"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDocumentationEdges, rows, "reducer/documentation")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "section.uid IN $section_uids") {
		t.Fatalf("delta retract cypher = %q, want section uid filter", stmt.Cypher)
	}
	sectionUIDs, ok := stmt.Parameters["section_uids"].([]string)
	if !ok {
		t.Fatalf("section_uids parameter type = %T, want []string", stmt.Parameters["section_uids"])
	}
	if got, want := strings.Join(sectionUIDs, ","), "docsection:doc:git:repo-123:README.md|sec-overview"; got != want {
		t.Fatalf("section_uids = %q, want %q", got, want)
	}
}

func TestEdgeWriterRetractEdgesDocumentationWholeScopeBindsScopeIDNotRepoID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			ScopeID:      "scope-doc",
			RepositoryID: "repo-code",
			Payload: map[string]any{
				"scope_id": "scope-doc",
				"repo_id":  "repo-code",
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDocumentationEdges, rows, "reducer/documentation")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		t.Fatalf("whole-scope retract cypher = %q, want scope filter", stmt.Cypher)
	}
	scopeIDs, ok := stmt.Parameters["scope_ids"].([]string)
	if !ok {
		t.Fatalf("scope_ids parameter type = %T, want []string", stmt.Parameters["scope_ids"])
	}
	if !reflect.DeepEqual(scopeIDs, []string{"scope-doc"}) {
		t.Fatalf("scope_ids = %#v, want [scope-doc] (must bind row.ScopeID, not RepositoryID)", scopeIDs)
	}
}

func TestEdgeWriterRetractEdgesDocumentationDeltaBindsScopeIDNotRepoID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			ScopeID:      "scope-doc",
			RepositoryID: "repo-code",
			Payload: map[string]any{
				"scope_id":         "scope-doc",
				"repo_id":          "repo-code",
				"delta_projection": true,
				"document_ids":     []string{"doc:git:repo-123:README.md"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDocumentationEdges, rows, "reducer/documentation")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "section.document_id IN $document_ids") {
		t.Fatalf("delta retract cypher = %q, want document_id filter", stmt.Cypher)
	}
	scopeIDs, ok := stmt.Parameters["scope_ids"].([]string)
	if !ok {
		t.Fatalf("scope_ids parameter type = %T, want []string", stmt.Parameters["scope_ids"])
	}
	if !reflect.DeepEqual(scopeIDs, []string{"scope-doc"}) {
		t.Fatalf("scope_ids = %#v, want [scope-doc] (must bind row.ScopeID, not RepositoryID)", scopeIDs)
	}
}

func TestEdgeWriterRetractEdgesDocumentationRejectsDeltaWithoutIdentity(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "scope-1",
			Payload: map[string]any{
				"scope_id":         "scope-1",
				"delta_projection": true,
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDocumentationEdges, rows, "reducer/documentation")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want malformed delta scope error")
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 for malformed delta scope", got)
	}
}
