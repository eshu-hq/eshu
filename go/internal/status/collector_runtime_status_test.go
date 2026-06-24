// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"encoding/json"
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
	if !strings.Contains(row.Detail, "aws_cloud_scan_status") ||
		!strings.Contains(row.Detail, "failure_class=throttled") {
		t.Fatalf("detail = %q, want AWS failure reason and evidence source", row.Detail)
	}
	for _, want := range []string{"workflow_coordinator", "aws_cloud_scan_status"} {
		if !collectorRuntimeEvidenceContains(row.EvidenceSources, want) {
			t.Fatalf("evidence sources = %#v, want %q", row.EvidenceSources, want)
		}
	}
}

func TestCollectorRuntimeStatusesTreatsCommittedSuccessfulAWSScanAsObserved(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 13, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "collector-aws-claims",
					CollectorKind:  "aws",
					Mode:           "continuous",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: now.Add(-20 * time.Minute),
					UpdatedAt:      now.Add(-19 * time.Minute),
				}},
			},
			AWSCloudScans: []status.AWSCloudScanStatus{{
				CollectorInstanceID: "collector-aws-claims",
				AccountID:           "123456789012",
				Region:              "us-east-1",
				ServiceKind:         "iam",
				Status:              "succeeded",
				CommitStatus:        "committed",
				FailureClass:        "commit_failure",
				FailureMessage:      "older commit attempt failed before retry",
				LastObservedAt:      now.Add(-4 * time.Minute),
				LastCompletedAt:     now.Add(-3 * time.Minute),
				LastSuccessfulAt:    now.Add(-3 * time.Minute),
				UpdatedAt:           now.Add(-3 * time.Minute),
			}},
			CollectorFactEvidence: []status.CollectorFactEvidence{
				{
					InstanceID:       "collector-aws-claims",
					CollectorKind:    "aws",
					EvidenceSource:   "source_facts",
					SourceSystems:    []string{"aws"},
					ObservationCount: 17,
					LastObservedAt:   now.Add(-3 * time.Minute),
					UpdatedAt:        now.Add(-2 * time.Minute),
				},
				{
					InstanceID:       "collector-aws-claims",
					CollectorKind:    "aws",
					EvidenceSource:   "reducer_facts",
					SourceSystems:    []string{"aws"},
					ObservationCount: 6,
					LastObservedAt:   now.Add(-2 * time.Minute),
					UpdatedAt:        now.Add(-1 * time.Minute),
				},
			},
		},
		status.DefaultOptions(),
	)

	rows := status.CollectorRuntimeStatuses(report)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("runtime rows = %d, want %d: %#v", got, want, rows)
	}
	row := rows[0]
	if got, want := row.Health, "observed"; got != want {
		t.Fatalf("health = %q, want %q", got, want)
	}
	if got, want := row.StatusCategory, status.CollectorRuntimeCoordinatorManaged; got != want {
		t.Fatalf("status category = %q, want %q", got, want)
	}
	for _, want := range []string{"workflow_coordinator", "aws_cloud_scan_status", "source_facts", "reducer_facts"} {
		if !collectorRuntimeEvidenceContains(row.EvidenceSources, want) {
			t.Fatalf("evidence sources = %#v, want %q", row.EvidenceSources, want)
		}
	}
}

