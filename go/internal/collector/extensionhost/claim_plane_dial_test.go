// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestDialClaimPlaneSharesOneClient proves both binding faces hold the
// same client instance: the fence must pin the claim the control store
// fenced first.
func TestDialClaimPlaneSharesOneClient(t *testing.T) {
	t.Parallel()

	binding := DialClaimPlane("http://claim-plane:8080", nil)
	if binding.ControlStore == nil || binding.Pinner == nil {
		t.Fatalf("DialClaimPlane returned nil face: %+v", binding)
	}
	control, ok := binding.ControlStore.(*workflow.ClaimPlaneClient)
	if !ok {
		t.Fatalf("ControlStore has unexpected type %T", binding.ControlStore)
	}
	pinner, ok := binding.Pinner.(*workflow.ClaimPlaneClient)
	if !ok {
		t.Fatalf("Pinner has unexpected type %T", binding.Pinner)
	}
	if control != pinner {
		t.Errorf("binding faces hold different clients: %p vs %p", control, pinner)
	}
}
