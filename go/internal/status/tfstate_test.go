package status_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportProjectsTerraformStateSerials(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: observedAt,
		// Two locators with multiple generations each — the reader is expected
		// to surface only the latest serial per locator. The projection keeps
		// whatever the reader sent, sorted by safe_locator_hash.
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{
			{
				SafeLocatorHash: "hash-z",
				BackendKind:     "s3",
				Lineage:         "lineage-z",
				Serial:          42,
				GenerationID:    "terraform_state:state_snapshot:s3:hash-z:lineage-z:serial:42",
				ObservedAt:      observedAt,
			},
			{
				SafeLocatorHash: "hash-a",
				BackendKind:     "local",
				Lineage:         "lineage-a",
				Serial:          7,
				GenerationID:    "terraform_state:state_snapshot:local:hash-a:lineage-a:serial:7",
				ObservedAt:      observedAt,
			},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if len(report.TerraformState.LastSerials) != 2 {
		t.Fatalf("LastSerials = %d rows, want 2", len(report.TerraformState.LastSerials))
	}
	if got := report.TerraformState.LastSerials[0].SafeLocatorHash; got != "hash-a" {
		t.Fatalf("LastSerials sorted; first hash = %q, want %q", got, "hash-a")
	}
	if got := report.TerraformState.LastSerials[0].Serial; got != 7 {
		t.Fatalf("LastSerials[0].Serial = %d, want %d", got, 7)
	}
	if got := report.TerraformState.LastSerials[1].SafeLocatorHash; got != "hash-z" {
		t.Fatalf("LastSerials[1].SafeLocatorHash = %q, want %q", got, "hash-z")
	}
	if got := report.TerraformState.LastSerials[1].Serial; got != 42 {
		t.Fatalf("LastSerials[1].Serial = %d, want %d", got, 42)
	}
}

func TestBuildReportGroupsRecentWarningsByLocatorAndKind(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: base,
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "state_in_vcs", Reason: "approved_local", Source: "git_local_file", ObservedAt: base},
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "output_value_dropped", Reason: "sensitive_composite_output", Source: "outputs.x", ObservedAt: base.Add(time.Minute)},
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "state_in_vcs", Reason: "approved_local", Source: "git_local_file", ObservedAt: base.Add(2 * time.Minute)},
			{SafeLocatorHash: "hash-2", BackendKind: "local", WarningKind: "state_too_large", Reason: "exceeded ceiling", Source: "graph", ObservedAt: base.Add(3 * time.Minute)},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if got := len(report.TerraformState.RecentWarnings); got != 4 {
		t.Fatalf("RecentWarnings = %d rows, want 4", got)
	}
	hash1, ok := report.TerraformState.WarningsByKind["hash-1"]
	if !ok {
		t.Fatalf("WarningsByKind missing hash-1; got %v", report.TerraformState.WarningsByKind)
	}
	if got := len(hash1["state_in_vcs"]); got != 2 {
		t.Fatalf("hash-1 state_in_vcs = %d rows, want 2", got)
	}
	if got := len(hash1["output_value_dropped"]); got != 1 {
		t.Fatalf("hash-1 output_value_dropped = %d rows, want 1", got)
	}
	hash2, ok := report.TerraformState.WarningsByKind["hash-2"]
	if !ok {
		t.Fatalf("WarningsByKind missing hash-2; got %v", report.TerraformState.WarningsByKind)
	}
	if got := len(hash2["state_too_large"]); got != 1 {
		t.Fatalf("hash-2 state_too_large = %d rows, want 1", got)
	}
}

func TestRenderJSONIncludesTerraformStateSection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: observedAt,
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{{
			SafeLocatorHash: "abc123",
			BackendKind:     "s3",
			Lineage:         "lineage-1",
			Serial:          5,
			GenerationID:    "terraform_state:state_snapshot:s3:abc123:lineage-1:serial:5",
			ObservedAt:      observedAt,
		}},
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{{
			SafeLocatorHash: "abc123",
			BackendKind:     "s3",
			WarningKind:     "state_in_vcs",
			Reason:          "approved_local",
			Source:          "git_local_file",
			GenerationID:    "terraform_state:state_snapshot:s3:abc123:lineage-1:serial:5",
			ObservedAt:      observedAt,
		}},
	}
	report := status.BuildReport(raw, status.DefaultOptions())
	body, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if !strings.Contains(string(body), `"terraform_state"`) {
		t.Fatalf("RenderJSON() missing terraform_state section: %s", body)
	}

	var decoded struct {
		TerraformState struct {
			LastSerials []struct {
				SafeLocatorHash string `json:"safe_locator_hash"`
				Serial          int64  `json:"serial"`
			} `json:"last_serials"`
			WarningsByKind map[string]map[string][]struct {
				WarningKind string `json:"warning_kind"`
			} `json:"warnings_by_kind"`
		} `json:"terraform_state"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v, body=%s", err, body)
	}
	if len(decoded.TerraformState.LastSerials) != 1 ||
		decoded.TerraformState.LastSerials[0].SafeLocatorHash != "abc123" ||
		decoded.TerraformState.LastSerials[0].Serial != 5 {
		t.Fatalf("decoded last_serials = %+v", decoded.TerraformState.LastSerials)
	}
	if len(decoded.TerraformState.WarningsByKind["abc123"]["state_in_vcs"]) != 1 {
		t.Fatalf("decoded warnings_by_kind = %+v", decoded.TerraformState.WarningsByKind)
	}
}

func TestRenderJSONOmitsTerraformStateWhenEmpty(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC),
	}, status.DefaultOptions())
	body, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if strings.Contains(string(body), `"terraform_state"`) {
		t.Fatalf("RenderJSON() unexpectedly included terraform_state: %s", body)
	}
}

// TestLoadReportSurfacesTerraformStateThroughReader proves the LoadReport path
// passes raw tfstate evidence into the projection unchanged before sorting.
func TestLoadReportSurfacesTerraformStateThroughReader(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)
	reader := &fakeReader{snapshot: status.RawSnapshot{
		AsOf: observedAt,
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{{
			SafeLocatorHash: "hash-zzz", Serial: 1,
		}, {
			SafeLocatorHash: "hash-aaa", Serial: 99,
		}},
	}}
	report, err := status.LoadReport(context.Background(), reader, observedAt, status.DefaultOptions())
	if err != nil {
		t.Fatalf("LoadReport() error = %v, want nil", err)
	}
	if got := report.TerraformState.LastSerials[0].SafeLocatorHash; got != "hash-aaa" {
		t.Fatalf("LoadReport() LastSerials[0] = %q, want %q", got, "hash-aaa")
	}
}
