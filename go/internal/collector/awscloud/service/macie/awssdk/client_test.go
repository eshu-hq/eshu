// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmacie2 "github.com/aws/aws-sdk-go-v2/service/macie2"
	macietypes "github.com/aws/aws-sdk-go-v2/service/macie2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesSensitivePayloadAndMutationAPIs is the security
// gate for the Amazon Macie SDK adapter and the single most important test in
// this package. Macie is the highest-redaction service in the collector: its
// sensitive-data findings ARE the personally identifiable information it
// detected, its custom data identifier regular expressions ARE descriptions of
// that data, and its allow-list contents and finding criteria reveal detection
// posture. The adapter must therefore never be able to reach any of those.
//
// The test reflects over the adapter's internal apiClient interface and FAILS
// if a future SDK refactor adds any forbidden method. The match is a substring
// match so additions like GetSensitiveDataOccurrencesAvailability or
// DescribeClassificationJob cannot slip past, while the eight allowed
// metadata-only operations remain reachable.
//
// The eight metadata-only operations form an explicit allow-set; the substring
// scan runs only against methods outside it. That keeps the scan from
// mis-flagging an allowed name (ListFindingsFilters contains the substring
// ListFindings yet is the safe filter-identity read, not the forbidden
// finding-body read) while still failing on any newly added method.
func TestAPIClientInterfaceExcludesSensitivePayloadAndMutationAPIs(t *testing.T) {
	allowed := map[string]struct{}{
		"GetMacieSession":           {},
		"GetAdministratorAccount":   {},
		"ListMembers":               {},
		"ListClassificationJobs":    {},
		"ListAllowLists":            {},
		"ListCustomDataIdentifiers": {},
		"ListFindingsFilters":       {},
		"GetFindingStatistics":      {},
	}
	forbidden := []string{
		// Sensitive-data finding reads: the PII detection results themselves.
		"GetSensitiveDataOccurrences", "GetSensitiveDataOccurrencesAvailability",
		"GetFindings", "ListFindings", "CreateSampleFindings",
		// Custom data identifier body / test reads: the regex IS the PII pattern.
		"GetCustomDataIdentifier", "BatchGetCustomDataIdentifiers",
		"TestCustomDataIdentifier",
		// Allow-list contents and findings filter criteria reads.
		"GetAllowList", "GetFindingsFilter",
		// Classification-job bucket-criteria reads and bucket enumeration.
		"DescribeClassificationJob", "DescribeBuckets", "SearchResources",
		"GetBucketStatistics", "GetClassificationScope",
		// Resource profile / sensitivity reads expose per-resource finding detail.
		"GetResourceProfile", "ListResourceProfile",
		"GetSensitivityInspectionTemplate", "GetRevealConfiguration",
		"GetUsageStatistics",
		// Mutation surface explicitly banned by issue #741.
		"Enable", "Disable", "Create", "Update", "Delete",
		"Accept", "Decline", "Associate", "Disassociate",
		"Put", "Tag", "Untag", "BatchUpdate",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		if _, ok := allowed[method.Name]; !ok {
			t.Fatalf("apiClient exposes method %q outside the metadata-only allow-set; add it to the allow-set only after proving it is identity/count metadata", method.Name)
		}
		for _, banned := range forbidden {
			if !strings.Contains(method.Name, banned) {
				continue
			}
			// ListFindingsFilters legitimately contains the ListFindings
			// substring but is the safe filter-identity read; every other
			// substring hit is a real violation.
			if method.Name == "ListFindingsFilters" && banned == "ListFindings" {
				continue
			}
			t.Fatalf("apiClient exposes method %q containing forbidden operation %q; Macie adapter is metadata-only and read-only", method.Name, banned)
		}
	}
}

