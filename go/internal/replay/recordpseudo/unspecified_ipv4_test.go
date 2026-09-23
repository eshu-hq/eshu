// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// TestVerifyAdmitsTheUnspecifiedAddress: 0.0.0.0 (and the 0.0.0.0/0 "any
// address" CIDR a security-group rule carries) is kept by the pseudonymizer
// and is not private data, so Verify admits it exactly -- a neighbouring
// address is still refused.
func TestVerifyAdmitsTheUnspecifiedAddress(t *testing.T) {
	key := mustKey(t, keyA)
	if got := pseudonymOf(t, key, "source_value", "0.0.0.0/0"); got != "0.0.0.0/0" {
		t.Fatalf("0.0.0.0/0 was rewritten to shape %q", shapeOf(got))
	}
	doc := []byte(`{"source_value":"0.0.0.0/0","bind":"0.0.0.0"}`)
	if err := recordpseudo.Verify(doc, recordpseudo.Set{}); err != nil {
		t.Fatalf("the unspecified address was refused: %v", err)
	}
	err := recordpseudo.Verify([]byte(`{"source_value":"0.0.0.1/32"}`), recordpseudo.Set{})
	if err == nil || !strings.Contains(err.Error(), "alternative ipv4") {
		t.Fatalf("a neighbour of the unspecified address passed: %v", err)
	}
}
