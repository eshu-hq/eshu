// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestBuildLiveBundleCarriesTheActiveWorkSource proves the bundle carries the
// stored-summary marker (#7660) when the snapshot reports one, and omits the
// key otherwise so demo bundles and the CLI export path stay unchanged.
func TestBuildLiveBundleCarriesTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	marker := status.ActiveWorkSource{
		Source: status.ActiveWorkSourceModel, Reason: status.ActiveWorkReasonFresh,
		AsOf: asOf, Age: 12 * time.Second,
	}.JSON()

	snapshot := LiveSnapshot{RepositoryCount: 5, ActiveWorkSource: marker}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate() error = %v, want nil for a bundle carrying the marker", err)
	}
	if bundle.ActiveWorkSource == nil {
		t.Fatal("Bundle.ActiveWorkSource = nil, want the snapshot marker carried")
	}
	if *bundle.ActiveWorkSource != *marker {
		t.Fatalf("Bundle.ActiveWorkSource = %+v, want %+v", bundle.ActiveWorkSource, marker)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	got, ok := decoded["active_work_source"].(map[string]any)
	if !ok {
		t.Fatalf("bundle JSON has no active_work_source object: %s", raw)
	}
	if got["source"] != "model" || got["reason"] != "fresh" ||
		got["as_of"] != "2026-10-08T12:00:00Z" || got["age_seconds"] != float64(12) ||
		got["stale"] != false {
		t.Fatalf("active_work_source = %#v", got)
	}

	// The same input must hash identically on a rebuild, marker included.
	again := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
	if again.BundleID != bundle.BundleID {
		t.Fatalf("bundle_id not stable across rebuilds: %q vs %q", bundle.BundleID, again.BundleID)
	}

	// No marker in, no key out: demo bundles and marker-less callers keep
	// their existing shape and bundle_id inputs.
	bare := BuildLiveBundle(LiveSnapshot{RepositoryCount: 5}, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
	if bare.ActiveWorkSource != nil {
		t.Fatalf("Bundle.ActiveWorkSource = %+v, want nil for a snapshot without one", bare.ActiveWorkSource)
	}
	bareRaw, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(bareRaw), "active_work_source") {
		t.Fatalf("bundle JSON carries active_work_source without a snapshot marker: %s", bareRaw)
	}
}
