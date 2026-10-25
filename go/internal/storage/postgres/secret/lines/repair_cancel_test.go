// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestRepairerCancelledTickLogsTheErrorText proves the cancelled outcome keeps
// the error that was reclassified (#8833 PR2b review F12). A release failure
// runs on a context that survives the cancel, so its text is a real "the lock
// was not held" signal and must not be dropped with the cancel.
func TestRepairerCancelledTickLogsTheErrorText(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := NewRepairer(&failingDatabase{}, nil, RepairOptions{Logger: logger})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome, err := r.Tick(ctx)
	if outcome != RepairCancelled || err != nil {
		t.Fatalf("Tick = %s, %v; want cancelled and no error", outcome, err)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if record["msg"] != "secret lines repair cancelled by shutdown" {
			continue
		}
		found = true
		if record["level"] != "WARN" {
			t.Errorf("cancelled event level = %v, want WARN", record["level"])
		}
		if text, _ := record["error"].(string); !strings.Contains(text, "database down") {
			t.Errorf("cancelled event error = %q, want the underlying error text", text)
		}
	}
	if !found {
		t.Fatalf("no cancelled event in %s", logs.String())
	}
}
