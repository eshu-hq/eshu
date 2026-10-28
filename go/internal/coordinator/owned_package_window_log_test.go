// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestLogOwnedPackageWindowWritesTheSlotStats pins the #9422 QA readback
// contract: one line per read with T, P, the slot, its size, the rows read and
// the selected count per ecosystem.
func TestLogOwnedPackageWindowWritesTheSlotStats(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	service := Service{Logger: slog.New(slog.NewJSONHandler(&buffer, nil))}
	instance := workflow.CollectorInstance{
		InstanceID:    "vuln-intel-activation",
		CollectorKind: scope.CollectorVulnerabilityIntelligence,
	}
	page := workflow.OwnedPackageDependencyTargetPage{
		Targets: []workflow.OwnedPackageDependencyTarget{
			{Ecosystem: "npm"}, {Ecosystem: "rubygems"}, {Ecosystem: "npm"},
		},
		Window: workflow.OwnedPackageDependencyTargetWindow{
			TotalTargets: 30264, SlotCount: 32, Slot: 22, SlotSize: 3, RowsRead: 3,
		},
	}
	service.logOwnedPackageWindow(instance, true, page)

	var line struct {
		Msg                 string   `json:"msg"`
		TotalTargets        int64    `json:"total_targets"`
		SlotCount           int64    `json:"slot_count"`
		Slot                int64    `json:"slot"`
		SlotSize            int64    `json:"slot_size"`
		RowsRead            int64    `json:"rows_read"`
		Truncated           bool     `json:"truncated"`
		Selected            int      `json:"selected"`
		SelectedEcosystems  []string `json:"selected_ecosystems"`
		SelectedByEcosystem []int    `json:"selected_by_ecosystem"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &line); err != nil {
		t.Fatalf("log line %q: %v", buffer.String(), err)
	}
	if line.Msg != "workflow coordinator read owned package target window" ||
		line.TotalTargets != 30264 || line.SlotCount != 32 || line.Slot != 22 ||
		line.SlotSize != 3 || line.RowsRead != 3 || line.Truncated || line.Selected != 3 ||
		!slices.Equal(line.SelectedEcosystems, []string{"npm", "rubygems"}) ||
		!slices.Equal(line.SelectedByEcosystem, []int{2, 1}) {
		t.Fatalf("log line = %+v (%s)", line, buffer.String())
	}

	buffer.Reset()
	service.logOwnedPackageWindow(instance, false, workflow.OwnedPackageDependencyTargetPage{})
	if buffer.Len() != 0 {
		t.Fatalf("disabled derivation logged %q, want nothing", buffer.String())
	}
}
