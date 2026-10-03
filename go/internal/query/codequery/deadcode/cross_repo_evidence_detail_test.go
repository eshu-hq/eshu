// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// handleGroupKeys is the exact key set of one handles-mode group object.
var handleGroupKeys = []string{"confidence_label", "consumer_repo_id", "evidence_family", "item_count", "relationship_type"}

// handlesItem is one entity-evidence item in consumer repo "consumer-<slot>".
// Items in different slots never share a group; items sharing a slot, type and
// family always do.
func handlesItem(slot int, relationship, family string, confidence float64, label string) deadcode.CrossRepoDeadCodeEvidence {
	return deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:   fmt.Sprintf("consumer-%02d", slot),
		ConsumerEntityID: fmt.Sprintf("consumer-entity-%02d", slot),
		RelationshipType: relationship,
		EvidenceFamily:   family,
		Citation:         fmt.Sprintf("code_reachability_rows:scope/gen/consumer-%02d/entity/p", slot),
		Confidence:       confidence,
		ConfidenceLabel:  label,
		ResolutionMethod: codeprovenance.MethodImportBinding,
		GenerationID:     "gen-a",
		GenerationStatus: "active",
	}
}

// ambiguousHandlesItem is a low-confidence item that keeps a row unknown.
func ambiguousHandlesItem(slot int) deadcode.CrossRepoDeadCodeEvidence {
	item := handlesItem(slot, "CALLS", "direct_code", 0.4, "low")
	item.Ambiguous = true
	item.NeedsEvidence = true
	item.Reason = "ambiguous_consumer_ownership"
	return item
}

func handlesEvidenceStore(
	evidence map[string][]deadcode.CrossRepoDeadCodeEvidence,
	relationships []map[string]any,
) *crossRepoDeadCodeContentStore {
	store := boundaryHoistStore(relationships)
	store.evidenceByEntity = evidence
	return store
}

// postHandlesRequest runs the real cross-repo handler and returns the status
// and the decoded data and truth objects.
func postHandlesRequest(
	t *testing.T,
	store *crossRepoDeadCodeContentStore,
	body string,
) (int, map[string]any, map[string]any) {
	t.Helper()

	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: store, Neo4j: graph.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code/cross-repo", bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", w.Body.String(), err)
	}
	data, _ := envelope["data"].(map[string]any)
	truth, _ := envelope["truth"].(map[string]any)
	return w.Code, data, truth
}

func decodeGroups(t *testing.T, row map[string]any) []map[string]any {
	t.Helper()

	raw, ok := row["consumer_evidence"].([]any)
	if !ok {
		t.Fatalf("consumer_evidence = %#v, want a list", row["consumer_evidence"])
	}
	groups := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		group := item.(map[string]any)
		keys := make([]string, 0, len(group))
		for key := range group {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, handleGroupKeys) {
			t.Fatalf("group keys = %v, want %v", keys, handleGroupKeys)
		}
		groups = append(groups, group)
	}
	return groups
}

// manyGroupsEvidence returns 15 groups in distinct consumer repos, listed in
// slot order, with the strongest last so an order that follows the input would
// cut it. Every item is unambiguous and above the repo-unique-name confidence,
// so the entity classifies as live_by_consumer.
func manyGroupsEvidence() []deadcode.CrossRepoDeadCodeEvidence {
	items := make([]deadcode.CrossRepoDeadCodeEvidence, 0, 17)
	for slot := 1; slot <= 14; slot++ {
		items = append(items, handlesItem(slot, "CALLS", "direct_code", 0.7, "medium"))
	}
	// Two items share the strongest group: same repo, type, and family.
	items = append(items,
		handlesItem(15, "IMPORTS", "direct_code", 0.95, "high"),
		handlesItem(15, "IMPORTS", "direct_code", 0.93, "high"),
	)
	return items
}

