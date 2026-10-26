// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func attrKey(k string) attribute.Key { return attribute.Key(k) }

// logLineForStage returns the decoded JSON log record of the content writer
// stage line for stage.
func logLineForStage(t *testing.T, logs, stage string) map[string]any {
	t.Helper()
	for _, raw := range strings.Split(strings.TrimSpace(logs), "\n") {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			continue
		}
		if line["stage"] == stage {
			return line
		}
	}
	t.Fatalf("no %q stage log line in:\n%s", stage, logs)
	return nil
}
