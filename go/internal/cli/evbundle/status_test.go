// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

// stubFetcher answers each status path from a canned JSON body, or with a
// canned error. It records the paths it was asked for, in order.
type stubFetcher struct {
	bodies map[string]string
	errs   map[string]error
	asked  []string
}

func (s *stubFetcher) Get(path string, result any) error {
	s.asked = append(s.asked, path)
	if err, ok := s.errs[path]; ok {
		return err
	}
	body, ok := s.bodies[path]
	if !ok {
		return fmt.Errorf("stub has no body for %s", path)
	}
	return json.Unmarshal([]byte(body), result)
}

func fullStatusBodies() map[string]string {
	return map[string]string{
		IndexEndpoint: `{
			"repository_count": 5,
			"queue_blockages": [{"blocked": 3}, {"blocked": 2}, {"blocked": 0}],
			"semantic_extraction": {
				"state": "unavailable",
				"reason": "provider_not_configured",
				"provider_configured": false,
				"provider_profiles": [{"profile_id": "default", "provider_kind": "openai_compatible", "state": "unavailable", "reason": "no_api_key"}]
			}
		}`,
		PipelineEndpoint: `{
			"health": {"state": "degraded", "reasons": ["queue backlog"]},
			"queue": {"total": 18, "outstanding": 7, "overdue_claims": 3, "oldest_outstanding_age": 12.5, "pending": 4, "in_flight": 2, "retrying": 1, "succeeded": 10, "failed": 6, "dead_letter": 1},
			"generation_history": {"active": 1, "pending": 8, "completed": 9, "superseded": 5, "failed": 4, "other": 2},
			"stage_summaries": [{"stage": "parse", "pending": 2, "claimed": 6, "running": 3, "retrying": 7, "succeeded": 11, "failed": 9, "dead_letter": 12}],
			"domain_backlogs": [{"domain": "aws_relationship_materialization", "outstanding": 1, "in_flight": 4, "blocked": 9, "retrying": 13, "failed": 14, "dead_letter": 15, "oldest_age": 41.5}],
			"scope_activity": {"active": 5, "changed": 1, "unchanged": 4}
		}`,
		CollectorsEndpoint: `{"collectors": [{"collector_kind": "git", "status_category": "ready", "health": "healthy"}]}`,
	}
}

// TestFetchLiveSnapshotReadsTheThreeStatusRoutes pins both the routes fetched
// and the decode tags. Every non-zero value in the bodies above is asserted,
// so a mistyped json tag fails here rather than silently zeroing a count in a
// shared artifact.
func TestFetchLiveSnapshotReadsTheThreeStatusRoutes(t *testing.T) {
	fetcher := &stubFetcher{bodies: fullStatusBodies()}

	snapshot, err := FetchLiveSnapshot(fetcher)
	if err != nil {
		t.Fatalf("FetchLiveSnapshot() error = %v", err)
	}

	wantAsked := []string{IndexEndpoint, PipelineEndpoint, CollectorsEndpoint}
	if !reflect.DeepEqual(fetcher.asked, wantAsked) {
		t.Fatalf("fetched %v, want %v", fetcher.asked, wantAsked)
	}

	want := evidencebundle.LiveSnapshot{
		RepositoryCount: 5,
		HealthState:     "degraded",
		HealthReasons:   []string{"queue backlog"},
		// 3 + 2, not 2: Blocked is a count of gated rows, not a flag.
		QueueBlockedCount: 5,
		Queue: evidencebundle.LiveQueueSnapshot{
			Total: 18, Outstanding: 7, OverdueClaims: 3, OldestOutstandingAgeS: 12.5,
			Pending: 4, InFlight: 2, Retrying: 1, Succeeded: 10, Failed: 6, DeadLetter: 1,
		},
		ScopeActivity: evidencebundle.LiveScopeActivitySnapshot{Active: 5, Changed: 1, Unchanged: 4},
		GenerationHistory: evidencebundle.LiveGenerationHistorySnapshot{
			Active: 1, Pending: 8, Completed: 9, Superseded: 5, Failed: 4, Other: 2,
		},
		StageSummaries: []evidencebundle.LiveStageSummarySnapshot{{
			Stage: "parse", Pending: 2, Claimed: 6, Running: 3, Retrying: 7,
			Succeeded: 11, Failed: 9, DeadLetter: 12,
		}},
		DomainBacklogs: []evidencebundle.LiveDomainBacklogSnapshot{{
			Domain: "aws_relationship_materialization", Outstanding: 1, InFlight: 4,
			Blocked: 9, Retrying: 13, Failed: 14, DeadLetter: 15, OldestAgeS: 41.5,
		}},
		Collectors: []evidencebundle.LiveCollectorSnapshot{{
			CollectorKind: "git", StatusCategory: "ready", Health: "healthy",
		}},
		SemanticExtraction: evidencebundle.LiveSemanticExtractionSnapshot{
			State:              "unavailable",
			Reason:             "provider_not_configured",
			ProviderConfigured: false,
			ProviderProfiles: []evidencebundle.LiveSemanticProviderProfileSnapshot{{
				ProfileID: "default", ProviderKind: "openai_compatible",
				State: "unavailable", Reason: "no_api_key",
			}},
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot mismatch\n got: %+v\nwant: %+v", snapshot, want)
	}
}

// TestFetchLiveSnapshotFailsOnAnyRoute proves a failing route aborts the
// whole fetch and names the route. Composing from a partial fetch would
// publish zero counts as observed truth.
func TestFetchLiveSnapshotFailsOnAnyRoute(t *testing.T) {
	for _, route := range []string{IndexEndpoint, PipelineEndpoint, CollectorsEndpoint} {
		t.Run(route, func(t *testing.T) {
			sentinel := errors.New("status reader not configured")
			fetcher := &stubFetcher{
				bodies: fullStatusBodies(),
				errs:   map[string]error{route: sentinel},
			}
			snapshot, err := FetchLiveSnapshot(fetcher)
			if err == nil {
				t.Fatalf("FetchLiveSnapshot() error = nil, want a failure for %s", route)
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("error %v does not wrap the route error", err)
			}
			if got := err.Error(); got != fmt.Sprintf("fetch %s: %s", route, sentinel) {
				t.Fatalf("error = %q, want it to name %s", got, route)
			}
			if !reflect.DeepEqual(snapshot, evidencebundle.LiveSnapshot{}) {
				t.Fatalf("snapshot = %+v on failure, want the zero value", snapshot)
			}
		})
	}
}

// TestQueueBlockedCountSumsGatedRows covers the summing rule directly:
// counting entries would report a single heavily-gated domain as 1.
func TestQueueBlockedCountSumsGatedRows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		blockages []QueueBlockage
		want      int
	}{
		{"none", nil, 0},
		{"single_heavily_gated", []QueueBlockage{{Blocked: 40}}, 40},
		{"summed_not_counted", []QueueBlockage{{Blocked: 3}, {Blocked: 2}}, 5},
		{"zero_entries_ignored", []QueueBlockage{{Blocked: 0}, {Blocked: 4}}, 4},
		{"negative_ignored", []QueueBlockage{{Blocked: -7}, {Blocked: 4}}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := LiveSnapshotFromStatus(
				IndexStatus{QueueBlockages: tc.blockages},
				PipelineStatus{},
				CollectorsResponse{},
			)
			if snapshot.QueueBlockedCount != tc.want {
				t.Fatalf("QueueBlockedCount = %d, want %d", snapshot.QueueBlockedCount, tc.want)
			}
		})
	}
}