func TestCrossRepoDeadCodeHandlesGroupsCapAndOrderRowEvidence(t *testing.T) {
	t.Parallel()

	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": manyGroupsEvidence()}, nil)
	status, data, _ := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "p-entity")
	groups := decodeGroups(t, row)
	if len(groups) != 5 {
		t.Fatalf("groups = %d, want the cap of 5", len(groups))
	}
	first := groups[0]
	if first["consumer_repo_id"] != "consumer-15" || first["item_count"] != float64(2) || first["confidence_label"] != "high" {
		t.Fatalf("first group = %#v, want the 2-item high group in consumer-15 (the group that decided live_by_consumer)", first)
	}
	if row["confidence_label"] != first["confidence_label"] {
		t.Fatalf("row confidence_label = %v, first group label = %v, want equal", row["confidence_label"], first["confidence_label"])
	}
	if row["consumer_evidence_count"] != float64(16) {
		t.Fatalf("consumer_evidence_count = %#v, want 16 items", row["consumer_evidence_count"])
	}
	if row["consumer_evidence_group_count"] != float64(15) {
		t.Fatalf("consumer_evidence_group_count = %#v, want 15 groups before the cap", row["consumer_evidence_group_count"])
	}
	if row["consumer_evidence_handles_truncated"] != true {
		t.Fatalf("consumer_evidence_handles_truncated = %#v, want true (10 groups cut)", row["consumer_evidence_handles_truncated"])
	}
	// The remaining four groups tie on confidence and item count, so consumer_repo_id ascending decides.
	for i := 1; i < len(groups); i++ {
		want := fmt.Sprintf("consumer-%02d", i)
		if groups[i]["consumer_repo_id"] != want {
			t.Fatalf("groups[%d].consumer_repo_id = %v, want %s", i, groups[i]["consumer_repo_id"], want)
		}
	}
}

func TestCrossRepoDeadCodeHandlesCollapseNeedsNoTruncationMarker(t *testing.T) {
	t.Parallel()

	// 40 items across 3 repositories, one family each: 3 groups, nothing cut.
	items := make([]deadcode.CrossRepoDeadCodeEvidence, 0, 40)
	for i := 0; i < 40; i++ {
		items = append(items, ambiguousHandlesItem(i%3))
	}
	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": items}, nil)
	_, data, _ := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "unknown", "p-entity")
	groups := decodeGroups(t, row)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if _, present := row["consumer_evidence_handles_truncated"]; present {
		t.Fatalf("consumer_evidence_handles_truncated present on a row with no cut groups: %#v", row)
	}
	if row["consumer_evidence_group_count"] != float64(3) || row["consumer_evidence_count"] != float64(40) {
		t.Fatalf("counts = group %v item %v, want 3 and 40", row["consumer_evidence_group_count"], row["consumer_evidence_count"])
	}
	total := 0.0
	for _, group := range groups {
		total += group["item_count"].(float64)
	}
	if total != 40 {
		t.Fatalf("sum of item_count = %v, want 40", total)
	}
}

func TestCrossRepoDeadCodeHandlesSentinelItemGroupsLikeAnyOther(t *testing.T) {
	t.Parallel()

	sentinel := deadcode.CrossRepoDeadCodeEvidence{
		RelationshipType: "CALLS", EvidenceFamily: "direct_code",
		Confidence: 0, ConfidenceLabel: "unknown",
		GenerationStatus: "active", NeedsEvidence: true, Reason: "consumer_evidence_truncated",
	}
	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": {ambiguousHandlesItem(1), sentinel}}, nil)
	_, data, _ := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "unknown", "p-entity")
	groups := decodeGroups(t, row)
	if len(groups) != 2 || groups[1]["consumer_repo_id"] != "" {
		t.Fatalf("groups = %#v, want the sentinel as a second, empty-repo group", groups)
	}
	assertCrossRepoDeadCodeReason(t, row, "consumer_evidence_truncated")
}