func TestCollectorRuntimeStatusesMergesPersistedFactEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 9, 30, 0, 0, time.UTC)
	instances := []status.CollectorInstanceSummary{
		{InstanceID: "collector-documentation", CollectorKind: "documentation", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-git-default", CollectorKind: "git", Mode: "continuous", Enabled: true, ClaimsEnabled: false},
		{InstanceID: "collector-jira", CollectorKind: "jira", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-pagerduty", CollectorKind: "pagerduty", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-oci", CollectorKind: "oci_registry", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-package-registry", CollectorKind: "package_registry", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-sbom", CollectorKind: "sbom_attestation", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-security-alert", CollectorKind: "security_alert", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-terraform-state", CollectorKind: "terraform_state", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-aws", CollectorKind: "aws", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
		{InstanceID: "collector-vulnerability", CollectorKind: "vulnerability_intelligence", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
	}
	for i := range instances {
		instances[i].LastObservedAt = now.Add(-20 * time.Minute)
		instances[i].UpdatedAt = now.Add(-19 * time.Minute)
	}

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: instances,
			},
			CollectorFactEvidence: []status.CollectorFactEvidence{
				{InstanceID: "collector-documentation", CollectorKind: "documentation", EvidenceSource: "source_facts", SourceSystems: []string{"confluence"}, ObservationCount: 5, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-git-default", CollectorKind: "git", EvidenceSource: "source_facts", SourceSystems: []string{"git"}, ObservationCount: 17, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "source_facts", ObservationCount: 7, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-pagerduty", CollectorKind: "pagerduty", EvidenceSource: "source_facts", ObservationCount: 4, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-oci", CollectorKind: "oci_registry", EvidenceSource: "source_facts", ObservationCount: 3, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-package-registry", CollectorKind: "package_registry", EvidenceSource: "source_facts", ObservationCount: 6, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-sbom", CollectorKind: "sbom_attestation", EvidenceSource: "source_facts", ObservationCount: 2, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-sbom", CollectorKind: "sbom_attestation", EvidenceSource: "reducer_facts", ObservationCount: 1, LastObservedAt: now.Add(-8 * time.Minute), UpdatedAt: now.Add(-7 * time.Minute)},
				{InstanceID: "collector-security-alert", CollectorKind: "security_alert", EvidenceSource: "source_facts", ObservationCount: 8, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-security-alert", CollectorKind: "security_alert", EvidenceSource: "reducer_facts", ObservationCount: 2, LastObservedAt: now.Add(-8 * time.Minute), UpdatedAt: now.Add(-7 * time.Minute)},
				{InstanceID: "collector-terraform-state", CollectorKind: "terraform_state", EvidenceSource: "source_facts", ObservationCount: 9, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-aws", CollectorKind: "aws", EvidenceSource: "source_facts", ObservationCount: 11, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
				{InstanceID: "collector-aws", CollectorKind: "aws", EvidenceSource: "reducer_facts", ObservationCount: 3, LastObservedAt: now.Add(-8 * time.Minute), UpdatedAt: now.Add(-7 * time.Minute)},
				{InstanceID: "collector-vulnerability", CollectorKind: "vulnerability_intelligence", EvidenceSource: "source_facts", ObservationCount: 13, LastObservedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-9 * time.Minute)},
			},
		},
		status.DefaultOptions(),
	)

	byID := map[string]status.CollectorRuntimeStatus{}
	for _, row := range status.CollectorRuntimeStatuses(report) {
		byID[row.InstanceID] = row
	}
	for _, instance := range instances {
		row := byID[instance.InstanceID]
		wantCategory := status.CollectorRuntimeCoordinatorManaged
		if !instance.ClaimsEnabled {
			wantCategory = status.CollectorRuntimeDirectMode
		}
		if got, want := row.StatusCategory, wantCategory; got != want {
			t.Fatalf("%s status category = %q, want %q", instance.InstanceID, got, want)
		}
		if got, want := row.Health, "observed"; got != want {
			t.Fatalf("%s health = %q, want %q", instance.InstanceID, got, want)
		}
		if !collectorRuntimeEvidenceContains(row.EvidenceSources, "workflow_coordinator") ||
			!collectorRuntimeEvidenceContains(row.EvidenceSources, "source_facts") {
			t.Fatalf("%s evidence sources = %#v, want workflow_coordinator and source_facts", instance.InstanceID, row.EvidenceSources)
		}
		if row.ObservationCount == 0 {
			t.Fatalf("%s observation count = 0, want persisted fact count", instance.InstanceID)
		}
	}
	for _, instanceID := range []string{"collector-sbom", "collector-security-alert", "collector-aws"} {
		row := byID[instanceID]
		if !collectorRuntimeEvidenceContains(row.EvidenceSources, "reducer_facts") {
			t.Fatalf("%s evidence sources = %#v, want reducer_facts", instanceID, row.EvidenceSources)
		}
	}
	documentation := byID["collector-documentation"]
	if !collectorRuntimeEvidenceContains(documentation.SourceSystems, "confluence") {
		t.Fatalf("documentation source systems = %#v, want confluence", documentation.SourceSystems)
	}

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	var rendered struct {
		CollectorRuntimes []struct {
			InstanceID    string   `json:"instance_id"`
			SourceSystems []string `json:"source_systems"`
		} `json:"collector_runtimes"`
	}
	if err := json.Unmarshal(payload, &rendered); err != nil {
		t.Fatalf("json.Unmarshal(RenderJSON()) error = %v, want nil", err)
	}
	if !renderedCollectorHasSourceSystem(rendered.CollectorRuntimes, "collector-documentation", "confluence") {
		t.Fatalf("RenderJSON() = %s, want collector-documentation source_systems=confluence", payload)
	}
	if !renderedCollectorHasSourceSystem(rendered.CollectorRuntimes, "collector-git-default", "git") {
		t.Fatalf("RenderJSON() = %s, want collector-git-default source_systems=git", payload)
	}

	text := status.RenderText(report)
	if !strings.Contains(text, "collector-documentation kind=documentation") ||
		!strings.Contains(text, "source_systems=confluence") {
		t.Fatalf("RenderText() = %s, want documentation source_systems", text)
	}
	if !strings.Contains(text, "collector-git-default kind=git") ||
		!strings.Contains(text, "source_systems=git") ||
		!strings.Contains(text, "observations=17") {
		t.Fatalf("RenderText() = %s, want Git source_systems and observations", text)
	}
}

