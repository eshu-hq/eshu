package inspector2

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAccountStatusFeaturesMembersFiltersAndCisConfigs(t *testing.T) {
	client := fakeClient{
		account: AccountStatus{
			AccountID: "123456789012",
			Status:    "ENABLED",
			Features: []FeatureStatus{
				{Feature: "ec2", Status: "ENABLED"},
				{Feature: "ecr", Status: "ENABLED"},
				{Feature: "lambda", Status: "DISABLED"},
				{Feature: "lambda_code", Status: "DISABLED"},
			},
		},
		members: []MemberAccount{{
			AccountID:          "111122223333",
			AdministratorID:    "123456789012",
			RelationshipStatus: "ENABLED",
			UpdatedAt:          "2026-05-27T12:10:00Z",
		}},
		filters: []FilterSummary{{
			ARN:     "arn:aws:inspector2:us-east-1:123456789012:owner/123456789012/filter/abc",
			Name:    "suppress-known-benign",
			Action:  "SUPPRESS",
			OwnerID: "123456789012",
		}},
		cisConfigs: []CisScanConfiguration{{
			ARN:            "arn:aws:inspector2:us-east-1:123456789012:owner/123456789012/cis-configuration/xyz",
			Name:           "weekly-level1",
			OwnerID:        "123456789012",
			SecurityLevel:  "LEVEL_1",
			ScheduleKind:   "weekly",
			TargetAccounts: []string{"111122223333", "444455556666"},
			Tags:           map[string]string{"Team": "security"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	account := resourceByType(t, envelopes, awscloud.ResourceTypeInspector2Account)
	accountAttrs := attributesOf(t, account)
	if got, want := accountAttrs["status"], "ENABLED"; got != want {
		t.Fatalf("account status = %#v, want %q", got, want)
	}
	features, ok := accountAttrs["features"].([]map[string]any)
	if !ok || len(features) != 4 {
		t.Fatalf("account features = %#v, want 4 entries", accountAttrs["features"])
	}

	// Each enabled-feature relationship must be emitted (account-to-feature-status).
	assertRelationshipCount(t, envelopes, awscloud.RelationshipInspector2AccountHasFeatureStatus, 4)

	member := resourceByType(t, envelopes, awscloud.ResourceTypeInspector2MemberAccount)
	memberAttrs := attributesOf(t, member)
	if got, want := memberAttrs["account_id"], "111122223333"; got != want {
		t.Fatalf("member account_id = %#v, want %q", got, want)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipInspector2MemberManagedByAdministrator)

	filter := resourceByType(t, envelopes, awscloud.ResourceTypeInspector2Filter)
	filterAttrs := attributesOf(t, filter)
	if got, want := filter.Payload["name"], "suppress-known-benign"; got != want {
		t.Fatalf("filter name = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"criteria", "filter_criteria", "description", "reason"} {
		if _, exists := filterAttrs[forbidden]; exists {
			t.Fatalf("filter attribute %q persisted; Inspector2 filters are name-only metadata", forbidden)
		}
	}

	cis := resourceByType(t, envelopes, awscloud.ResourceTypeInspector2CisScanConfiguration)
	cisAttrs := attributesOf(t, cis)
	if got, want := cisAttrs["security_level"], "LEVEL_1"; got != want {
		t.Fatalf("cis security_level = %#v, want %q", got, want)
	}
	if got, want := cisAttrs["schedule_kind"], "weekly"; got != want {
		t.Fatalf("cis schedule_kind = %#v, want %q", got, want)
	}
	// One relationship per target account (CIS-config-to-target-account-set).
	assertRelationshipCount(t, envelopes, awscloud.RelationshipInspector2CisScanConfigurationTargetsAccount, 2)

	// No finding-detail fields may appear anywhere in the emitted payloads.
	for _, envelope := range envelopes {
		assertNoFindingDetails(t, envelope)
	}
}

func TestScannerEmitsNoMemberRelationshipsForStandaloneAccount(t *testing.T) {
	client := fakeClient{
		account: AccountStatus{
			AccountID: "123456789012",
			Status:    "ENABLED",
			Features:  []FeatureStatus{{Feature: "ec2", Status: "ENABLED"}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipInspector2MemberManagedByAdministrator {
			t.Fatalf("standalone account emitted a member relationship: %#v", envelope)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceGuardDuty

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
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
		ServiceKind:         awscloud.ServiceInspector2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:inspector2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	account    AccountStatus
	members    []MemberAccount
	filters    []FilterSummary
	cisConfigs []CisScanConfiguration
}

func (c fakeClient) AccountStatus(context.Context) (AccountStatus, error) {
	return c.account, nil
}

func (c fakeClient) ListMembers(context.Context) ([]MemberAccount, error) {
	return c.members, nil
}

func (c fakeClient) ListFilters(context.Context) ([]FilterSummary, error) {
	return c.filters, nil
}

func (c fakeClient) ListCisScanConfigurations(context.Context) ([]CisScanConfiguration, error) {
	return c.cisConfigs, nil
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if relationshipCount(envelopes, relationshipType) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
}

func assertRelationshipCount(t *testing.T, envelopes []facts.Envelope, relationshipType string, want int) {
	t.Helper()
	if got := relationshipCount(envelopes, relationshipType); got != want {
		t.Fatalf("relationship_type %q count = %d, want %d", relationshipType, got, want)
	}
}

func relationshipCount(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// assertNoFindingDetails fails if any emitted payload carries a finding-body
// field. Inspector v2 finding details (CVE, package version, affected host ARN)
// reveal exploitation surface and must never be persisted.
func assertNoFindingDetails(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	forbidden := []string{
		"finding", "findings", "vulnerability", "vulnerabilities", "cve",
		"package_version", "affected_resource", "exploit", "remediation",
		"finding_arn", "title",
	}
	walk := func(attrs map[string]any) {
		for key := range attrs {
			for _, banned := range forbidden {
				if key == banned {
					t.Fatalf("payload carries forbidden finding-detail field %q: %#v", key, envelope)
				}
			}
		}
	}
	if attrs, ok := envelope.Payload["attributes"].(map[string]any); ok {
		walk(attrs)
	}
}
