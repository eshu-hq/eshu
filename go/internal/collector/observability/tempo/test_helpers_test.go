// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"fmt"
	"strings"
	"testing"
)

func assertPayloadOmitsString(t *testing.T, payload any, forbidden string) {
	t.Helper()
	if forbidden == "" {
		return
	}
	if strings.Contains(fmt.Sprintf("%#v", payload), forbidden) {
		t.Fatalf("payload leaked forbidden string %q: %#v", forbidden, payload)
	}
}
