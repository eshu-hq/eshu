// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// coordinatorSourceRoutes are the routes that render the coordinator section
// and so carry coordinator_source (#8828).
var coordinatorSourceRoutes = []string{
	"/api/v0/status/pipeline",
	"/api/v0/status/index",
	"/api/v0/index-status",
	"/api/v0/ingesters/repository",
}

func coordinatorSourcedSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		ActiveWorkSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceLive, Reason: statuspkg.ActiveWorkReasonFlagOff, AsOf: asOf,
		},
		CoordinatorSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceModel, Reason: statuspkg.ActiveWorkReasonFresh,
			AsOf: asOf.Add(-12 * time.Second), Age: 12 * time.Second,
		},
	}
}

func TestStatusRoutesCarryTheCoordinatorSource(t *testing.T) {
	t.Parallel()
	for _, path := range coordinatorSourceRoutes {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			payload := getStatusPayload(t, coordinatorSourcedSnapshot(), path)
			source, ok := payload["coordinator_source"].(map[string]any)
			if !ok {
				t.Fatalf("GET %s has no coordinator_source object: %#v", path, payload)
			}
			if source["source"] != "model" || source["reason"] != "fresh" || source["stale"] != false ||
				source["as_of"] != "2026-10-25T09:29:48Z" || source["age_seconds"] != float64(12) || len(source) != 5 {
				t.Fatalf("GET %s coordinator_source = %#v", path, source)
			}
			if active, _ := payload["active_work_source"].(map[string]any); active["source"] != "live" {
				t.Fatalf("GET %s active_work_source = %#v, want it to stay live: the two markers are independent", path, payload["active_work_source"])
			}
		})
	}
}

func TestStatusRoutesOmitTheCoordinatorSourceWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()
	for _, path := range coordinatorSourceRoutes {
		payload := getStatusPayload(t, statuspkg.RawSnapshot{AsOf: time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)}, path)
		if _, present := payload["coordinator_source"]; present {
			t.Fatalf("GET %s emitted coordinator_source for a reader that reports none", path)
		}
	}
}

func TestHostedReadinessCarriesTheCoordinatorSource(t *testing.T) {
	t.Parallel()
	handler := &StatusHandler{StatusReader: &selectionRecordingReader{snapshot: coordinatorSourcedSnapshot()}}
	payload := hostedReadinessPayloadFor(t, handler)
	source, ok := payload["coordinator_source"].(map[string]any)
	if !ok || source["source"] != "model" || source["age_seconds"] != float64(12) {
		t.Fatalf("hosted readiness coordinator_source = %#v", payload["coordinator_source"])
	}
}
