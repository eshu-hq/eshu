// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "testing"

func TestPublisherReuseDescriptionRequiresBoundV3Proof(t *testing.T) {
	check := RequiredStatusCheck{Context: "required-gates-complete"}
	descriptions := map[int]string{
		awaitExitPassedCode:            "full",
		awaitExitGateFailedCode:        "failure",
		awaitExitGateCancelledCode:     "cancelled",
		awaitExitReusedCode:            "Tree-identical reuse: verdict copied from a previous head (see run log)",
		awaitExitSmokePassedCode:       "smoke",
		publisherUnclassifiedProbeCode: "broken",
	}
	if errs := publishedDescriptionErrors(check, descriptions); len(errs) == 0 {
		t.Fatal("generic historical reuse description must fail publisher validation")
	}
	descriptions[awaitExitReusedCode] = publisherProbeReuseProof
	descriptions[awaitExitSmokePassedCode] = publisherProbeSmokeProof
	if errs := publishedDescriptionErrors(check, descriptions); len(errs) != 0 {
		t.Fatalf("versioned full-backed description rejected: %v", errs)
	}
}
