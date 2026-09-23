// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"strings"
	"testing"
)

// Review finding F4 on 40a4ac36e (verdict-p3.md): the code half.

// TestIPv6InIdentFieldIsPseudonymized (F4 code half): a Route53 AAAA value
// lands in values[] (ClassIdent) and must reach the documentation block, not
// the name:revision branch.
func TestIPv6InIdentFieldIsPseudonymized(t *testing.T) {
	got := pseudonymOf(t, mustKey(t, keyA), "values", "2600:1f18:2f9a:7d00::1")
	if !strings.HasPrefix(got, "2001:db8:") {
		t.Errorf("IPv6 in an ident field has shape %q", shapeOf(got))
	}
}
