// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestRenderStatusExplainsAFencedInfraReadModel pins the admin status field
// for the infra read model (#6793): it names the read model state and, when
// fenced, the marked repository count and the oldest mark's age, so an
// operator can see why unscoped infra aggregate reads are on the graph.
func TestRenderStatusExplainsAFencedInfraReadModel(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		InfraInventory: status.InfraInventorySnapshot{
			Reported:       true,
			State:          "fenced",
			MarkerPresent:  true,
			DirtyRepos:     4,
			OldestDirtyAge: 3 * time.Minute,
		},
	}, status.DefaultOptions())

	text := status.RenderText(report)
	for _, want := range []string{"Infra read model:", "state=fenced", "dirty_repos=4", "oldest_dirty=3m0s"} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q:\n%s", want, text)
		}
	}

	encoded, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var payload struct {
		InfraInventory *struct {
			State                   string  `json:"state"`
			MarkerPresent           bool    `json:"marker_present"`
			DirtyRepos              int64   `json:"dirty_repos"`
			OldestDirtyAgeSeconds   float64 `json:"oldest_dirty_age_seconds"`
			ReadsServedFromGraphFor string  `json:"reads_served_from_graph_because"`
		} `json:"infra_inventory"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	got := payload.InfraInventory
	if got == nil {
		t.Fatalf("infra_inventory missing from JSON: %s", encoded)
	}
	if got.State != "fenced" || !got.MarkerPresent || got.DirtyRepos != 4 || got.OldestDirtyAgeSeconds != 180 {
		t.Fatalf("infra_inventory = %+v", *got)
	}
	if !strings.Contains(got.ReadsServedFromGraphFor, "fence") {
		t.Fatalf("reads_served_from_graph_because = %q, want the fence named", got.ReadsServedFromGraphFor)
	}

	ready := status.BuildReport(status.RawSnapshot{InfraInventory: status.InfraInventorySnapshot{
		Reported: true, State: "ready", MarkerPresent: true,
	}}, status.DefaultOptions())
	encoded, err = status.RenderJSON(ready)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	payload.InfraInventory = nil
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.InfraInventory == nil || payload.InfraInventory.State != "ready" ||
		payload.InfraInventory.ReadsServedFromGraphFor != "" {
		t.Fatalf("ready infra_inventory JSON = %s, want state ready and no graph reason", encoded)
	}
}
