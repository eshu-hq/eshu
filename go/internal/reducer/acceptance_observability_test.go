package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestSharedAcceptanceTelemetryRecordStaleIntents(t *testing.T) {
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

	sharedAcceptanceTelemetry{
		Instruments: instruments,
		Logger:      logger,
	}.RecordStaleIntents(context.Background(), "code_call_projection", DomainCodeCalls, 3)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got, want := entry["acceptance.stale_count"], float64(3); got != want {
		t.Fatalf("acceptance stale count = %v, want %v", got, want)
	}
	if got, want := entry[telemetry.LogKeyDomain], string(DomainCodeCalls); got != want {
		t.Fatalf("domain = %v, want %v", got, want)
	}
	if got, want := entry["runner"], "code_call_projection"; got != want {
		t.Fatalf("runner = %v, want %v", got, want)
	}
	if got, want := entry[telemetry.LogKeyPipelinePhase], telemetry.PhaseShared; got != want {
		t.Fatalf("pipeline_phase = %v, want %v", got, want)
	}
}

func TestSharedAcceptanceTelemetryRecordLookupError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf)

	sharedAcceptanceTelemetry{
		Logger: logger,
	}.RecordLookup(context.Background(), sharedAcceptanceLookupEvent{
		Runner:   "shared_projection",
		Result:   "error",
		Duration: 0.12,
		Err:      assertableError("lookup failed"),
	})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got, want := entry["runner"], "shared_projection"; got != want {
		t.Fatalf("runner = %v, want %v", got, want)
	}
	if got, want := entry["lookup_result"], "error"; got != want {
		t.Fatalf("lookup_result = %v, want %v", got, want)
	}
	if got, want := entry["error_type"], "lookup_failed"; got != want {
		t.Fatalf("error_type = %v, want %v", got, want)
	}
	if got, want := entry[telemetry.LogKeyFailureClass], "shared_acceptance_lookup_error"; got != want {
		t.Fatalf("failure_class = %v, want %v", got, want)
	}
}

type assertableError string

func (e assertableError) Error() string { return string(e) }
