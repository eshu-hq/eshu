package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestCodeCallProjectionRunnerRecordCycleUsesAcceptanceLogKeys(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf)
	instruments, err := telemetry.NewInstruments(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	runner := CodeCallProjectionRunner{
		Logger:      logger,
		Instruments: instruments,
	}

	err = runner.recordCodeCallCycle(
		context.Background(),
		SharedProjectionAcceptanceKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
		},
		"gen-1",
		2,
		1,
		time.Now().Add(-250*time.Millisecond),
		PartitionProcessResult{
			MaxIntentWaitSeconds:         12.5,
			ProcessingDurationSeconds:    0.25,
			RetractDurationSeconds:       0.10,
			WriteDurationSeconds:         0.12,
			MarkCompletedDurationSeconds: 0.03,
			SelectionDurationSeconds:     0.05,
			LeaseClaimDurationSeconds:    0.01,
			MaxBlockedIntentWaitSeconds:  0,
		},
	)
	if err != nil {
		t.Fatalf("recordCodeCallCycle() error = %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := entry[telemetry.LogKeyAcceptanceScopeID], "scope-a"; got != want {
		t.Fatalf("%s = %v, want %v", telemetry.LogKeyAcceptanceScopeID, got, want)
	}
	if got, want := entry[telemetry.LogKeyAcceptanceUnitID], "repo-a"; got != want {
		t.Fatalf("%s = %v, want %v", telemetry.LogKeyAcceptanceUnitID, got, want)
	}
	if got, want := entry[telemetry.LogKeyAcceptanceSourceRunID], "run-1"; got != want {
		t.Fatalf("%s = %v, want %v", telemetry.LogKeyAcceptanceSourceRunID, got, want)
	}
	if got, want := entry[telemetry.LogKeyAcceptanceGenerationID], "gen-1"; got != want {
		t.Fatalf("%s = %v, want %v", telemetry.LogKeyAcceptanceGenerationID, got, want)
	}
	if _, ok := entry["acceptance_unit_id"]; ok {
		t.Fatalf("unexpected legacy acceptance_unit_id key in log entry: %v", entry["acceptance_unit_id"])
	}
	if got, want := entry["intent_wait_seconds"], 12.5; got != want {
		t.Fatalf("intent_wait_seconds = %v, want %v", got, want)
	}
	if got, want := entry["processing_duration_seconds"], 0.25; got != want {
		t.Fatalf("processing_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["retract_duration_seconds"], 0.10; got != want {
		t.Fatalf("retract_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["write_duration_seconds"], 0.12; got != want {
		t.Fatalf("write_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["mark_completed_duration_seconds"], 0.03; got != want {
		t.Fatalf("mark_completed_duration_seconds = %v, want %v", got, want)
	}
}
