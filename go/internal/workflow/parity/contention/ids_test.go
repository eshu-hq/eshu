// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"strings"
	"testing"
)

// TestInstanceIDIsOutsideTheCensusInstanceFamily pins the arbiter (b2)
// rename: the harness seed instance keeps the synthetic Prefix but lives
// outside the collector_instances census family (instance-%), so the
// satellite-side declaration row for it never trips the baseline gate.
// The LIKE pattern carries no wildcard besides the trailing %, and the id
// carries no LIKE metacharacter, so the Go prefix check is exactly the
// SQL family match.
func TestInstanceIDIsOutsideTheCensusInstanceFamily(t *testing.T) {
	if InstanceID != Prefix+"leg-1" {
		t.Fatalf("InstanceID = %q, want %q", InstanceID, Prefix+"leg-1")
	}
	if !IsSynthetic(InstanceID) {
		t.Fatalf("InstanceID %q lost the synthetic prefix %q", InstanceID, Prefix)
	}
	if strings.ContainsAny(InstanceID, "%_") {
		t.Fatalf("InstanceID %q carries a LIKE metacharacter", InstanceID)
	}
	if strings.HasPrefix(InstanceID, strings.TrimSuffix(instanceFamily, "%")) {
		t.Fatalf("InstanceID %q matches the census instance family %q", InstanceID, instanceFamily)
	}
}
