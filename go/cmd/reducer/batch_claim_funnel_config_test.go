// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestLoadReducerBatchClaimFunnelEnabledDefaultsOff(t *testing.T) {
	t.Parallel()

	got, err := loadReducerBatchClaimFunnelEnabled(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadReducerBatchClaimFunnelEnabled() error = %v", err)
	}
	if got {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() = true, want false (default off until the QA-lane delta proof, #10565)")
	}
}

func TestLoadReducerBatchClaimFunnelEnabledReadsTrue(t *testing.T) {
	t.Parallel()

	got, err := loadReducerBatchClaimFunnelEnabled(func(k string) string {
		if k == reducerBatchClaimFunnelEnabledEnv {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadReducerBatchClaimFunnelEnabled() error = %v", err)
	}
	if !got {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() = false, want true")
	}
}

func TestLoadReducerBatchClaimFunnelEnabledRejectsUnparsable(t *testing.T) {
	t.Parallel()

	_, err := loadReducerBatchClaimFunnelEnabled(func(k string) string {
		if k == reducerBatchClaimFunnelEnabledEnv {
			return "yes-please"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() error = nil, want startup error")
	}
	if !strings.Contains(err.Error(), reducerBatchClaimFunnelEnabledEnv) {
		t.Fatalf("error %q does not name %s", err, reducerBatchClaimFunnelEnabledEnv)
	}
}
