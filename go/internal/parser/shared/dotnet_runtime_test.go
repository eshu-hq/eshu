// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"
)

// TestDotNetRuntimeEvidenceAddCountsEveryCall proves TokenCount measures
// every Add call, including rejected values, so an over_tokens skip carries
// the exact count.
func TestDotNetRuntimeEvidenceAddCountsEveryCall(t *testing.T) {
	t.Parallel()

	evidence := NewDotNetRuntimeEvidence([]byte(`{"sdk":{"version":"8.0.100"}}`))
	evidence.Add("dotnet_global_json_sdk", "8.0.100", 3)
	evidence.Add("dotnet_global_json_sdk", "", 4)
	evidence.Add("dotnet_global_json_sdk", "8.0.100", 0)

	if evidence.TokenCount != 3 {
		t.Fatalf("TokenCount = %d, want 3 (every Add call counts)", evidence.TokenCount)
	}
	if evidence.Rejected != 2 {
		t.Fatalf("Rejected = %d, want 2 (empty value and line 0)", evidence.Rejected)
	}
	if len(evidence.Values) != 1 {
		t.Fatalf("len(Values) = %d, want 1 (only the valid declaration survives)", len(evidence.Values))
	}
	if evidence.State != "parsed_values" {
		t.Fatalf("State = %q, want parsed_values", evidence.State)
	}
}

// TestDotNetRuntimeEvidenceUsesSharedRuntimeVersionTokenCap proves the .NET
// evidence honors the shared RuntimeVersionTokenCap (1024): the measured
// count keeps growing past the cap while retained values are cleared and the
// state becomes over_tokens, mirroring RuntimeEvidence.Add.
func TestDotNetRuntimeEvidenceUsesSharedRuntimeVersionTokenCap(t *testing.T) {
	t.Parallel()

	if RuntimeVersionTokenCap != 1024 {
		t.Fatalf("RuntimeVersionTokenCap = %d, want 1024 (shared cap)", RuntimeVersionTokenCap)
	}

	evidence := NewDotNetRuntimeEvidence([]byte(`<Project></Project>`))
	for i := 0; i < RuntimeVersionTokenCap; i++ {
		evidence.Add("dotnet_csproj_target_framework", "net8.0", i+1)
	}
	if evidence.TokenCount != RuntimeVersionTokenCap {
		t.Fatalf("TokenCount = %d, want %d", evidence.TokenCount, RuntimeVersionTokenCap)
	}
	if evidence.State != "parsed_values" {
		t.Fatalf("State at cap = %q, want parsed_values", evidence.State)
	}
	if len(evidence.Values) != RuntimeVersionTokenCap {
		t.Fatalf("len(Values) at cap = %d, want %d", len(evidence.Values), RuntimeVersionTokenCap)
	}

	evidence.Add("dotnet_csproj_target_framework", "net8.0", RuntimeVersionTokenCap+1)
	if evidence.TokenCount != RuntimeVersionTokenCap+1 {
		t.Fatalf("TokenCount past cap = %d, want %d (calls past the cap still count)",
			evidence.TokenCount, RuntimeVersionTokenCap+1)
	}
	if evidence.State != "over_tokens" {
		t.Fatalf("State past cap = %q, want over_tokens", evidence.State)
	}
	if len(evidence.Values) != 0 {
		t.Fatalf("len(Values) past cap = %d, want 0 (retained values cleared)", len(evidence.Values))
	}

	evidence.Add("dotnet_csproj_target_framework", "", 0)
	if evidence.TokenCount != RuntimeVersionTokenCap+2 {
		t.Fatalf("TokenCount after over_tokens Add = %d, want %d (rejected calls past the cap still count)",
			evidence.TokenCount, RuntimeVersionTokenCap+2)
	}
	if evidence.State != "over_tokens" {
		t.Fatalf("State after over_tokens Add = %q, want over_tokens", evidence.State)
	}
}
