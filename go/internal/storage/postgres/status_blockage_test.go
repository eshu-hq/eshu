// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueBlockagesReportAWSRelationshipReadinessWait(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: nil}}}
	if _, err := listReducerConflictBlockages(
		context.Background(),
		queryer,
		time.Date(2026, time.May, 31, 10, 30, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("listReducerConflictBlockages() error = %v", err)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}

	query := queryer.queries[0]
	for _, want := range []string{
		"active_fact_work_items AS (",
		"FROM fact_work_items AS work",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = active_generation.generation_id",
		"work.stage = 'reducer'",
		"work.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"FROM active_fact_work_items",
		"readiness_blocked AS (",
		"JOIN reducer_claim_readiness_requirements AS readiness_req",
		"readiness_phase.acceptance_unit_id = CASE readiness_req.acceptance_unit_source",
		"readiness_phase.keyspace = readiness_req.keyspace",
		"readiness_phase.phase = readiness_req.phase",
		"'readiness' AS conflict_domain",
		"readiness_req.keyspace || ':' || readiness_req.phase || ':'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("blockage query missing AWS relationship readiness diagnostic %q:\n%s", want, query)
		}
	}
	if !queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainAWSRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("blockage query missing AWS relationship readiness requirement:\n%s", query)
	}
}

func TestReducerQueueBlockagesReportIAMPermissionReadinessWait(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: nil}}}
	if _, err := listReducerConflictBlockages(
		context.Background(),
		queryer,
		time.Date(2026, time.June, 6, 10, 30, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("listReducerConflictBlockages() error = %v", err)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}

	query := queryer.queries[0]
	for _, domain := range []reducer.Domain{
		reducer.DomainIAMEscalationMaterialization,
		reducer.DomainIAMCanPerformMaterialization,
	} {
		if !queryHasBoundedReadinessRequirement(query, string(domain), "cloud_resource_uid", "canonical_nodes_committed") {
			t.Fatalf("blockage query missing IAM permission readiness requirement for %q:\n%s", domain, query)
		}
	}
	if !strings.Contains(query, "readiness_req.keyspace || ':' || readiness_req.phase || ':'") {
		t.Fatalf("blockage query missing bounded readiness conflict key expression:\n%s", query)
	}
}

func TestReducerQueueBlockagesExposeDistinctDomainBlockedCount(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: nil}}}
	if _, err := listReducerConflictBlockages(
		context.Background(),
		queryer,
		time.Date(2026, time.June, 6, 10, 35, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("listReducerConflictBlockages() error = %v", err)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}

	query := queryer.queries[0]
	for _, want := range []string{
		"work_item_id",
		"COUNT(DISTINCT work_item_id) AS domain_blocked_count",
		"JOIN domain_blocked USING (domain)",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("blockage query missing distinct domain blocked count %q:\n%s", want, query)
		}
	}
}
