// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accessanalyzer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAccessAnalyzerMetadataOnlyFactsAndRelationships(t *testing.T) {
	externalARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/account-external"
	unusedARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/org-unused"
	client := fakeClient{analyzers: []Analyzer{{
		ARN:                    externalARN,
		Name:                   "account-external",
		Type:                   "ACCOUNT",
		Status:                 "ACTIVE",
		CreatedAt:              time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
		LastResourceAnalyzed:   "arn:aws:s3:::prod-bucket",
		LastResourceAnalyzedAt: time.Date(2026, 5, 27, 10, 15, 0, 0, time.UTC),
		Tags:                   map[string]string{"Environment": "prod"},
		ArchiveRules: []ArchiveRule{{
			Name:        "archive-known-shared-bucket",
			AnalyzerARN: externalARN,
			CreatedAt:   time.Date(2026, 5, 27, 10, 20, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 27, 10, 30, 0, 0, time.UTC),
		}},
		FindingCounts: []FindingCount{{
			Status:       "ACTIVE",
			ResourceType: "AWS::S3::Bucket",
			Count:        2,
		}, {
			Status:       "ARCHIVED",
			ResourceType: "AWS::IAM::Role",
			Count:        1,
		}},
	}, {
		ARN:                    unusedARN,
		Name:                   "org-unused",
		Type:                   "ORGANIZATION_UNUSED_ACCESS",
		Status:                 "ACTIVE",
		CreatedAt:              time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC),
		LastResourceAnalyzedAt: time.Date(2026, 5, 27, 11, 15, 0, 0, time.UTC),
		FindingCounts: []FindingCount{{
			Status:       "RESOLVED",
			ResourceType: "AWS::IAM::User",
			Count:        4,
		}},
		UnusedAccessSummaries: []UnusedAccessSummary{{
			FindingID:            "unused-permission-1",
			FindingType:          "UnusedPermission",
			ResourceID:           "arn:aws:iam::123456789012:role/stale-admin",
			ResourceOwnerAccount: "123456789012",
			ResourceType:         "AWS::IAM::Role",
			Status:               "ACTIVE",
			LastAccessedAt:       time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
			AnalyzedAt:           time.Date(2026, 5, 27, 11, 20, 0, 0, time.UTC),
			UpdatedAt:            time.Date(2026, 5, 27, 11, 25, 0, 0, time.UTC),
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerAnalyzer, 2)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerArchiveRule, 1)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerFindingCount, 3)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerUnusedAccessSummary, 1)
	assertRelationshipType(t, envelopes, awscloud.RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount)
	assertRelationshipType(t, envelopes, awscloud.RelationshipAccessAnalyzerAnalyzerHasArchiveRule)

	analyzer := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAccessAnalyzerAnalyzer, unusedARN)
	attrs := attributesOf(t, analyzer)
	if got, want := attrs["scope"], "ORGANIZATION"; got != want {
		t.Fatalf("scope = %#v, want %q", got, want)
	}
	if got, want := attrs["analysis_type"], "unused_access"; got != want {
		t.Fatalf("analysis_type = %#v, want %q", got, want)
	}

	findingCount := resourceByTypeAndID(
		t,
		envelopes,
		awscloud.ResourceTypeAccessAnalyzerFindingCount,
		externalARN+"/finding-count/ACTIVE/AWS::S3::Bucket",
	)
	if got, want := attributesOf(t, findingCount)["count"], int64(2); got != want {
		t.Fatalf("finding count = %#v, want %d", got, want)
	}

	unused := resourceByTypeAndID(
		t,
		envelopes,
		awscloud.ResourceTypeAccessAnalyzerUnusedAccessSummary,
		unusedARN+"/unused-access/unused-permission-1",
	)
	unusedAttrs := attributesOf(t, unused)
	if got, want := unusedAttrs["last_accessed_at"], time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC); got != want {
		t.Fatalf("last_accessed_at = %#v, want %v", got, want)
	}
	assertNoForbiddenPayloadKeys(t, envelopes)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerSkipsAnalyzerChildrenWhenAnalyzerARNMissing(t *testing.T) {
	client := fakeClient{analyzers: []Analyzer{{
		Name:   "missing-arn",
		Type:   "ACCOUNT",
		Status: "ACTIVE",
		ArchiveRules: []ArchiveRule{{
			Name: "archive-rule-without-analyzer",
		}},
		FindingCounts: []FindingCount{{
			Status:       "ACTIVE",
			ResourceType: "AWS::S3::Bucket",
			Count:        1,
		}},
		UnusedAccessSummaries: []UnusedAccessSummary{{
			FindingID:    "unused-1",
			ResourceID:   "arn:aws:iam::123456789012:role/stale-admin",
			ResourceType: "AWS::IAM::Role",
			Status:       "ACTIVE",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerAnalyzer, 1)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerArchiveRule, 0)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerFindingCount, 0)
	assertResourceCount(t, envelopes, awscloud.ResourceTypeAccessAnalyzerUnusedAccessSummary, 0)
	assertNoResourceIDPrefix(t, envelopes, "/")
}

func TestScannerEmitsWarnings(t *testing.T) {
	client := fakeClient{analyzers: []Analyzer{{
		ARN:    "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/unused",
		Name:   "unused",
		Type:   "ACCOUNT_UNUSED_ACCESS",
		Status: "ACTIVE",
		Warnings: []awscloud.WarningObservation{{
			WarningKind: awscloud.WarningBudgetExhausted,
			ErrorClass:  "unused_access_detail_budget_exhausted",
			Message:     "unused access detail reads exceeded the bounded Access Analyzer detail-read budget",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertWarningKind(t, envelopes, awscloud.WarningBudgetExhausted)
}

func TestScannerMapsSupportedAnalyzerScopesAndAnalysisTypes(t *testing.T) {
	tests := []struct {
		name             string
		analyzerType     string
		wantScope        string
		wantAnalysisType string
	}{
		{
			name:             "account external",
			analyzerType:     "ACCOUNT",
			wantScope:        "ACCOUNT",
			wantAnalysisType: "external_access",
		},
		{
			name:             "organization external",
			analyzerType:     "ORGANIZATION",
			wantScope:        "ORGANIZATION",
			wantAnalysisType: "external_access",
		},
		{
			name:             "account unused",
			analyzerType:     "ACCOUNT_UNUSED_ACCESS",
			wantScope:        "ACCOUNT",
			wantAnalysisType: "unused_access",
		},
		{
			name:             "organization unused",
			analyzerType:     "ORGANIZATION_UNUSED_ACCESS",
			wantScope:        "ORGANIZATION",
			wantAnalysisType: "unused_access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzerARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/" + tt.name
			client := fakeClient{analyzers: []Analyzer{{
				ARN:    analyzerARN,
				Name:   tt.name,
				Type:   tt.analyzerType,
				Status: "ACTIVE",
			}}}

			envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
			if err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}

			analyzer := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAccessAnalyzerAnalyzer, analyzerARN)
			attrs := attributesOf(t, analyzer)
			if got := attrs["scope"]; got != tt.wantScope {
				t.Fatalf("scope = %#v, want %q", got, tt.wantScope)
			}
			if got := attrs["analysis_type"]; got != tt.wantAnalysisType {
				t.Fatalf("analysis_type = %#v, want %q", got, tt.wantAnalysisType)
			}
		})
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAccessAnalyzer,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:accessanalyzer:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	analyzers []Analyzer
}

func (c fakeClient) ListAnalyzers(context.Context) ([]Analyzer, error) {
	return c.analyzers, nil
}

func assertResourceCount(t *testing.T, envelopes []facts.Envelope, resourceType string, want int) {
	t.Helper()
	var got int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			got++
		}
	}
	if got != want {
		t.Fatalf("resource_type %q count = %d, want %d", resourceType, got, want)
	}
}

func assertRelationshipType(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func assertWarningKind(t *testing.T, envelopes []facts.Envelope, warningKind string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
}

func assertNoResourceIDPrefix(t *testing.T, envelopes []facts.Envelope, prefix string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resourceID, _ := envelope.Payload["resource_id"].(string)
		if len(resourceID) >= len(prefix) && resourceID[:len(prefix)] == prefix {
			t.Fatalf("resource_id %q has forbidden prefix %q", resourceID, prefix)
		}
	}
}

func resourceByTypeAndID(t *testing.T, envelopes []facts.Envelope, resourceType string, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType && envelope.Payload["resource_id"] == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q resource_id %q in %#v", resourceType, resourceID, envelopes)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertNoForbiddenPayloadKeys(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	forbidden := map[string]bool{
		"actions":              true,
		"condition":            true,
		"filter":               true,
		"generated_policy":     true,
		"input":                true,
		"policy_generation":    true,
		"principal":            true,
		"resource_policy":      true,
		"sources":              true,
		"unused_action_detail": true,
	}
	for _, envelope := range envelopes {
		assertMapExcludesKeys(t, envelope.Payload, forbidden)
	}
}

func assertMapExcludesKeys(t *testing.T, value any, forbidden map[string]bool) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if forbidden[key] {
				t.Fatalf("forbidden key %q persisted in %#v", key, typed)
			}
			assertMapExcludesKeys(t, child, forbidden)
		}
	case []any:
		for _, child := range typed {
			assertMapExcludesKeys(t, child, forbidden)
		}
	}
}
