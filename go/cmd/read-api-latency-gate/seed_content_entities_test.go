// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestContentEntityRowsMatchTheGraphNodesTheyMirror pins the parity the infra
// read model check depends on (issue #6797): eshu-api serves
// /infra/resources/* from a Postgres table derived from content_entities (a
// derived read model), so the seed must write content_entities rows that mirror
// the seeded graph nodes: same id, and the same provider/environment values.
func TestContentEntityRowsMatchTheGraphNodesTheyMirror(t *testing.T) {
	rows := infraContentEntityRows("K8sResource", idRange{First: 4, Last: 7})

	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	for i, r := range rows {
		idx := 4 + i
		if r.EntityID != seedInfraID("K8sResource", idx) {
			t.Errorf("rows[%d].EntityID = %q, want the graph node's id %q", i, r.EntityID, seedInfraID("K8sResource", idx))
		}
		if r.EntityType != "K8sResource" {
			t.Errorf("rows[%d].EntityType = %q, want K8sResource", i, r.EntityType)
		}
		if r.Metadata["provider"] != seedProvider(idx) || r.Metadata["environment"] != seedEnvironment(idx) {
			t.Errorf("rows[%d].Metadata = %v, want the graph node's provider/environment", i, r.Metadata)
		}
		if r.RepoID == "" || r.RelativePath == "" || r.EntityName == "" {
			t.Errorf("rows[%d] has an empty NOT NULL-ish field: %+v", i, r)
		}
	}
}

func TestContentEntityRowsSpreadAcrossRepositories(t *testing.T) {
	rows := infraContentEntityRows("HelmChart", idRange{First: 0, Last: infraSeedRepoCount*3 - 1})

	repos := map[string]int{}
	for _, r := range rows {
		repos[r.RepoID]++
	}
	if len(repos) != infraSeedRepoCount {
		t.Errorf("distinct repos = %d, want %d: the backfill derives one repository at a time, so a single repo would hide its per-repo cost", len(repos), infraSeedRepoCount)
	}
}

func TestIaCContentEntityRowsCarryTheFactIdentity(t *testing.T) {
	facts := BuildIaCFacts("seed-scope-terraform_state-0000", "gen-0", 6)
	rows := iacContentEntityRows(facts)

	if len(rows) != len(facts) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(facts))
	}
	for i, f := range facts {
		if rows[i].EntityID != f.EntityID || rows[i].EntityType != f.EntityType || rows[i].EntityName != f.EntityName {
			t.Errorf("rows[%d] = %+v, want the fact's entity id/type/name %s/%s/%s", i, rows[i], f.EntityID, f.EntityType, f.EntityName)
		}
	}
}

func TestContentEntityCopyRowMatchesTheColumnList(t *testing.T) {
	row := infraContentEntityRows("K8sResource", idRange{First: 0, Last: 0})[0]

	copyRow, err := contentEntityCopyRow(row, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(copyRow) != len(contentEntityColumns) {
		t.Fatalf("copy row has %d values for %d columns", len(copyRow), len(contentEntityColumns))
	}
	var meta map[string]string
	if err := json.Unmarshal(copyRow[len(copyRow)-2].([]byte), &meta); err != nil || meta["provider"] == "" {
		t.Errorf("metadata column is not the JSON metadata: %v %v", copyRow[len(copyRow)-2], err)
	}
}

// TestExpectedContentEntityCountsAgreeWithTheGraphForContentLabels is the
// parity check: for every label that has content_entities rows the graph must
// hold the same number of nodes, and the two labels with no content row
// (CloudResource, TerraformStateResource) must stay graph-only.
func TestExpectedContentEntityCountsAgreeWithTheGraphForContentLabels(t *testing.T) {
	facts := BuildIaCFacts("s", "g", 30) // 10 each of Resource, Module, DataSource
	content := expectedContentEntityCounts(100, facts)
	graph := expectedGraphNodeCounts(100, facts)

	for label, n := range content {
		if graph[label] != n {
			t.Errorf("%s: content_entities %d != graph %d", label, n, graph[label])
		}
	}
	for _, graphOnly := range []string{"CloudResource", "TerraformStateResource"} {
		if _, ok := content[graphOnly]; ok {
			t.Errorf("%s must not have content_entities rows", graphOnly)
		}
		if graph[graphOnly] != 100 {
			t.Errorf("%s graph count = %d, want 100", graphOnly, graph[graphOnly])
		}
	}
	if content["TerraformResource"] != 110 {
		t.Errorf("TerraformResource content count = %d, want 110 (100 bulk + 10 correlated)", content["TerraformResource"])
	}
}

// TestOwnerLedgerMarkerTargetsTheBackfillStateTable pins the two literals the
// marker shares with go/internal/storage/postgres/graph/owner/backfill.go
// (an unexported key and SQL there), since the gate cannot import them.
func TestOwnerLedgerMarkerTargetsTheBackfillStateTable(t *testing.T) {
	if ownerLedgerBackfillKey != "cloud_resource_owner:v1" {
		t.Errorf("key = %q, want cloud_resource_owner:v1", ownerLedgerBackfillKey)
	}
	for _, want := range []string{"graph_node_owner_backfill_state", "backfill_key", "completed_at", "ON CONFLICT (backfill_key) DO NOTHING"} {
		if !strings.Contains(ownerLedgerBackfillMarkSQL, want) {
			t.Errorf("marker SQL does not contain %q", want)
		}
	}
}
