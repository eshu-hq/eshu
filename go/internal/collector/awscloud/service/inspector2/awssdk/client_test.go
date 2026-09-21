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
	awsinspector2 "github.com/aws/aws-sdk-go-v2/service/inspector2"
	i2types "github.com/aws/aws-sdk-go-v2/service/inspector2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndFindingDetailAPIs is the security
// gate for the Inspector v2 SDK adapter. The scanner contract is metadata-only
// and read-only: it must never expose a mutation API (Enable, Disable,
// Create/Update/Delete, Associate/Disassociate, BatchUpdate...) nor any
// finding-body, code-snippet, or SBOM read. This test reflects over the
// adapter's internal apiClient interface and FAILS if a future SDK refactor
// adds any forbidden method, including a substring match so additions like
// EnableEc2 or BatchGetFindingDetails cannot slip past.
func TestAPIClientInterfaceExcludesMutationAndFindingDetailAPIs(t *testing.T) {
	forbidden := []string{
		// Mutation surface explicitly banned by issue #740.
		"Enable", "Disable",
		"EnableDelegatedAdminAccount", "DisableDelegatedAdminAccount",
		"AssociateMember", "DisassociateMember",
		"Create", "Update", "Delete",
		"BatchUpdateMemberEc2DeepInspectionStatus",
		// Finding-body / exploitation-surface reads banned as forbidden payloads.
		"GetFindings", "ListFindings", "ListFindingAggregations",
		"BatchGetFindingDetails", "BatchGetCodeSnippet", "GetSbomExport",
		"GetCisScanReport", "GetCisScanResultDetails",
		"ListCisScanResultsAggregatedByChecks",
		"ListCisScanResultsAggregatedByTargetResource",
		// GetFilter would resolve filter criteria expressions.
		"GetFilter",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		for _, banned := range forbidden {
			if strings.Contains(method.Name, banned) {
				t.Fatalf("apiClient exposes method %q containing forbidden operation %q; Inspector2 adapter is metadata-only and read-only", method.Name, banned)
			}
		}
	}
}