func TestCrossRepoDeadCodeHTTPDefaultKeepsFullEvidence(t *testing.T) {
	t.Parallel()

	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": manyGroupsEvidence()}, nil)
	_, data, truth := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10}`)
	if data["evidence_detail"] != "full" {
		t.Fatalf("evidence_detail = %#v, want full as the HTTP default", data["evidence_detail"])
	}
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "p-entity")
	items := row["consumer_evidence"].([]any)
	if len(items) != 16 {
		t.Fatalf("consumer_evidence = %d items, want all 16 in full", len(items))
	}
	if _, ok := items[0].(map[string]any)["citation"]; !ok {
		t.Fatalf("full item lacks citation: %#v", items[0])
	}
	for _, key := range []string{"consumer_evidence_group_count", "consumer_evidence_handles_truncated"} {
		if _, present := row[key]; present {
			t.Fatalf("full row carries handles-only key %s", key)
		}
	}
	if _, present := data["evidence_detail_drilldown"]; present {
		t.Fatalf("full response carries evidence_detail_drilldown")
	}
	if omissions, present := truth["omissions"]; present {
		t.Fatalf("full response truth.omissions = %#v, want absent", omissions)
	}
}

func TestCrossRepoDeadCodeRejectsInvalidEvidenceDetail(t *testing.T) {
	t.Parallel()

	status, _, _ := postHandlesRequest(t, handlesEvidenceStore(nil, nil), `{"repo_id":"repo-producer","evidence_detail":"everything"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown evidence_detail", status)
	}
}

func TestCrossRepoDeadCodeHandlesBoundaryListIsCappedAndCounted(t *testing.T) {
	t.Parallel()

	relationships := make([]map[string]any, 0, 60)
	for i := 0; i < 60; i++ {
		relationships = append(relationships, boundaryRelationship(fmt.Sprintf("consumer-%02d", i)))
	}
	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{}, relationships)
	_, data, truth := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)

	boundary := data["boundary_consumer_evidence"].([]any)
	if len(boundary) != 25 {
		t.Fatalf("boundary_consumer_evidence = %d, want the cap of 25", len(boundary))
	}
	for _, raw := range boundary {
		item := raw.(map[string]any)
		if item["item_count"] != float64(1) {
			t.Fatalf("boundary item = %#v, want item_count 1", item)
		}
		if _, present := item["citation"]; present {
			t.Fatalf("boundary item keeps citation under handles: %#v", item)
		}
	}
	if data["boundary_consumer_evidence_count"] != float64(60) {
		t.Fatalf("boundary_consumer_evidence_count = %#v, want the total 60", data["boundary_consumer_evidence_count"])
	}
	if data["boundary_consumer_evidence_truncated"] != true {
		t.Fatalf("boundary_consumer_evidence_truncated = %#v, want true", data["boundary_consumer_evidence_truncated"])
	}
	assertHandlesOmission(t, truth, "boundary_consumer_evidence", 60)
	if _, present := data["evidence_detail_drilldown"]; !present {
		t.Fatalf("evidence_detail_drilldown absent although the boundary list was reduced")
	}
}

func TestCrossRepoDeadCodeFullBoundaryListIsNotCapped(t *testing.T) {
	t.Parallel()

	relationships := make([]map[string]any, 0, 60)
	for i := 0; i < 60; i++ {
		relationships = append(relationships, boundaryRelationship(fmt.Sprintf("consumer-%02d", i)))
	}
	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{}, relationships)
	_, data, _ := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"full"}`)
	if got := len(data["boundary_consumer_evidence"].([]any)); got != 60 {
		t.Fatalf("full boundary_consumer_evidence = %d, want all 60", got)
	}
	if _, present := data["boundary_consumer_evidence_truncated"]; present {
		t.Fatalf("full response carries boundary_consumer_evidence_truncated")
	}
}

func TestCrossRepoDeadCodeHandlesOmissionsCountRowItems(t *testing.T) {
	t.Parallel()

	store := handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": manyGroupsEvidence()}, nil)
	_, data, truth := postHandlesRequest(t, store, `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)
	assertHandlesOmission(t, truth, "candidate_buckets.consumer_evidence", 16)
	for _, raw := range truth["omissions"].([]any) {
		if raw.(map[string]any)["section"] == "boundary_consumer_evidence" {
			t.Fatalf("boundary omission present although no row used the boundary list: %#v", truth["omissions"])
		}
	}
	if data["evidence_detail"] != "handles" {
		t.Fatalf("evidence_detail = %#v, want handles", data["evidence_detail"])
	}
	drilldown, _ := data["evidence_detail_drilldown"].(map[string]any)
	if drilldown["full_rows"] == nil {
		t.Fatalf("evidence_detail_drilldown = %#v, want a full_rows hint", data["evidence_detail_drilldown"])
	}
}