func TestClientReadsMetadataAndDropsSensitivePayloads(t *testing.T) {
	api := &fakeMacie2API{
		session: &awsmacie2.GetMacieSessionOutput{
			Status:                     macietypes.MacieStatusEnabled,
			FindingPublishingFrequency: macietypes.FindingPublishingFrequencyFifteenMinutes,
			ServiceRole:                aws.String("arn:aws:iam::123456789012:role/aws-service-role/macie.amazonaws.com/AWSServiceRoleForAmazonMacie"),
			CreatedAt:                  aws.Time(mustTime()),
			UpdatedAt:                  aws.Time(mustTime()),
		},
		administrator: &awsmacie2.GetAdministratorAccountOutput{
			Administrator: &macietypes.Invitation{AccountId: aws.String("999988887777")},
		},
		memberPages: []*awsmacie2.ListMembersOutput{{
			Members: []macietypes.Member{{
				AccountId:              aws.String("111122223333"),
				AdministratorAccountId: aws.String("123456789012"),
				// Email is personal contact data and must be dropped.
				Email:              aws.String("security@example.com"),
				RelationshipStatus: macietypes.RelationshipStatusEnabled,
				InvitedAt:          aws.Time(mustTime()),
				UpdatedAt:          aws.Time(mustTime()),
				Tags:               map[string]string{"Team": "security"},
			}},
		}},
		jobPages: []*awsmacie2.ListClassificationJobsOutput{{
			Items: []macietypes.JobSummary{{
				JobId:     aws.String("job-abc"),
				Name:      aws.String("weekly-pii-scan"),
				JobType:   macietypes.JobTypeScheduled,
				JobStatus: macietypes.JobStatusRunning,
				CreatedAt: aws.Time(mustTime()),
				// BucketDefinitions is the explicit bucket list and BucketCriteria
				// is the property/tag criteria. Both must be reduced to counts only.
				BucketDefinitions: []macietypes.S3BucketDefinitionForJob{
					{AccountId: aws.String("123456789012"), Buckets: []string{"orders-raw", "orders-pii"}},
					{AccountId: aws.String("111122223333"), Buckets: []string{"member-data"}},
				},
			}},
		}},
		allowListPages: []*awsmacie2.ListAllowListsOutput{{
			AllowLists: []macietypes.AllowListSummary{{
				Id:   aws.String("allow-1"),
				Name: aws.String("approved-test-data"),
				// Description must not be carried into the scanner type.
				Description: aws.String("known-benign synthetic SSNs used in QA"),
			}},
		}},
		identifierPages: []*awsmacie2.ListCustomDataIdentifiersOutput{{
			Items: []macietypes.CustomDataIdentifierSummary{{
				Id:   aws.String("cdi-1"),
				Name: aws.String("internal-employee-id"),
				// Description must not be carried into the scanner type.
				Description: aws.String("matches EMP-#### badge numbers"),
			}},
		}},
		filterPages: []*awsmacie2.ListFindingsFiltersOutput{{
			FindingsFilterListItems: []macietypes.FindingsFilterListItem{{
				Id:     aws.String("filter-1"),
				Name:   aws.String("suppress-known-benign"),
				Action: macietypes.FindingsFilterActionArchive,
			}},
		}},
		statistics: &awsmacie2.GetFindingStatisticsOutput{
			CountsByGroup: []macietypes.GroupCount{
				{GroupKey: aws.String("Low"), Count: aws.Int64(3)},
				{GroupKey: aws.String("High"), Count: aws.Int64(1)},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMacie},
	}

	session, err := adapter.Session(context.Background())
	if err != nil {
		t.Fatalf("Session() error = %v, want nil", err)
	}
	if !session.Enabled || session.Status != "ENABLED" {
		t.Fatalf("session = %#v, want enabled ENABLED", session)
	}

	admin, err := adapter.AdministratorAccountID(context.Background())
	if err != nil {
		t.Fatalf("AdministratorAccountID() error = %v, want nil", err)
	}
	if admin != "999988887777" {
		t.Fatalf("administrator = %q, want 999988887777", admin)
	}

	members, err := adapter.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers() error = %v, want nil", err)
	}
	if len(members) != 1 || members[0].AccountID != "111122223333" {
		t.Fatalf("members = %#v, want one member 111122223333", members)
	}
	// The scanner-owned MemberAccount type has no field that can carry email.
	memberType := reflect.TypeOf(members[0])
	for _, banned := range []string{"Email", "EmailAddress"} {
		if _, ok := memberType.FieldByName(banned); ok {
			t.Fatalf("MemberAccount exposes field %q; Macie members are email-free", banned)
		}
	}

	jobs, err := adapter.ListClassificationJobs(context.Background())
	if err != nil {
		t.Fatalf("ListClassificationJobs() error = %v, want nil", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].BucketCount != 3 {
		t.Fatalf("job bucket count = %d, want 3 (2 + 1 across two accounts)", jobs[0].BucketCount)
	}
	if jobs[0].AccountCount != 2 {
		t.Fatalf("job account count = %d, want 2", jobs[0].AccountCount)
	}
	// The scanner-owned ClassificationJob type cannot carry the bucket list or
	// criteria; reflect over it to prove the fields do not exist.
	jobType := reflect.TypeOf(jobs[0])
	for _, banned := range []string{"Buckets", "BucketDefinitions", "BucketCriteria", "Criteria"} {
		if _, ok := jobType.FieldByName(banned); ok {
			t.Fatalf("ClassificationJob exposes field %q; jobs carry counts only", banned)
		}
	}

	allowLists, err := adapter.ListAllowLists(context.Background())
	if err != nil {
		t.Fatalf("ListAllowLists() error = %v, want nil", err)
	}
	if len(allowLists) != 1 || allowLists[0].Name != "approved-test-data" {
		t.Fatalf("allow lists = %#v, want approved-test-data", allowLists)
	}
	allowListType := reflect.TypeOf(allowLists[0])
	for _, banned := range []string{"Description", "Criteria", "Regex", "S3WordsList", "Contents"} {
		if _, ok := allowListType.FieldByName(banned); ok {
			t.Fatalf("AllowList exposes field %q; allow lists are identity-only", banned)
		}
	}

	identifiers, err := adapter.ListCustomDataIdentifiers(context.Background())
	if err != nil {
		t.Fatalf("ListCustomDataIdentifiers() error = %v, want nil", err)
	}
	if len(identifiers) != 1 || identifiers[0].Name != "internal-employee-id" {
		t.Fatalf("identifiers = %#v, want internal-employee-id", identifiers)
	}
	identifierType := reflect.TypeOf(identifiers[0])
	for _, banned := range []string{"Regex", "Keywords", "IgnoreWords", "Description", "MaximumMatchDistance"} {
		if _, ok := identifierType.FieldByName(banned); ok {
			t.Fatalf("CustomDataIdentifier exposes field %q; identifiers are identity-only", banned)
		}
	}

	filters, err := adapter.ListFindingsFilters(context.Background())
	if err != nil {
		t.Fatalf("ListFindingsFilters() error = %v, want nil", err)
	}
	if len(filters) != 1 || filters[0].Action != "ARCHIVE" {
		t.Fatalf("filters = %#v, want ARCHIVE action", filters)
	}
	filterType := reflect.TypeOf(filters[0])
	for _, banned := range []string{"Criteria", "FindingCriteria", "Description"} {
		if _, ok := filterType.FieldByName(banned); ok {
			t.Fatalf("FindingsFilter exposes field %q; filters omit criteria", banned)
		}
	}

	counts, err := adapter.FindingCountsBySeverity(context.Background())
	if err != nil {
		t.Fatalf("FindingCountsBySeverity() error = %v, want nil", err)
	}
	if counts["Low"] != 3 || counts["High"] != 1 {
		t.Fatalf("severity counts = %#v, want Low=3 High=1", counts)
	}

	for _, forbidden := range []string{
		"GetSensitiveDataOccurrences", "GetFindings", "ListFindings",
		"GetCustomDataIdentifier", "GetAllowList", "GetFindingsFilter",
		"DescribeClassificationJob", "DescribeBuckets",
	} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden Macie call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

// TestSessionMapsNotEnabledToDisabled proves the adapter treats Macie's
// "not enabled" response as a disabled session rather than an error, so a clean
// account that has never turned on Macie produces a truthful disabled record.
func TestSessionMapsNotEnabledToDisabled(t *testing.T) {
	api := &fakeMacie2API{sessionErr: &macietypes.AccessDeniedException{
		Message: aws.String("Macie is not enabled. Enable Macie and try again."),
	}}
	adapter := &Client{client: api, boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMacie}}

	session, err := adapter.Session(context.Background())
	if err != nil {
		t.Fatalf("Session() error = %v, want nil for not-enabled account", err)
	}
	if session.Enabled {
		t.Fatalf("session = %#v, want Enabled false", session)
	}
}

// TestSessionSurfacesRealAccessDenied proves the adapter does not swallow a
// genuine authorization failure as a disabled session. Reporting a disabled
// session when the caller simply lacks macie2:GetMacieSession permission would
// be wrong truth.
func TestSessionSurfacesRealAccessDenied(t *testing.T) {
	api := &fakeMacie2API{sessionErr: &macietypes.AccessDeniedException{
		Message: aws.String("User is not authorized to perform macie2:GetMacieSession"),
	}}
	adapter := &Client{client: api, boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMacie}}

	_, err := adapter.Session(context.Background())
	if err == nil {
		t.Fatalf("Session() error = nil, want surfaced access-denied error")
	}
}

// TestAdministratorAccountNotFoundMapsToEmpty proves a standalone or
// administrator account (which has no administrator) returns an empty id rather
// than an error.
func TestAdministratorAccountNotFoundMapsToEmpty(t *testing.T) {
	api := &fakeMacie2API{administratorErr: &macietypes.ResourceNotFoundException{
		Message: aws.String("no administrator account"),
	}}
	adapter := &Client{client: api, boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMacie}}

	admin, err := adapter.AdministratorAccountID(context.Background())
	if err != nil {
		t.Fatalf("AdministratorAccountID() error = %v, want nil", err)
	}
	if admin != "" {
		t.Fatalf("administrator = %q, want empty", admin)
	}
}
