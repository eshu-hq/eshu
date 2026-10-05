// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// rootPathCall is one consumer-root path read the handler sent (#7603).
type rootPathCall struct {
	rootEntityIDs   []string
	consumerRepoIDs []string
}

// CrossRepoDeadCodeConsumerRootPaths answers the batched root path read from the
// test's rootPaths and records what the handler asked.
func (s *crossRepoDeadCodeContentStore) CrossRepoDeadCodeConsumerRootPaths(
	_ context.Context,
	rootEntityIDs []string,
	consumerRepoIDs []string,
) (map[string]string, error) {
	s.rootPathCalls = append(s.rootPathCalls, rootPathCall{
		rootEntityIDs:   slices.Clone(rootEntityIDs),
		consumerRepoIDs: slices.Clone(consumerRepoIDs),
	})
	if s.rootPathsErr != nil {
		return nil, s.rootPathsErr
	}
	found := make(map[string]string, len(rootEntityIDs))
	for _, id := range rootEntityIDs {
		if path, ok := s.rootPaths[id]; ok {
			found[id] = path
		}
	}
	return found, nil
}

// rootedConsumer is a strong consumer whose root entity is rootID.
func rootedConsumer(repoID, rootID string) deadcode.CrossRepoDeadCodeEvidence {
	item := strongConsumerEvidence(repoID)
	item.ConsumerEntityID = rootID
	return item
}