func TestCrossRepoDeadCodeHandlesNoOmissionWhenNothingReduced(t *testing.T) {
	t.Parallel()

	_, data, truth := postHandlesRequest(t, handlesEvidenceStore(map[string][]deadcode.CrossRepoDeadCodeEvidence{}, nil),
		`{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)
	if _, present := truth["omissions"]; present {
		t.Fatalf("truth.omissions = %#v, want absent when no evidence was reduced", truth["omissions"])
	}
	if _, present := data["evidence_detail_drilldown"]; present {
		t.Fatalf("evidence_detail_drilldown present although nothing was reduced")
	}
	if data["evidence_detail"] != "handles" {
		t.Fatalf("evidence_detail = %#v, want handles", data["evidence_detail"])
	}
}

func assertHandlesOmission(t *testing.T, truth map[string]any, section string, total float64) {
	t.Helper()

	raw, _ := truth["omissions"].([]any)
	for _, item := range raw {
		omission := item.(map[string]any)
		if omission["section"] == section {
			if omission["detail"] != "handles" || omission["total"] != total {
				t.Fatalf("omission %s = %#v, want detail handles total %v", section, omission, total)
			}
			return
		}
	}
	t.Fatalf("truth.omissions = %#v, want an entry for %s", truth["omissions"], section)
}

// TestCrossRepoDeadCodeHandlesDoesNotChangeClassification pins that shaping is
// output projection: every bucket, reason, hidden count, bucket count, and the
// analysis block is identical between full and handles; only the consumer
// evidence fields differ.
func TestCrossRepoDeadCodeHandlesDoesNotChangeClassification(t *testing.T) {
	t.Parallel()

	build := func() *crossRepoDeadCodeContentStore {
		store := boundaryHoistStore([]map[string]any{boundaryRelationship("consumer-1"), boundaryRelationship("consumer-2")})
		store.evidenceByEntity = map[string][]deadcode.CrossRepoDeadCodeEvidence{"p-entity": manyGroupsEvidence()}
		store.hiddenConsumers = []string{"p-boundary-a"}
		return store
	}
	_, full, _ := postHandlesRequest(t, build(), `{"repo_id":"repo-producer","limit":10,"evidence_detail":"full"}`)
	_, handles, _ := postHandlesRequest(t, build(), `{"repo_id":"repo-producer","limit":10,"evidence_detail":"handles"}`)

	if !reflect.DeepEqual(full["bucket_counts"], handles["bucket_counts"]) {
		t.Fatalf("bucket_counts differ: full %#v handles %#v", full["bucket_counts"], handles["bucket_counts"])
	}
	if !reflect.DeepEqual(full["analysis"], handles["analysis"]) {
		t.Fatalf("analysis differs: full %#v handles %#v", full["analysis"], handles["analysis"])
	}
	fullBuckets := full["candidate_buckets"].(map[string]any)
	handleBuckets := handles["candidate_buckets"].(map[string]any)
	for _, name := range []string{"dead", "live_by_consumer", "unknown", "suppressed"} {
		fullRows := fullBuckets[name].([]any)
		handleRows := handleBuckets[name].([]any)
		if len(fullRows) != len(handleRows) {
			t.Fatalf("bucket %s: full %d rows, handles %d rows", name, len(fullRows), len(handleRows))
		}
		for i := range fullRows {
			if !reflect.DeepEqual(stripEvidenceFields(fullRows[i]), stripEvidenceFields(handleRows[i])) {
				t.Fatalf("bucket %s row %d differs beyond evidence fields:\nfull    %#v\nhandles %#v", name, i, fullRows[i], handleRows[i])
			}
		}
	}
	if len(fullBuckets["unknown"].([]any)) == 0 || len(fullBuckets["live_by_consumer"].([]any)) == 0 {
		t.Fatalf("fixture must exercise the live and unknown buckets: %#v", fullBuckets)
	}
}

// stripEvidenceFields returns a copy of a row without the consumer evidence
// payload and its handles-only companions, leaving everything classification
// decided (buckets, reasons, hidden counts, labels).
func stripEvidenceFields(raw any) map[string]any {
	row, _ := raw.(map[string]any)
	out := make(map[string]any, len(row))
	for key, value := range row {
		switch key {
		case "consumer_evidence", "consumer_evidence_group_count", "consumer_evidence_handles_truncated":
			continue
		}
		out[key] = value
	}
	return out
}