func TestClientReadsMetadataAndDropsFilterCriteria(t *testing.T) {
	api := &fakeInspector2API{
		accountStatus: &awsinspector2.BatchGetAccountStatusOutput{
			Accounts: []i2types.AccountState{{
				AccountId: aws.String("123456789012"),
				State:     &i2types.State{Status: i2types.StatusEnabled},
				ResourceState: &i2types.ResourceState{
					Ec2:        &i2types.State{Status: i2types.StatusEnabled},
					Ecr:        &i2types.State{Status: i2types.StatusEnabled},
					Lambda:     &i2types.State{Status: i2types.StatusDisabled},
					LambdaCode: &i2types.State{Status: i2types.StatusDisabled},
				},
			}},
		},
		memberPages: []*awsinspector2.ListMembersOutput{{
			Members: []i2types.Member{{
				AccountId:               aws.String("111122223333"),
				DelegatedAdminAccountId: aws.String("123456789012"),
				RelationshipStatus:      i2types.RelationshipStatusEnabled,
				UpdatedAt:               aws.Time(mustTime()),
			}},
		}},
		filterPages: []*awsinspector2.ListFiltersOutput{{
			Filters: []i2types.Filter{{
				Arn:     aws.String("arn:aws:inspector2:us-east-1:123456789012:owner/123456789012/filter/abc"),
				Name:    aws.String("suppress-known-benign"),
				Action:  i2types.FilterActionSuppress,
				OwnerId: aws.String("123456789012"),
				// Criteria, Description, and Reason are present on the SDK record
				// and must be dropped by the adapter.
				Criteria:    &i2types.FilterCriteria{},
				Description: aws.String("hunt hypothesis: lateral movement via SSM"),
				Reason:      aws.String("threat hunting"),
			}},
		}},
		cisPages: []*awsinspector2.ListCisScanConfigurationsOutput{{
			ScanConfigurations: []i2types.CisScanConfiguration{{
				ScanConfigurationArn: aws.String("arn:aws:inspector2:us-east-1:123456789012:owner/123456789012/cis-configuration/xyz"),
				ScanName:             aws.String("weekly-level1"),
				OwnerId:              aws.String("123456789012"),
				SecurityLevel:        i2types.CisSecurityLevelLevel1,
				Schedule:             &i2types.ScheduleMemberWeekly{},
				Targets: &i2types.CisTargets{
					AccountIds: []string{"111122223333", "444455556666"},
				},
				Tags: map[string]string{"Team": "security"},
			}},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceInspector2},
	}

	account, err := adapter.AccountStatus(context.Background())
	if err != nil {
		t.Fatalf("AccountStatus() error = %v, want nil", err)
	}
	if account.Status != "ENABLED" {
		t.Fatalf("account status = %q, want ENABLED", account.Status)
	}
	wantFeatures := map[string]string{"ec2": "ENABLED", "ecr": "ENABLED", "lambda": "DISABLED", "lambda_code": "DISABLED"}
	if got := len(account.Features); got != len(wantFeatures) {
		t.Fatalf("len(features) = %d, want %d", got, len(wantFeatures))
	}
	for _, feature := range account.Features {
		if want := wantFeatures[feature.Feature]; want != feature.Status {
			t.Fatalf("feature %q status = %q, want %q", feature.Feature, feature.Status, want)
		}
	}

	members, err := adapter.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers() error = %v, want nil", err)
	}
	if len(members) != 1 || members[0].AccountID != "111122223333" {
		t.Fatalf("members = %#v, want one member 111122223333", members)
	}
	if members[0].AdministratorID != "123456789012" {
		t.Fatalf("member administrator = %q, want 123456789012", members[0].AdministratorID)
	}

	filters, err := adapter.ListFilters(context.Background())
	if err != nil {
		t.Fatalf("ListFilters() error = %v, want nil", err)
	}
	if len(filters) != 1 {
		t.Fatalf("len(filters) = %d, want 1", len(filters))
	}
	if filters[0].Name != "suppress-known-benign" {
		t.Fatalf("filter name = %q, want suppress-known-benign", filters[0].Name)
	}
	// The scanner-owned FilterSummary type has no field that can carry criteria,
	// description, or reason, so the adapter physically cannot leak them.
	filterType := reflect.TypeOf(filters[0])
	for _, banned := range []string{"Criteria", "Description", "Reason"} {
		if _, ok := filterType.FieldByName(banned); ok {
			t.Fatalf("FilterSummary exposes field %q; Inspector2 filters are name-only", banned)
		}
	}

	cisConfigs, err := adapter.ListCisScanConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListCisScanConfigurations() error = %v, want nil", err)
	}
	if len(cisConfigs) != 1 {
		t.Fatalf("len(cisConfigs) = %d, want 1", len(cisConfigs))
	}
	if cisConfigs[0].SecurityLevel != "LEVEL_1" {
		t.Fatalf("cis security_level = %q, want LEVEL_1", cisConfigs[0].SecurityLevel)
	}
	if cisConfigs[0].ScheduleKind != "weekly" {
		t.Fatalf("cis schedule_kind = %q, want weekly", cisConfigs[0].ScheduleKind)
	}
	if !slices.Equal(cisConfigs[0].TargetAccounts, []string{"111122223333", "444455556666"}) {
		t.Fatalf("cis target accounts = %#v, want [111122223333 444455556666]", cisConfigs[0].TargetAccounts)
	}

	for _, forbidden := range []string{"ListFindings", "GetFindings", "GetFilter", "BatchGetFindingDetails", "GetSbomExport"} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden Inspector2 call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

// TestAccountStatusSurfacesFailedAccount proves the adapter does not swallow a
// per-account failure. BatchGetAccountStatus can return HTTP success while
// placing the requested account in FailedAccounts (for example ACCESS_DENIED or
// BLOCKED_BY_ORGANIZATION_POLICY). Reporting an empty status in that case would
// be wrong truth, so the adapter must return an error carrying the AWS code.
func TestAccountStatusSurfacesFailedAccount(t *testing.T) {
	api := &fakeInspector2API{
		accountStatus: &awsinspector2.BatchGetAccountStatusOutput{
			FailedAccounts: []i2types.FailedAccount{{
				AccountId:    aws.String("123456789012"),
				ErrorCode:    i2types.ErrorCodeAccessDenied,
				ErrorMessage: aws.String("not authorized to view Inspector status"),
			}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceInspector2},
	}

	_, err := adapter.AccountStatus(context.Background())
	if err == nil {
		t.Fatalf("AccountStatus() error = nil, want failed-account error")
	}
	if !strings.Contains(err.Error(), "ACCESS_DENIED") {
		t.Fatalf("AccountStatus() error = %v, want AWS error code ACCESS_DENIED", err)
	}
}