func TestCollectorRuntimeStatusesMapsUnattributedFactsToSingleCoordinatorInstance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "collector-aws-claims",
					CollectorKind:  "aws",
					Mode:           "continuous",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: now.Add(-20 * time.Minute),
					UpdatedAt:      now.Add(-19 * time.Minute),
				}},
			},
			AWSCloudScans: []status.AWSCloudScanStatus{{
				CollectorInstanceID: "collector-aws-direct",
				AccountID:           "123456789012",
				Region:              "us-east-1",
				ServiceKind:         "lambda",
				Status:              "succeeded",
				CommitStatus:        "committed",
				LastObservedAt:      now.Add(-5 * time.Minute),
				UpdatedAt:           now.Add(-4 * time.Minute),
			}},
			CollectorFactEvidence: []status.CollectorFactEvidence{{
				CollectorKind:    "aws",
				EvidenceSource:   "source_facts",
				ObservationCount: 9,
				LastObservedAt:   now.Add(-3 * time.Minute),
				UpdatedAt:        now.Add(-2 * time.Minute),
			}},
		},
		status.DefaultOptions(),
	)

	byID := map[string]status.CollectorRuntimeStatus{}
	for _, row := range status.CollectorRuntimeStatuses(report) {
		byID[row.InstanceID] = row
	}
	coordinator := byID["collector-aws-claims"]
	if got, want := coordinator.StatusCategory, status.CollectorRuntimeCoordinatorManaged; got != want {
		t.Fatalf("coordinator status category = %q, want %q", got, want)
	}
	if got, want := coordinator.Health, "observed"; got != want {
		t.Fatalf("coordinator health = %q, want %q", got, want)
	}
	if got, want := coordinator.ObservationCount, 9; got != want {
		t.Fatalf("coordinator observation count = %d, want %d", got, want)
	}
	if !collectorRuntimeEvidenceContains(coordinator.EvidenceSources, "source_facts") {
		t.Fatalf("coordinator evidence sources = %#v, want source_facts", coordinator.EvidenceSources)
	}
	if _, ok := byID["aws-persisted-facts"]; ok {
		t.Fatal("persisted fact evidence created synthetic aws-persisted-facts row; want merge into single coordinator instance")
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

func renderedCollectorHasSourceSystem(
	rows []struct {
		InstanceID    string   `json:"instance_id"`
		SourceSystems []string `json:"source_systems"`
	},
	instanceID string,
	sourceSystem string,
) bool {
	for _, row := range rows {
		if row.InstanceID != instanceID {
			continue
		}
		return collectorRuntimeEvidenceContains(row.SourceSystems, sourceSystem)
	}
	return false
}
