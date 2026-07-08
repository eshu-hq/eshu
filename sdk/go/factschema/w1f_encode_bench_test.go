// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"testing"

	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

func BenchmarkW1fEncodeNoRegression(b *testing.B) {
	b.Run("sbom_document", func(b *testing.B) {
		document := sbomv1.Document{
			DocumentID:         "sbom-document://repo/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DocumentDigest:     stringPtr("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			SubjectDigest:      stringPtr("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
			SubjectDigests:     []string{"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
			ParseStatus:        stringPtr("parsed"),
			VerificationStatus: stringPtr("verified"),
			VerificationPolicy: stringPtr("slsa"),
			Format:             stringPtr("cyclonedx"),
			SpecVersion:        stringPtr("1.6"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeSBOMDocument(document)
			if err != nil {
				b.Fatalf("EncodeSBOMDocument() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("ci_run", func(b *testing.B) {
		run := cicdrunv1.Run{
			Provider:           "github_actions",
			RunID:              "17290001",
			RunAttempt:         stringPtr("2"),
			RunNumber:          stringPtr("438"),
			WorkflowName:       stringPtr("deploy"),
			Event:              stringPtr("push"),
			Status:             stringPtr("completed"),
			Result:             stringPtr("success"),
			Branch:             stringPtr("main"),
			CommitSHA:          stringPtr("0123456789abcdef0123456789abcdef01234567"),
			RepositoryID:       stringPtr("github.com/example/api"),
			RepositoryURL:      stringPtr("https://github.com/example/api"),
			Actor:              stringPtr("deployer"),
			StartedAt:          stringPtr("2026-07-08T01:02:03Z"),
			UpdatedAt:          stringPtr("2026-07-08T01:05:03Z"),
			URL:                stringPtr("https://github.com/example/api/actions/runs/17290001"),
			CorrelationAnchors: []string{"github.com/example/api", "0123456789abcdef0123456789abcdef01234567", "17290001"},
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeCICDRun(run)
			if err != nil {
				b.Fatalf("EncodeCICDRun() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("work_item_record", func(b *testing.B) {
		present := true
		record := workitemv1.WorkItemRecord{
			Provider:               "jira_cloud",
			ProviderWorkItemID:     "10042",
			WorkItemKey:            "OPS-42",
			RedactionPolicyVersion: stringPtr("2026-07"),
			Summary:                stringPtr(""),
			SummaryPresent:         &present,
			IssueTypeID:            stringPtr("10001"),
			IssueTypeName:          stringPtr("Incident"),
			StatusID:               stringPtr("3"),
			StatusName:             stringPtr("In Progress"),
			ProjectID:              stringPtr("10000"),
			ProjectKey:             stringPtr("OPS"),
			ProjectName:            stringPtr(""),
			ProjectNamePresent:     &present,
			AssigneeAccountID:      stringPtr(""),
			AssigneeDisplayName:    stringPtr(""),
			AssigneePresent:        &present,
			ReporterAccountID:      stringPtr(""),
			ReporterDisplayName:    stringPtr(""),
			ReporterPresent:        &present,
			CreatedAt:              stringPtr("2026-07-08T00:00:00Z"),
			UpdatedAt:              stringPtr("2026-07-08T01:00:00Z"),
			SelfURL:                stringPtr(""),
			SelfURLFingerprint:     stringPtr("sha256:self"),
			SourceURL:              stringPtr(""),
			SourceURLFingerprint:   stringPtr("sha256:source"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeWorkItemRecord(record)
			if err != nil {
				b.Fatalf("EncodeWorkItemRecord() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("incident_record", func(b *testing.B) {
		incidentNumber := int64(1234)
		service := incidentv1.ServiceReference{
			ID:      stringPtr("P123"),
			Type:    stringPtr("service_reference"),
			Summary: stringPtr("api"),
			URL:     stringPtr("https://example.pagerduty.com/services/P123"),
		}
		record := incidentv1.IncidentRecord{
			Provider:           "pagerduty",
			ProviderIncidentID: "Q123",
			IncidentNumber:     &incidentNumber,
			Title:              stringPtr("API latency"),
			Status:             stringPtr("resolved"),
			Urgency:            stringPtr("high"),
			ServiceID:          stringPtr("P123"),
			Service:            &service,
			Teams:              []incidentv1.ServiceReference{service},
			Assignments:        []incidentv1.ServiceReference{service},
			CreatedAt:          stringPtr("2026-07-08T01:00:00Z"),
			UpdatedAt:          stringPtr("2026-07-08T01:10:00Z"),
			ResolvedAt:         stringPtr("2026-07-08T01:20:00Z"),
			SourceURL:          stringPtr("https://example.pagerduty.com/incidents/Q123"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeIncidentRecord(record)
			if err != nil {
				b.Fatalf("EncodeIncidentRecord() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("incident_routing_applied_alert_route", func(b *testing.B) {
		serial := int64(22)
		route := incidentv1.AppliedAlertRoute{
			SourceClass:                "applied",
			SourceKind:                 "terraform_state",
			Outcome:                    "applied",
			TerraformStateAddress:      "aws_cloudwatch_event_rule.incident",
			ResourceType:               "aws_cloudwatch_event_rule",
			ResourceName:               "incident",
			ModuleAddress:              "",
			ProviderAddress:            "registry.terraform.io/hashicorp/aws",
			ScopeID:                    "tfstate://prod",
			StateGenerationID:          "generation-1",
			StateLineage:               "lineage-1",
			BackendKind:                "s3",
			LocatorHash:                "sha256:locator",
			DeclaredMatchState:         "not_compared",
			RedactionState:             "redacted",
			RouteType:                  "event_rule",
			StateSerial:                &serial,
			AWSARN:                     stringPtr("arn:aws:events:us-east-1:123456789012:rule/incident"),
			TargetReferenceKind:        stringPtr("pagerduty_integration_key"),
			TargetReferenceFingerprint: stringPtr("sha256:target"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeIncidentRoutingAppliedAlertRoute(route)
			if err != nil {
				b.Fatalf("EncodeIncidentRoutingAppliedAlertRoute() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("security_alert_repository_alert", func(b *testing.B) {
		alertNumber := int64(7)
		pagesFetched := int64(2)
		truncated := false
		alert := securityalertv1.RepositoryAlert{
			RepositoryID:            "github.com/example/api",
			Provider:                stringPtr("github_dependabot"),
			ProviderAlertID:         stringPtr("dependabot-7"),
			ProviderAlertNumber:     &alertNumber,
			ProviderState:           stringPtr("open"),
			PackageID:               stringPtr("npm://lodash"),
			Ecosystem:               stringPtr("npm"),
			PackageName:             stringPtr("lodash"),
			ManifestPath:            stringPtr("package-lock.json"),
			DependencyScope:         stringPtr("runtime"),
			Relationship:            stringPtr("direct"),
			GHSAIDs:                 []string{"GHSA-xxxx-yyyy-zzzz"},
			CVEIDs:                  []string{"CVE-2026-0001"},
			VulnerableRange:         stringPtr("<4.17.21"),
			PatchedVersion:          stringPtr("4.17.21"),
			Severity:                stringPtr("high"),
			CVSS:                    map[string]any{"score": 7.5, "vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N"},
			EPSS:                    map[string]string{"percentage": "0.42"},
			CWEs:                    []map[string]string{{"id": "CWE-79", "name": "Cross-site Scripting"}},
			Summary:                 stringPtr("Prototype pollution"),
			SourceURL:               stringPtr("https://github.com/example/api/security/dependabot/7"),
			CreatedAt:               stringPtr("2026-07-08T00:00:00Z"),
			UpdatedAt:               stringPtr("2026-07-08T01:00:00Z"),
			SourceFreshness:         stringPtr("current"),
			CollectionCoverageState: stringPtr("complete"),
			CollectionTruncated:     &truncated,
			CollectionPagesFetched:  &pagesFetched,
			CollectionStateFilter:   stringPtr("open"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeSecurityAlertRepositoryAlert(alert)
			if err != nil {
				b.Fatalf("EncodeSecurityAlertRepositoryAlert() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})
}