// TestLiveSnapshotCopiesHealthReasons proves the snapshot does not alias the
// caller's slice: a later append by the caller must not rewrite evidence that
// has already been captured.
func TestLiveSnapshotCopiesHealthReasons(t *testing.T) {
	reasons := make([]string, 1, 2)
	reasons[0] = "queue backlog"
	snapshot := LiveSnapshotFromStatus(
		IndexStatus{},
		PipelineStatus{Health: PipelineHealth{Reasons: reasons}},
		CollectorsResponse{},
	)
	reasons[0] = "rewritten after capture"
	if snapshot.HealthReasons[0] != "queue backlog" {
		t.Fatalf("HealthReasons[0] = %q, want the value captured at map time", snapshot.HealthReasons[0])
	}
}

// TestLiveSnapshotFromStatusEmitsEmptySlicesNotNil keeps the rendered bundle
// stable for a stack with no stages, domains, collectors, or profiles.
func TestLiveSnapshotFromStatusEmitsEmptySlicesNotNil(t *testing.T) {
	snapshot := LiveSnapshotFromStatus(IndexStatus{}, PipelineStatus{}, CollectorsResponse{})
	if snapshot.StageSummaries == nil || snapshot.DomainBacklogs == nil ||
		snapshot.Collectors == nil || snapshot.SemanticExtraction.ProviderProfiles == nil {
		t.Fatalf("empty status produced nil slices: %+v", snapshot)
	}
}

// TestLiveSnapshotCarriesDomainBacklogTruncation pins the flag the status
// layer sets when it capped the domain list. Without it a bundle presents a
// partial enumeration as complete, and the API route
// (internal/query/evidence_bundle_live.go) already carries it, so dropping it
// here also splits the two readings of one stack.
func TestLiveSnapshotCarriesDomainBacklogTruncation(t *testing.T) {
	for _, truncated := range []bool{true, false} {
		t.Run(fmt.Sprintf("truncated=%v", truncated), func(t *testing.T) {
			snapshot := LiveSnapshotFromStatus(
				IndexStatus{},
				PipelineStatus{DomainBacklogsTruncated: truncated},
				CollectorsResponse{},
			)
			if snapshot.DomainBacklogsTruncated != truncated {
				t.Fatalf("DomainBacklogsTruncated = %v, want %v", snapshot.DomainBacklogsTruncated, truncated)
			}
		})
	}
}

// TestFetchLiveSnapshotDecodesDomainBacklogTruncation covers the json tag,
// which the mapping test above cannot: the status route serializes the flag
// as domain_backlogs_truncated (internal/status/json.go).
func TestFetchLiveSnapshotDecodesDomainBacklogTruncation(t *testing.T) {
	bodies := fullStatusBodies()
	bodies[PipelineEndpoint] = `{"health": {"state": "degraded"}, "domain_backlogs_truncated": true}`
	snapshot, err := FetchLiveSnapshot(&stubFetcher{bodies: bodies})
	if err != nil {
		t.Fatalf("FetchLiveSnapshot() error = %v", err)
	}
	if !snapshot.DomainBacklogsTruncated {
		t.Fatal("DomainBacklogsTruncated = false; the status route's domain_backlogs_truncated was dropped")
	}
}
