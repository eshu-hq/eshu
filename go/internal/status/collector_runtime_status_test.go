package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestRenderStatusIncludesCollectorRuntimeCategories(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 10, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "aws-coordinator",
					CollectorKind:  "aws",
					Mode:           "continuous",
					Enabled:        true,
					Bootstrap:      true,
					ClaimsEnabled:  true,
					LastObservedAt: now.Add(-5 * time.Minute),
					UpdatedAt:      now.Add(-4 * time.Minute),
				}},
			},
			AWSCloudScans: []status.AWSCloudScanStatus{{
				CollectorInstanceID: "aws-direct",
				AccountID:           "123456789012",
				Region:              "us-east-1",
				ServiceKind:         "ecr",
				Status:              "succeeded",
				CommitStatus:        "committed",
				LastObservedAt:      now.Add(-2 * time.Minute),
				UpdatedAt:           now.Add(-1 * time.Minute),
			}},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"collector_runtimes\"",
		"\"instance_id\": \"aws-coordinator\"",
		"\"status_category\": \"coordinator_managed\"",
		"\"instance_id\": \"aws-direct\"",
		"\"status_category\": \"unregistered\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() = %s, want %q", payload, want)
		}
	}

	text := status.RenderText(report)
	if !strings.Contains(text, "Collector runtimes:") ||
		!strings.Contains(text, "aws-coordinator kind=aws category=coordinator_managed") ||
		!strings.Contains(text, "aws-direct kind=aws category=unregistered") {
		t.Fatalf("RenderText() = %s, want collector runtime categories", text)
	}
}

func TestCollectorRuntimeStatusesClassifiesCoordinatorModes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 11, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{
					{
						InstanceID:     "package-registry-direct",
						CollectorKind:  "package_registry",
						Mode:           "manual",
						Enabled:        true,
						ClaimsEnabled:  false,
						LastObservedAt: now,
						UpdatedAt:      now,
					},
					{
						InstanceID:     "jira-disabled",
						CollectorKind:  "jira",
						Mode:           "scheduled",
						Enabled:        false,
						ClaimsEnabled:  false,
						LastObservedAt: now,
						UpdatedAt:      now,
					},
				},
			},
		},
		status.DefaultOptions(),
	)

	byID := map[string]status.CollectorRuntimeStatus{}
	for _, row := range status.CollectorRuntimeStatuses(report) {
		byID[row.InstanceID] = row
	}
	if got, want := byID["package-registry-direct"].StatusCategory, status.CollectorRuntimeDirectMode; got != want {
		t.Fatalf("direct status category = %q, want %q", got, want)
	}
	if got, want := byID["package-registry-direct"].RuntimeMode, "direct"; got != want {
		t.Fatalf("direct runtime mode = %q, want %q", got, want)
	}
	if got, want := byID["jira-disabled"].StatusCategory, status.CollectorRuntimeDisabled; got != want {
		t.Fatalf("disabled status category = %q, want %q", got, want)
	}
	if got, want := byID["jira-disabled"].RuntimeMode, "registration_only"; got != want {
		t.Fatalf("disabled runtime mode = %q, want %q", got, want)
	}
}

func TestCollectorRuntimeStatusesMergesDirectEvidenceHealthForRegisteredCollector(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "aws-shared",
					CollectorKind:  "aws",
					Mode:           "continuous",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: now.Add(-10 * time.Minute),
					UpdatedAt:      now.Add(-9 * time.Minute),
				}},
			},
			AWSCloudScans: []status.AWSCloudScanStatus{{
				CollectorInstanceID: "aws-shared",
				AccountID:           "123456789012",
				Region:              "us-east-1",
				ServiceKind:         "ec2",
				Status:              "failed_retryable",
				FailureClass:        "throttled",
				LastObservedAt:      now.Add(-2 * time.Minute),
				UpdatedAt:           now.Add(-1 * time.Minute),
			}},
		},
		status.DefaultOptions(),
	)

	rows := status.CollectorRuntimeStatuses(report)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("runtime rows = %d, want %d: %#v", got, want, rows)
	}
	row := rows[0]
	if got, want := row.StatusCategory, status.CollectorRuntimeCoordinatorManaged; got != want {
		t.Fatalf("status category = %q, want %q", got, want)
	}
	if got, want := row.Health, "degraded"; got != want {
		t.Fatalf("health = %q, want %q", got, want)
	}
	if got, want := row.ObservationCount, 1; got != want {
		t.Fatalf("observation count = %d, want %d", got, want)
	}
	for _, want := range []string{"workflow_coordinator", "aws_cloud_scan_status"} {
		if !collectorRuntimeEvidenceContains(row.EvidenceSources, want) {
			t.Fatalf("evidence sources = %#v, want %q", row.EvidenceSources, want)
		}
	}
}

func collectorRuntimeEvidenceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
