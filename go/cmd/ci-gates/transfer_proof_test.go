// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestTransferProofRoundTripAndStrictShape(t *testing.T) {
	t.Parallel()
	input := testedInput{RunID: 1_000_000_000_000_000, Attempt: 1000, MergeSHA: testedMergeSHA}
	for _, mode := range []string{"full", "reuse", "smoke"} {
		proof := transferProof{Mode: mode, Input: input}
		if mode != "full" {
			proof.FullRootSHA = reuseOldSHA
		}
		text, err := formatTransferProof(proof)
		if err != nil || len(text) > 140 {
			t.Fatalf("mode %s description length=%d err=%v", mode, len(text), err)
		}
		got, ok := parseTransferProof(text)
		if !ok || got.Mode != mode || got.Input.RunID != input.RunID || got.Input.Attempt != input.Attempt || got.Input.MergeSHA != input.MergeSHA || got.FullRootSHA != proof.FullRootSHA {
			t.Fatalf("mode %s round trip = %#v, ok=%v", mode, got, ok)
		}
	}
	valid, _ := formatTransferProof(transferProof{Mode: "reuse", Input: input, FullRootSHA: reuseOldSHA})
	for _, malformed := range []string{
		strings.Replace(valid, " s=", " f=", 1),
		valid + " extra=1",
		valid + "\n",
		strings.Replace(valid, " r=", " r=0", 1),
		strings.Replace(valid, " a=1000", " a=1001", 1),
		strings.Replace(valid, testedMergeSHA, testedMergeSHA[:39], 1),
		strings.Replace(valid, "v3 reuse", "v2 reuse", 1),
	} {
		if _, ok := parseTransferProof(malformed); ok {
			t.Fatalf("malformed proof accepted: %q", malformed)
		}
	}
}
