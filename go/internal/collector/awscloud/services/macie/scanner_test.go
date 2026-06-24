// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package macie

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsSessionMembersJobsAllowListsIdentifiersAndFilters(t *testing.T) {
	client := &fakeClient{
		session: Session{
			Enabled:                    true,
			Status:                     "ENABLED",
			FindingPublishingFrequency: "FIFTEEN_MINUTES",
			ServiceRoleARN:             "arn:aws:iam::123456789012:role/aws-service-role/macie.amazonaws.com/AWSServiceRoleForAmazonMacie",
			CreatedAt:                  "2026-05-01T00:00:00Z",
			UpdatedAt:                  "2026-05-20T00:00:00Z",
		},
		severityCounts: map[string]int64{"Low": 3, "Medium": 1, "High": 0},
		members: []MemberAccount{{
			AccountID:          "111122223333",
			AdministratorID:    "123456789012",
			RelationshipStatus: "Enabled",
			InvitedAt:          "2026-05-02T00:00:00Z",
			UpdatedAt:          "2026-05-03T00:00:00Z",
			Tags:               map[string]string{"Team": "security"},
		}},
		jobs: []ClassificationJob{{
			JobID:             "job-abc",
			Name:              "weekly-pii-scan",
			JobType:           "SCHEDULED",
			JobStatus:         "RUNNING",
			CreatedAt:         "2026-05-04T00:00:00Z",
			BucketCount:       7,
			AccountCount:      2,
			HasBucketCriteria: true,
		}},
		allowLists: []AllowList{{
			ID:   "allow-1",
			Name: "approved-test-data",
		}},
		identifiers: []CustomDataIdentifier{{
			ID:   "cdi-1",
			Name: "internal-employee-id",
		}},
		filters: []FindingsFilter{{
			ID:     "filter-1",
			Name:   "suppress-known-benign",
			Action: "ARCHIVE",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	session := resourceByType(t, envelopes, awscloud.ResourceTypeMacieSession)
	sessionAttrs := attributesOf(t, session)
	if got, want := sessionAttrs["enabled"], true; got != want {
		t.Fatalf("session enabled = %#v, want %v", got, want)
	}
	if got, want := sessionAttrs["status"], "ENABLED"; got != want {
		t.Fatalf("session status = %#v, want %q", got, want)
	}
	counts, ok := sessionAttrs["finding_counts_by_severity"].(map[string]int64)
	if !ok {
		t.Fatalf("finding_counts_by_severity = %#v, want map[string]int64", sessionAttrs["finding_counts_by_severity"])
	}
	if counts["Low"] != 3 || counts["Medium"] != 1 {
		t.Fatalf("finding counts = %#v, want Low=3 Medium=1", counts)
	}

	member := resourceByType(t, envelopes, awscloud.ResourceTypeMacieMemberAccount)
	memberAttrs := attributesOf(t, member)
	if got, want := memberAttrs["account_id"], "111122223333"; got != want {
		t.Fatalf("member account_id = %#v, want %q", got, want)
	}
	// Member email is personal contact data and must never appear.
	for _, forbidden := range []string{"email", "email_address"} {
		if _, exists := memberAttrs[forbidden]; exists {
			t.Fatalf("member attribute %q persisted; Macie members omit email", forbidden)
		}
	}

	job := resourceByType(t, envelopes, awscloud.ResourceTypeMacieClassificationJob)
	jobAttrs := attributesOf(t, job)
	if got, want := jobAttrs["job_status"], "RUNNING"; got != want {
		t.Fatalf("job status = %#v, want %q", got, want)
	}
	if got, want := jobAttrs["target_bucket_count"], 7; got != want {
		t.Fatalf("job target_bucket_count = %#v, want %d", got, want)
	}
	if got, want := jobAttrs["target_account_count"], 2; got != want {
		t.Fatalf("job target_account_count = %#v, want %d", got, want)
	}
	// The bucket-criteria summary is an aggregate count and a boolean flag only.
	// No bucket list and no criteria expression may appear on the job.
	for _, forbidden := range []string{"buckets", "bucket_list", "bucket_definitions", "bucket_criteria", "criteria"} {
		if _, exists := jobAttrs[forbidden]; exists {
			t.Fatalf("job attribute %q persisted; classification jobs omit bucket lists and criteria", forbidden)
		}
	}

	allowList := resourceByType(t, envelopes, awscloud.ResourceTypeMacieAllowList)
	allowListAttrs := attributesOf(t, allowList)
	if got, want := allowList.Payload["name"], "approved-test-data"; got != want {
		t.Fatalf("allow list name = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"contents", "regex", "criteria", "description", "s3"} {
		if _, exists := allowListAttrs[forbidden]; exists {
			t.Fatalf("allow list attribute %q persisted; allow lists are identity-only", forbidden)
		}
	}

	identifier := resourceByType(t, envelopes, awscloud.ResourceTypeMacieCustomDataIdentifier)
	identifierAttrs := attributesOf(t, identifier)
	if got, want := identifier.Payload["name"], "internal-employee-id"; got != want {
		t.Fatalf("identifier name = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"regex", "keywords", "ignore_words", "description", "maximum_match_distance"} {
		if _, exists := identifierAttrs[forbidden]; exists {
			t.Fatalf("identifier attribute %q persisted; custom data identifiers are identity-only", forbidden)
		}
	}

	filter := resourceByType(t, envelopes, awscloud.ResourceTypeMacieFindingsFilter)
	filterAttrs := attributesOf(t, filter)
	if got, want := filter.Payload["name"], "suppress-known-benign"; got != want {
		t.Fatalf("filter name = %#v, want %q", got, want)
	}
	if got, want := filterAttrs["action"], "ARCHIVE"; got != want {
		t.Fatalf("filter action = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"criteria", "finding_criteria", "description"} {
		if _, exists := filterAttrs[forbidden]; exists {
			t.Fatalf("filter attribute %q persisted; findings filters omit criteria", forbidden)
		}
	}

	// Member-to-administrator edge targets the administrator session resource.
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipMacieMemberManagedByAdministrator)
	if got, want := relationship.Payload["target_resource_id"], sessionResourceID("123456789012"); got != want {
		t.Fatalf("member relationship target = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_type"], awscloud.ResourceTypeMacieSession; got != want {
		t.Fatalf("member relationship target_type = %#v, want %q", got, want)
	}
}

// TestScannerStructTypesCannotHoldSensitivePayloads is the scanner-side
// redaction gate. It feeds the most sensitive fields Macie reports (custom data
// identifier regular expressions, allow-list contents, findings filter
// criteria, classification-job bucket lists and bucket criteria, and
// sensitive-data finding details) and asserts the scanner-owned domain types
// have NO field that could land them. The types carry identity and counts only,
// so the sensitive data is unreachable by construction, not by careful mapping.
func TestScannerStructTypesCannotHoldSensitivePayloads(t *testing.T) {
	cases := []struct {
		name      string
		value     any
		forbidden []string
	}{
		{
			name:      "CustomDataIdentifier",
			value:     CustomDataIdentifier{},
			forbidden: []string{"Regex", "Keywords", "IgnoreWords", "MaximumMatchDistance", "Description"},
		},
		{
			name:      "AllowList",
			value:     AllowList{},
			forbidden: []string{"Regex", "Criteria", "S3WordsList", "Contents", "Description"},
		},
		{
			name:      "FindingsFilter",
			value:     FindingsFilter{},
			forbidden: []string{"Criteria", "FindingCriteria", "Description"},
		},
		{
			name:      "ClassificationJob",
			value:     ClassificationJob{},
			forbidden: []string{"Buckets", "BucketDefinitions", "BucketCriteria", "Criteria", "S3JobDefinition"},
		},
		{
			name:      "MemberAccount",
			value:     MemberAccount{},
			forbidden: []string{"Email", "EmailAddress"},
		},
		{
			name:      "Session",
			value:     Session{},
			forbidden: []string{"Finding", "Findings", "SensitiveData", "Occurrences"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ := reflect.TypeOf(tc.value)
			for _, banned := range tc.forbidden {
				if _, ok := typ.FieldByName(banned); ok {
					t.Fatalf("%s exposes field %q; Macie scanner types must not be able to hold that payload", tc.name, banned)
				}
			}
		})
	}
}

func TestScannerEmitsDisabledSessionAndStopsForDisabledAccount(t *testing.T) {
	client := &fakeClient{
		session: Session{Enabled: false, Status: "DISABLED"},
		// These would be a bug to read for a disabled account; the scanner must
		// not enumerate them, so populated fakes prove the early return.
		members:     []MemberAccount{{AccountID: "111122223333"}},
		jobs:        []ClassificationJob{{JobID: "job-x"}},
		allowLists:  []AllowList{{ID: "allow-x"}},
		identifiers: []CustomDataIdentifier{{ID: "cdi-x"}},
		filters:     []FindingsFilter{{ID: "filter-x"}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("envelopes = %d, want 1 disabled session only", len(envelopes))
	}
	session := resourceByType(t, envelopes, awscloud.ResourceTypeMacieSession)
	if got := attributesOf(t, session)["enabled"]; got != false {
		t.Fatalf("session enabled = %#v, want false", got)
	}
	if client.memberCalls != 0 || client.jobCalls != 0 || client.findingCalls != 0 {
		t.Fatalf("disabled account triggered downstream reads: members=%d jobs=%d findings=%d", client.memberCalls, client.jobCalls, client.findingCalls)
	}
}

func TestScannerEmitsNoMemberRelationshipsForStandaloneAccount(t *testing.T) {
	client := &fakeClient{session: Session{Enabled: true, Status: "ENABLED"}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipMacieMemberManagedByAdministrator {
			t.Fatalf("standalone account emitted a member relationship: %#v", envelope)
		}
	}
}

// TestScannerRelationshipTargetsJoinEmittedOrAdministratorSession proves no
// relationship leaves target_type empty and the member edge target is a Macie
// session resource id, so the edge can join the administrator account's session
// node by the shared resource_id convention.
func TestScannerRelationshipTargetsJoinEmittedOrAdministratorSession(t *testing.T) {
	client := &fakeClient{
		session: Session{Enabled: true, Status: "ENABLED"},
		members: []MemberAccount{{AccountID: "111122223333", AdministratorID: "123456789012", RelationshipStatus: "Enabled"}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if target, _ := envelope.Payload["target_type"].(string); strings.TrimSpace(target) == "" {
			t.Fatalf("relationship has empty target_type: %#v", envelope)
		}
		if target, _ := envelope.Payload["target_resource_id"].(string); strings.TrimSpace(target) == "" {
			t.Fatalf("relationship has empty target_resource_id: %#v", envelope)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceGuardDuty

	_, err := (Scanner{Client: &fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceMacie,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:macie2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	session        Session
	administrator  string
	severityCounts map[string]int64
	members        []MemberAccount
	jobs           []ClassificationJob
	allowLists     []AllowList
	identifiers    []CustomDataIdentifier
	filters        []FindingsFilter

	memberCalls  int
	jobCalls     int
	findingCalls int
}

func (c *fakeClient) Session(context.Context) (Session, error) { return c.session, nil }

func (c *fakeClient) AdministratorAccountID(context.Context) (string, error) {
	return c.administrator, nil
}

func (c *fakeClient) FindingCountsBySeverity(context.Context) (map[string]int64, error) {
	c.findingCalls++
	return c.severityCounts, nil
}

func (c *fakeClient) ListMembers(context.Context) ([]MemberAccount, error) {
	c.memberCalls++
	return c.members, nil
}

func (c *fakeClient) ListClassificationJobs(context.Context) ([]ClassificationJob, error) {
	c.jobCalls++
	return c.jobs, nil
}

func (c *fakeClient) ListAllowLists(context.Context) ([]AllowList, error) { return c.allowLists, nil }

func (c *fakeClient) ListCustomDataIdentifiers(context.Context) ([]CustomDataIdentifier, error) {
	return c.identifiers, nil
}

func (c *fakeClient) ListFindingsFilters(context.Context) ([]FindingsFilter, error) {
	return c.filters, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
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
