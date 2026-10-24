// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestCoordinatorSnapshotJSONOmitsRawTerminalFailureClass pins that the
// snapshot carries the persisted terminal failure class only in memory (#8803).
// The slice holds the class before BoundedFailureClass runs, so a marshaled
// snapshot must not expose it: the status surfaces bound it first.
func TestCoordinatorSnapshotJSONOmitsRawTerminalFailureClass(t *testing.T) {
	t.Parallel()

	const rawClass = "raw_unbounded_class_text"
	snapshot := CoordinatorSnapshot{
		CollectorTerminalFailures: []collector.TerminalFailure{{
			CollectorKind:    "security_alert",
			InstanceID:       "instance-a",
			FailureClass:     rawClass,
			FailureOperation: "list_org_alerts",
			ObservedAt:       time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),
		}},
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(CoordinatorSnapshot) error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), rawClass) {
		t.Fatalf("marshaled snapshot = %s, want no raw terminal failure class", encoded)
	}
	if strings.Contains(string(encoded), "collector_terminal_failures") {
		t.Fatalf("marshaled snapshot = %s, want no collector_terminal_failures key", encoded)
	}
}
