// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsds "github.com/aws/aws-sdk-go-v2/service/directoryservice"
	awsdstypes "github.com/aws/aws-sdk-go-v2/service/directoryservice/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndSecretAPIs is the primary contract
// guard for issue #827. apiClient is the single seam between the Directory
// Service adapter and the AWS SDK client (Client.client is typed as apiClient,
// pinned by var _ apiClient = (*awsds.Client)(nil) in client.go), so any SDK
// method the adapter could call must be listed here. A regression that added a
// mutation API (ResetUserPassword, Create/Delete/Update/Enable/Disable/...) or a
// secret read would either fail to compile against this interface or trip this
// shape assertion.
func TestAPIClientInterfaceExcludesMutationAndSecretAPIs(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	want := map[string]bool{
		"DescribeDirectories":       true,
		"DescribeTrusts":            true,
		"DescribeSharedDirectories": true,
		"DescribeLDAPSSettings":     true,
		"ListTagsForResource":       true,
	}
	have := map[string]bool{}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		have[ifaceType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("apiClient missing required metadata-read method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("apiClient exposes unexpected method %q; metadata-only contract violated", name)
		}
	}

	// Defensive check: every method on the SDK seam must be a read-class call
	// (Describe/List/Get prefix), and no method may name a forbidden mutation or
	// password API. ResetUserPassword is the highest-risk Directory Service write
	// and is called out explicitly in issue #827. The read-prefix guard is
	// checked first so legitimate reads like DescribeSharedDirectories are not
	// flagged by a verb that also appears in a mutation name (Share/Connect).
	readPrefixes := []string{"Describe", "List", "Get"}
	forbiddenSubstrings := []string{
		"Create", "Update", "Delete", "Remove", "Add", "Put", "Modify",
		"Enable", "Disable", "Reset", "Register", "Deregister", "Accept",
		"Reject", "Unshare", "Cancel", "Start", "Restore", "Password",
	}
	for name := range have {
		isRead := false
		for _, prefix := range readPrefixes {
			if strings.HasPrefix(name, prefix) {
				isRead = true
				break
			}
		}
		if !isRead {
			t.Errorf("apiClient method %q is not a read-class (Describe/List/Get) call; metadata-only contract violated", name)
		}
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("apiClient method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}

func TestClientListDirectoriesMapsTypesAndPlacement(t *testing.T) {
	fake := &fakeDSAPI{
		directories: []awsdstypes.DirectoryDescription{
			{
				DirectoryId: aws.String("d-1234567890"),
				Name:        aws.String("corp.example.com"),
				ShortName:   aws.String("CORP"),
				Type:        awsdstypes.DirectoryTypeMicrosoftAd,
				Edition:     awsdstypes.DirectoryEditionEnterprise,
				Size:        awsdstypes.DirectorySizeLarge,
				Stage:       awsdstypes.DirectoryStageActive,
				SsoEnabled:  true,
				VpcSettings: &awsdstypes.DirectoryVpcSettingsDescription{
					VpcId:             aws.String("vpc-aaa"),
					SubnetIds:         []string{"subnet-1", "subnet-2"},
					SecurityGroupId:   aws.String("sg-123"),
					AvailabilityZones: []string{"us-east-1a", "us-east-1b"},
				},
			},
			{
				DirectoryId: aws.String("d-0987654321"),
				Name:        aws.String("connector.example.com"),
				Type:        awsdstypes.DirectoryTypeAdConnector,
				Size:        awsdstypes.DirectorySizeSmall,
				Stage:       awsdstypes.DirectoryStageActive,
				// AD Connector reports placement under ConnectSettings, and the
				// CustomerUserName service-account field must never be read.
				ConnectSettings: &awsdstypes.DirectoryConnectSettingsDescription{
					VpcId:            aws.String("vpc-bbb"),
					SubnetIds:        []string{"subnet-9"},
					SecurityGroupId:  aws.String("sg-999"),
					CustomerUserName: aws.String("svc-connector"),
				},
			},
		},
		ldaps: map[string][]awsdstypes.LDAPSSettingInfo{
			"d-1234567890": {{LDAPSStatus: awsdstypes.LDAPSStatusEnabled}},
		},
		tags: map[string][]awsdstypes.Tag{
			"d-1234567890": {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	directories, err := adapter.ListDirectories(context.Background())
	if err != nil {
		t.Fatalf("ListDirectories() error = %v, want nil", err)
	}
	if got, want := len(directories), 2; got != want {
		t.Fatalf("len(directories) = %d, want %d", got, want)
	}
	byID := map[string]int{}
	for i, d := range directories {
		byID[d.ID] = i
	}

	ad := directories[byID["d-1234567890"]]
	if got, want := ad.Type, "MicrosoftAD"; got != want {
		t.Fatalf("MicrosoftAD type = %q, want %q", got, want)
	}
	if got, want := ad.Edition, "Enterprise"; got != want {
		t.Fatalf("edition = %q, want %q", got, want)
	}
	if got, want := ad.VPCID, "vpc-aaa"; got != want {
		t.Fatalf("vpc_id = %q, want %q", got, want)
	}
	if got, want := len(ad.SubnetIDs), 2; got != want {
		t.Fatalf("subnet count = %d, want %d", got, want)
	}
	if got, want := len(ad.LDAPSStatuses), 1; got != want {
		t.Fatalf("ldaps statuses = %d, want %d", got, want)
	}
	if got, want := ad.Tags["Environment"], "prod"; got != want {
		t.Fatalf("tag Environment = %q, want %q", got, want)
	}
	if !ad.SsoEnabled {
		t.Fatalf("sso_enabled = false, want true")
	}

	connector := directories[byID["d-0987654321"]]
	if got, want := connector.VPCID, "vpc-bbb"; got != want {
		t.Fatalf("connector vpc_id = %q, want %q (must read ConnectSettings)", got, want)
	}
	if got, want := len(connector.LDAPSStatuses), 0; got != want {
		t.Fatalf("connector ldaps statuses = %d, want %d (LDAPS unsupported for AD Connector)", got, want)
	}
}

// TestClientDoesNotCallLDAPSForUnsupportedTypes proves the adapter skips
// DescribeLDAPSSettings for SimpleAD and AD Connector directories, which do not
// support LDAPS, avoiding an UnsupportedOperationException at scan time.
func TestClientDoesNotCallLDAPSForUnsupportedTypes(t *testing.T) {
	fake := &fakeDSAPI{
		directories: []awsdstypes.DirectoryDescription{
			{DirectoryId: aws.String("d-simple0001"), Type: awsdstypes.DirectoryTypeSimpleAd, Stage: awsdstypes.DirectoryStageActive},
			{DirectoryId: aws.String("d-connector1"), Type: awsdstypes.DirectoryTypeAdConnector, Stage: awsdstypes.DirectoryStageActive},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}
	if _, err := adapter.ListDirectories(context.Background()); err != nil {
		t.Fatalf("ListDirectories() error = %v, want nil", err)
	}
	if fake.ldapsCalls != 0 {
		t.Fatalf("DescribeLDAPSSettings called %d times for SimpleAD/ADConnector, want 0", fake.ldapsCalls)
	}
}

func TestClientListDirectoriesPaginates(t *testing.T) {
	fake := &fakeDSAPI{
		directoryPages: [][]awsdstypes.DirectoryDescription{
			{{DirectoryId: aws.String("d-page0001a"), Type: awsdstypes.DirectoryTypeSimpleAd, Stage: awsdstypes.DirectoryStageActive}},
			{{DirectoryId: aws.String("d-page0002b"), Type: awsdstypes.DirectoryTypeSimpleAd, Stage: awsdstypes.DirectoryStageActive}},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}
	directories, err := adapter.ListDirectories(context.Background())
	if err != nil {
		t.Fatalf("ListDirectories() error = %v, want nil", err)
	}
	if got, want := len(directories), 2; got != want {
		t.Fatalf("len(directories) = %d, want %d (pagination not followed)", got, want)
	}
	if fake.directoryCalls != 2 {
		t.Fatalf("DescribeDirectories calls = %d, want 2", fake.directoryCalls)
	}
}

func TestClientListTrustsAndSharedDirectories(t *testing.T) {
	fake := &fakeDSAPI{
		trusts: map[string][]awsdstypes.Trust{
			"d-1234567890": {{
				TrustId:          aws.String("t-aaaa111122"),
				DirectoryId:      aws.String("d-1234567890"),
				RemoteDomainName: aws.String("remote.example.com"),
				TrustDirection:   awsdstypes.TrustDirectionTwoWay,
				TrustType:        awsdstypes.TrustTypeForest,
				TrustState:       awsdstypes.TrustStateVerified,
			}},
		},
		shares: map[string][]awsdstypes.SharedDirectory{
			"d-1234567890": {{
				OwnerAccountId:    aws.String("123456789012"),
				OwnerDirectoryId:  aws.String("d-1234567890"),
				SharedAccountId:   aws.String("210987654321"),
				SharedDirectoryId: aws.String("d-shared00001"),
				ShareMethod:       awsdstypes.ShareMethodHandshake,
				ShareStatus:       awsdstypes.ShareStatusShared,
			}},
		},
	}
	adapter := &Client{client: fake, boundary: testBoundary()}

	trusts, err := adapter.ListTrusts(context.Background(), "d-1234567890")
	if err != nil {
		t.Fatalf("ListTrusts() error = %v, want nil", err)
	}
	if got, want := len(trusts), 1; got != want {
		t.Fatalf("len(trusts) = %d, want %d", got, want)
	}
	if got, want := trusts[0].Direction, "Two-Way"; got != want {
		t.Fatalf("trust direction = %q, want %q", got, want)
	}

	shares, err := adapter.ListSharedDirectories(context.Background(), "d-1234567890")
	if err != nil {
		t.Fatalf("ListSharedDirectories() error = %v, want nil", err)
	}
	if got, want := len(shares), 1; got != want {
		t.Fatalf("len(shares) = %d, want %d", got, want)
	}
	if got, want := shares[0].SharedAccountID, "210987654321"; got != want {
		t.Fatalf("shared account id = %q, want %q", got, want)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceDirectoryService}
}

type fakeDSAPI struct {
	directories    []awsdstypes.DirectoryDescription
	directoryPages [][]awsdstypes.DirectoryDescription
	trusts         map[string][]awsdstypes.Trust
	shares         map[string][]awsdstypes.SharedDirectory
	ldaps          map[string][]awsdstypes.LDAPSSettingInfo
	tags           map[string][]awsdstypes.Tag

	directoryCalls int
	ldapsCalls     int
}

func (f *fakeDSAPI) DescribeDirectories(
	_ context.Context,
	input *awsds.DescribeDirectoriesInput,
	_ ...func(*awsds.Options),
) (*awsds.DescribeDirectoriesOutput, error) {
	f.directoryCalls++
	if len(f.directoryPages) > 0 {
		idx := 0
		if token := aws.ToString(input.NextToken); token != "" {
			idx = int(token[0] - '0')
		}
		page := f.directoryPages[idx]
		var next *string
		if idx+1 < len(f.directoryPages) {
			next = aws.String(string(rune('0' + idx + 1)))
		}
		return &awsds.DescribeDirectoriesOutput{DirectoryDescriptions: page, NextToken: next}, nil
	}
	return &awsds.DescribeDirectoriesOutput{DirectoryDescriptions: f.directories}, nil
}

func (f *fakeDSAPI) DescribeTrusts(
	_ context.Context,
	input *awsds.DescribeTrustsInput,
	_ ...func(*awsds.Options),
) (*awsds.DescribeTrustsOutput, error) {
	return &awsds.DescribeTrustsOutput{Trusts: f.trusts[aws.ToString(input.DirectoryId)]}, nil
}

func (f *fakeDSAPI) DescribeSharedDirectories(
	_ context.Context,
	input *awsds.DescribeSharedDirectoriesInput,
	_ ...func(*awsds.Options),
) (*awsds.DescribeSharedDirectoriesOutput, error) {
	return &awsds.DescribeSharedDirectoriesOutput{SharedDirectories: f.shares[aws.ToString(input.OwnerDirectoryId)]}, nil
}

func (f *fakeDSAPI) DescribeLDAPSSettings(
	_ context.Context,
	input *awsds.DescribeLDAPSSettingsInput,
	_ ...func(*awsds.Options),
) (*awsds.DescribeLDAPSSettingsOutput, error) {
	f.ldapsCalls++
	return &awsds.DescribeLDAPSSettingsOutput{LDAPSSettingsInfo: f.ldaps[aws.ToString(input.DirectoryId)]}, nil
}

func (f *fakeDSAPI) ListTagsForResource(
	_ context.Context,
	input *awsds.ListTagsForResourceInput,
	_ ...func(*awsds.Options),
) (*awsds.ListTagsForResourceOutput, error) {
	return &awsds.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(input.ResourceId)]}, nil
}

var _ apiClient = (*fakeDSAPI)(nil)
