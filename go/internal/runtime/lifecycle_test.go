package runtime

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestNewLifecycleInitializesTelemetryBootstrap(t *testing.T) {
	t.Parallel()

	got, err := NewLifecycle(Config{
		ServiceName: "collector-git",
		Command:     "collector-git",
		ListenAddr:  "0.0.0.0:8080",
		MetricsAddr: "0.0.0.0:9464",
	})
	if err != nil {
		t.Fatalf("NewLifecycle() error = %v, want nil", err)
	}

	if got.Telemetry.ServiceName != "collector-git" {
		t.Fatalf("Telemetry.ServiceName = %q, want %q", got.Telemetry.ServiceName, "collector-git")
	}

	if got.Telemetry.MeterName != "eshu/go/data-plane" {
		t.Fatalf("Telemetry.MeterName = %q, want %q", got.Telemetry.MeterName, "eshu/go/data-plane")
	}
}

func TestLifecycleStartValidatesTelemetryBootstrap(t *testing.T) {
	t.Parallel()

	got := Lifecycle{
		ServiceName: "collector-git",
		Telemetry:   telemetry.Bootstrap{},
	}

	if err := got.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil, want non-nil")
	}
}
