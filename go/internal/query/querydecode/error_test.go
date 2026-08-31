// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querydecode

import (
	"strings"
	"testing"
)

// Exporting this type so root can alias it also lets any importer build one
// from the exported fields alone. New always sets the unexported wrapped error,
// but a struct literal cannot, and Error()/Unwrap() must not panic on that
// value. A panic here surfaces as a 500 on a read path whose whole purpose is
// to degrade one malformed fact gracefully.
func TestZeroValueErrorDoesNotPanic(t *testing.T) {
	e := &Error{FactKind: "work_item", FactID: "fact-123"}

	msg := e.Error()
	if !strings.Contains(msg, "work_item") || !strings.Contains(msg, "fact-123") {
		t.Fatalf("Error() = %q, want it to name the fact kind and id", msg)
	}
	if got := e.Unwrap(); got != nil {
		t.Fatalf("Unwrap() = %v, want nil for a value with no wrapped error", got)
	}
}