func TestCrossRepoDeadCodeTestOnlyConsumers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		evidence  []deadcode.CrossRepoDeadCodeEvidence
		rootPaths map[string]string
		hidden    bool
		wantRow   string
		want      bool
	}{
		{
			name:      "every consumer root is a test file",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a"), rootedConsumer("repo-other", "root-b")},
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go", "root-b": "tests/unit/test_charge.py"},
			wantRow:   "live_by_consumer",
			want:      true,
		},
		{
			name:      "one non-test consumer root keeps the flag off",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a"), rootedConsumer("repo-other", "root-b")},
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go", "root-b": "pkg/checkout/charge.go"},
			wantRow:   "live_by_consumer",
		},
		{
			name:      "a root outside the test file rule is not a test",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			rootPaths: map[string]string{"root-a": "pkg/contest/helper.go"},
			wantRow:   "live_by_consumer",
		},
		{
			name:      "a fixture directory root is a test by the single rule",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			rootPaths: map[string]string{"root-a": "services/api/tests/fixtures/seed.ts"},
			wantRow:   "live_by_consumer",
			want:      true,
		},
		{
			name:      "a java test source set root is a test by the single rule",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			rootPaths: map[string]string{"root-a": "app/src/integrationTest/java/ChargeIT.java"},
			wantRow:   "live_by_consumer",
			want:      true,
		},
		{
			name:      "a missing root entity keeps the flag off",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a"), rootedConsumer("repo-other", "root-b")},
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go"},
			wantRow:   "live_by_consumer",
		},
		{
			name:      "a consumer with no root entity id keeps the flag off",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "")},
			rootPaths: map[string]string{"": "pkg/checkout/charge_test.go"},
			wantRow:   "live_by_consumer",
		},
		{
			name:      "a hidden consumer keeps the flag off",
			evidence:  []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go"},
			hidden:    true,
			wantRow:   "live_by_consumer",
		},
		{
			name: "a needs-evidence consumer keeps the row unknown and unflagged",
			evidence: []deadcode.CrossRepoDeadCodeEvidence{
				rootedConsumer("repo-consumer", "root-a"),
				{ConsumerRepoID: "repo-other", ConsumerEntityID: "root-b", NeedsEvidence: true, Reason: "stale_generation", GenerationStatus: "stale"},
			},
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go", "root-b": "pkg/checkout/other_test.go"},
			wantRow:   "unknown",
		},
		{
			name:      "no consumer leaves the row dead and unflagged",
			rootPaths: map[string]string{"root-a": "pkg/checkout/charge_test.go"},
			wantRow:   "dead",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := coverageStore(tt.evidence)
			store.rootPaths = tt.rootPaths
			if tt.hidden {
				store.hiddenConsumers = []string{"producer-symbol"}
			}
			for _, detail := range []string{"full", "handles"} {
				status, data := postCoverageRequest(t, store,
					`{"repo_id":"repo-producer","limit":10,"evidence_detail":"`+detail+`"}`, nil)
				if status != http.StatusOK {
					t.Fatalf("%s: status = %d, want 200", detail, status)
				}
				buckets := data["candidate_buckets"].(map[string]any)
				row := assertCrossRepoDeadCodeBucketEntity(t, buckets, tt.wantRow, "producer-symbol")
				value, present := row["test_only_consumers"]
				if tt.want {
					if value != true {
						t.Fatalf("%s: test_only_consumers = %#v, want true", detail, value)
					}
					continue
				}
				if present {
					t.Fatalf("%s: test_only_consumers = %#v, want the key omitted", detail, value)
				}
			}
		})
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersLeavesLivenessUntouched pins the owner
// decision: a test caller is a caller. The flag adds one fact and changes no
// bucket, classification, confidence label or evidence.
func TestCrossRepoDeadCodeTestOnlyConsumersLeavesLivenessUntouched(t *testing.T) {
	t.Parallel()

	evidence := []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")}
	withPaths := coverageStore(evidence)
	withPaths.rootPaths = map[string]string{"root-a": "pkg/checkout/charge_test.go"}
	withoutPaths := coverageStore(evidence)

	_, flagged := postCoverageRequest(t, withPaths, `{"repo_id":"repo-producer","limit":10}`, nil)
	_, plain := postCoverageRequest(t, withoutPaths, `{"repo_id":"repo-producer","limit":10}`, nil)

	flaggedRow := assertCrossRepoDeadCodeBucketEntity(t, flagged["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
	plainRow := assertCrossRepoDeadCodeBucketEntity(t, plain["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
	delete(flaggedRow, "test_only_consumers")
	if !reflect.DeepEqual(flaggedRow, plainRow) {
		t.Fatalf("flag changed more than itself:\nflagged %#v\nplain   %#v", flaggedRow, plainRow)
	}
	if !reflect.DeepEqual(flagged["bucket_counts"], plain["bucket_counts"]) {
		t.Fatalf("bucket_counts changed: %#v vs %#v", flagged["bucket_counts"], plain["bucket_counts"])
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersOneBatchedRead pins the cost: one read
// per request carrying every distinct consumer root id once and the consumer
// repositories, and none when no candidate has a consumer root.
func TestCrossRepoDeadCodeTestOnlyConsumersOneBatchedRead(t *testing.T) {
	t.Parallel()

	two := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{
		rootedConsumer("repo-consumer", "root-a"),
		rootedConsumer("repo-consumer", "root-a"),
		rootedConsumer("repo-other", "root-b"),
		{ConsumerRepoID: "repo-other", ConsumerEntityID: "root-c", NeedsEvidence: true, Reason: "consumer_evidence_truncated"},
	})
	if status, _ := postCoverageRequest(t, two, `{"repo_id":"repo-producer","limit":10}`, nil); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(two.rootPathCalls) != 1 {
		t.Fatalf("root path reads = %d, want exactly 1", len(two.rootPathCalls))
	}
	call := two.rootPathCalls[0]
	slices.Sort(call.rootEntityIDs)
	slices.Sort(call.consumerRepoIDs)
	if want := []string{"root-a", "root-b"}; !reflect.DeepEqual(call.rootEntityIDs, want) {
		t.Fatalf("root ids = %v, want %v (distinct, no needs-evidence marker)", call.rootEntityIDs, want)
	}
	if want := []string{"repo-consumer", "repo-other"}; !reflect.DeepEqual(call.consumerRepoIDs, want) {
		t.Fatalf("consumer repo ids = %v, want %v", call.consumerRepoIDs, want)
	}

	// The flag cannot be set when the consumer set may be unseen, so the read is
	// skipped: no consumer root, a named consumer selector, an incomplete
	// consumer snapshot.
	skipped := []struct {
		name     string
		evidence []deadcode.CrossRepoDeadCodeEvidence
		gaps     []string
		body     string
	}{
		{name: "no consumer root", body: `{"repo_id":"repo-producer","limit":10}`},
		{
			name:     "a named consumer selector",
			evidence: []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			body:     `{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`,
		},
		{
			name:     "an incomplete consumer snapshot",
			evidence: []deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")},
			gaps:     []string{"repo-other"},
			body:     `{"repo_id":"repo-producer","limit":10}`,
		},
	}
	for _, tt := range skipped {
		store := coverageStore(tt.evidence)
		store.coverageGaps = tt.gaps
		if status, _ := postCoverageRequest(t, store, tt.body, nil); status != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tt.name, status)
		}
		if len(store.rootPathCalls) != 0 {
			t.Fatalf("%s: root path reads = %d, want 0", tt.name, len(store.rootPathCalls))
		}
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersBoundaryRowsStayUnflagged keeps the
// repository-boundary fallback out of the flag: those rows have no entity-level
// consumer, so there is no root to call a test.
func TestCrossRepoDeadCodeTestOnlyConsumersBoundaryRowsStayUnflagged(t *testing.T) {
	t.Parallel()

	store := boundaryHoistStore([]map[string]any{boundaryRelationship("consumer-1")})
	store.rootPaths = map[string]string{"consumer-root": "pkg/consumer/root_test.go"}
	data := postBoundaryHoistRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
	buckets := data["candidate_buckets"].(map[string]any)
	flagged := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "p-entity")
	if flagged["test_only_consumers"] != true {
		t.Fatalf("entity-evidence row test_only_consumers = %#v, want true", flagged["test_only_consumers"])
	}
	for _, entityID := range []string{"p-boundary-a", "p-boundary-b"} {
		row := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", entityID)
		if row["consumer_evidence_source"] != "repository_boundary" {
			t.Fatalf("%s source = %#v, want repository_boundary", entityID, row["consumer_evidence_source"])
		}
		if _, present := row["test_only_consumers"]; present {
			t.Fatalf("%s carries test_only_consumers on a boundary fallback row", entityID)
		}
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersReadFailureFailsRequest keeps a failed
// read from becoming a silent "not test only".
func TestCrossRepoDeadCodeTestOnlyConsumersReadFailureFailsRequest(t *testing.T) {
	t.Parallel()

	store := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")})
	store.rootPathsErr = context.DeadlineExceeded
	status, _ := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
	if status == http.StatusOK {
		t.Fatalf("status = %d, want a failed request when the root path read fails", status)
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersWireShape pins the JSON: true when set,
// the key absent (not false) otherwise.
func TestCrossRepoDeadCodeTestOnlyConsumersWireShape(t *testing.T) {
	t.Parallel()

	store := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")})
	store.rootPaths = map[string]string{"root-a": "pkg/checkout/charge_test.go"}
	_, data := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	if got := string(decoded["test_only_consumers"]); got != "true" {
		t.Fatalf("wire test_only_consumers = %q, want true", got)
	}

	store.rootPaths = map[string]string{"root-a": "pkg/checkout/charge.go"}
	_, data = postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
	row = assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
	encoded, _ = json.Marshal(row)
	decoded = nil
	_ = json.Unmarshal(encoded, &decoded)
	if _, present := decoded["test_only_consumers"]; present {
		t.Fatalf("wire carries test_only_consumers for a non-test consumer: %s", encoded)
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersNeedsEveryConsumerVisible keeps the flag
// off when the answer cannot see every consumer. Strong evidence still makes the
// row live (one consumer is enough), but "only tests call this" needs the whole
// consumer set: an incomplete consumer snapshot, or a consumer selector that
// bounds the read to the named repositories, can hide a production caller.
func TestCrossRepoDeadCodeTestOnlyConsumersNeedsEveryConsumerVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		gaps []string
		body string
	}{
		{name: "an incomplete consumer snapshot", gaps: []string{"repo-other"}, body: `{"repo_id":"repo-producer","limit":10}`},
		{name: "a named consumer selector", body: `{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")})
			store.rootPaths = map[string]string{"root-a": "pkg/checkout/charge_test.go"}
			store.coverageGaps = tt.gaps
			status, data := postCoverageRequest(t, store, tt.body, nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
			if value, present := row["test_only_consumers"]; present {
				t.Fatalf("test_only_consumers = %#v, want the key omitted: an unseen consumer may not be a test", value)
			}
		})
	}
}

// TestCrossRepoDeadCodeTestOnlyConsumersGrantOnlyRequest covers the fourth
// request shape against the merged coverage code: a scoped caller who names no
// consumer. The hidden-consumer probe runs for them, so the flag is set only
// when it finds nothing outside the grant, and the evidence of a granted
// consumer still decides it.
func TestCrossRepoDeadCodeTestOnlyConsumersGrantOnlyRequest(t *testing.T) {
	t.Parallel()

	grant := &auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-producer", "repo-consumer"},
	}
	for _, tt := range []struct {
		name   string
		hidden bool
		want   bool
	}{
		{name: "nothing outside the grant", want: true},
		{name: "a consumer outside the grant", hidden: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := coverageStore([]deadcode.CrossRepoDeadCodeEvidence{rootedConsumer("repo-consumer", "root-a")})
			store.rootPaths = map[string]string{"root-a": "pkg/checkout/charge_test.go"}
			if tt.hidden {
				store.hiddenConsumers = []string{"producer-symbol"}
			}
			status, data := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, grant)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "live_by_consumer", "producer-symbol")
			if got := row["test_only_consumers"] == true; got != tt.want {
				t.Fatalf("test_only_consumers = %#v, want set=%v", row["test_only_consumers"], tt.want)
			}
			if len(store.rootPathCalls) != 1 {
				t.Fatalf("root path reads = %d, want 1 for a grant-only request", len(store.rootPathCalls))
			}
		})
	}
}
