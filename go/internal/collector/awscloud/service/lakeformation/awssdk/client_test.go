// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslf "github.com/aws/aws-sdk-go-v2/service/lakeformation"
	awslftypes "github.com/aws/aws-sdk-go-v2/service/lakeformation/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutatingAndCredentialOperations is the security
// gate for the Lake Formation SDK adapter. The scanner contract forbids every
// grant/revoke, register/deregister, settings-put, and LF-Tag mutation, and it
// forbids the credential-vending and data-access readers that would expose the
// governed data or temporary access credentials. This test reflects over the
// adapter's internal apiClient interface and FAILS if any forbidden method
// appears, so a future SDK refactor cannot quietly broaden the contract.
func TestAPIClientInterfaceExcludesMutatingAndCredentialOperations(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Permission grant / revoke mutations.
		"GrantPermissions",
		"RevokePermissions",
		"BatchGrantPermissions",
		"BatchRevokePermissions",
		// Registration mutations.
		"RegisterResource",
		"DeregisterResource",
		"UpdateResource",
		// Settings and identity-center mutations.
		"PutDataLakeSettings",
		"CreateLakeFormationIdentityCenterConfiguration",
		"UpdateLakeFormationIdentityCenterConfiguration",
		"DeleteLakeFormationIdentityCenterConfiguration",
		"CreateLakeFormationOptIn",
		"DeleteLakeFormationOptIn",
		// LF-Tag mutations (tag values are sensitive).
		"AddLFTagsToResource",
		"RemoveLFTagsFromResource",
		"CreateLFTag",
		"DeleteLFTag",
		"UpdateLFTag",
		"CreateLFTagExpression",
		"DeleteLFTagExpression",
		"UpdateLFTagExpression",
		// Data-cells-filter mutations.
		"CreateDataCellsFilter",
		"DeleteDataCellsFilter",
		"UpdateDataCellsFilter",
		// Credential vending and governed-data access readers.
		"GetTemporaryGlueTableCredentials",
		"GetTemporaryGluePartitionCredentials",
		"GetTemporaryDataLocationCredentials",
		"GetTableObjects",
		"UpdateTableObjects",
		"GetWorkUnits",
		"GetWorkUnitResults",
		"StartQueryPlanning",
		"GetQueryState",
		"GetQueryStatistics",
	}
	for _, name := range forbidden {
		if _, ok := apiClientType.MethodByName(name); ok {
			t.Fatalf("apiClient interface exposes %q; Lake Formation scanner forbids this API", name)
		}
	}
}

func TestClientGetDataLakeSettingsReadsAdminIdentifiersOnly(t *testing.T) {
	api := &fakeLakeFormationAPI{
		settings: &awslf.GetDataLakeSettingsOutput{
			DataLakeSettings: &awslftypes.DataLakeSettings{
				DataLakeAdmins: []awslftypes.DataLakePrincipal{
					{DataLakePrincipalIdentifier: aws.String("arn:aws:iam::123456789012:role/Admin")},
				},
				ReadOnlyAdmins: []awslftypes.DataLakePrincipal{
					{DataLakePrincipalIdentifier: aws.String("arn:aws:iam::123456789012:role/Auditor")},
				},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	settings, err := adapter.GetDataLakeSettings(context.Background())
	if err != nil {
		t.Fatalf("GetDataLakeSettings() error = %v, want nil", err)
	}
	if got, want := settings.Admins, []string{"arn:aws:iam::123456789012:role/Admin"}; !sliceEqual(got, want) {
		t.Fatalf("Admins = %#v, want %#v", got, want)
	}
	if got, want := settings.ReadOnlyAdmins, []string{"arn:aws:iam::123456789012:role/Auditor"}; !sliceEqual(got, want) {
		t.Fatalf("ReadOnlyAdmins = %#v, want %#v", got, want)
	}
}

func TestClientListResourcesMapsRegistrationMetadata(t *testing.T) {
	api := &fakeLakeFormationAPI{
		resourcePages: []*awslf.ListResourcesOutput{{
			ResourceInfoList: []awslftypes.ResourceInfo{{
				ResourceArn:                  aws.String("arn:aws:s3:::analytics-lake/governed/"),
				RoleArn:                      aws.String("arn:aws:iam::123456789012:role/Register"),
				HybridAccessEnabled:          aws.Bool(true),
				WithFederation:               aws.Bool(false),
				VerificationStatus:           awslftypes.VerificationStatusVerified,
				ExpectedResourceOwnerAccount: aws.String("123456789012"),
				LastModified:                 aws.Time(time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	resources, err := adapter.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources() error = %v, want nil", err)
	}
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	resource := resources[0]
	if resource.ResourceARN != "arn:aws:s3:::analytics-lake/governed/" {
		t.Fatalf("ResourceARN = %q", resource.ResourceARN)
	}
	if resource.RoleARN != "arn:aws:iam::123456789012:role/Register" {
		t.Fatalf("RoleARN = %q", resource.RoleARN)
	}
	if !resource.HybridAccessEnabled {
		t.Fatalf("HybridAccessEnabled = false, want true")
	}
	if resource.VerificationStatus != "VERIFIED" {
		t.Fatalf("VerificationStatus = %q, want VERIFIED", resource.VerificationStatus)
	}
}

func TestClientListPermissionsDropsConditionAndKeepsGrantIdentity(t *testing.T) {
	api := &fakeLakeFormationAPI{
		permissionPages: []*awslf.ListPermissionsOutput{{
			PrincipalResourcePermissions: []awslftypes.PrincipalResourcePermissions{{
				Principal: &awslftypes.DataLakePrincipal{
					DataLakePrincipalIdentifier: aws.String("arn:aws:iam::123456789012:role/Analyst"),
				},
				Resource: &awslftypes.Resource{
					Table: &awslftypes.TableResource{
						DatabaseName: aws.String("analytics"),
						Name:         aws.String("orders"),
						CatalogId:    aws.String("123456789012"),
					},
				},
				Permissions:                []awslftypes.Permission{awslftypes.PermissionSelect, awslftypes.PermissionDescribe},
				PermissionsWithGrantOption: []awslftypes.Permission{awslftypes.PermissionSelect},
				Condition:                  &awslftypes.Condition{Expression: aws.String("secret-lf-tag-expression")},
				AdditionalDetails:          &awslftypes.DetailsMap{ResourceShare: []string{"arn:aws:ram::123456789012:resource-share/abc"}},
				LastUpdated:                aws.Time(time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	permissions, err := adapter.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("ListPermissions() error = %v, want nil", err)
	}
	if got, want := len(permissions), 1; got != want {
		t.Fatalf("len(permissions) = %d, want %d", got, want)
	}
	permission := permissions[0]
	if permission.PrincipalID != "arn:aws:iam::123456789012:role/Analyst" {
		t.Fatalf("PrincipalID = %q", permission.PrincipalID)
	}
	if permission.ResourceKind != "table" {
		t.Fatalf("ResourceKind = %q, want table", permission.ResourceKind)
	}
	if permission.DatabaseName != "analytics" || permission.TableName != "orders" {
		t.Fatalf("database/table = %q/%q, want analytics/orders", permission.DatabaseName, permission.TableName)
	}
	if got, want := permission.Privileges, []string{"DESCRIBE", "SELECT"}; !sliceEqual(got, want) {
		t.Fatalf("Privileges = %#v, want sorted %#v", got, want)
	}
	if got, want := permission.GrantablePrivileges, []string{"SELECT"}; !sliceEqual(got, want) {
		t.Fatalf("GrantablePrivileges = %#v, want %#v", got, want)
	}
}

func TestClientListPermissionsSortsPrivilegesDeterministically(t *testing.T) {
	// AWS does not guarantee a stable privilege order; the adapter must sort so
	// the `privileges` fact payload stays byte-identical across rescans.
	api := &fakeLakeFormationAPI{
		permissionPages: []*awslf.ListPermissionsOutput{{
			PrincipalResourcePermissions: []awslftypes.PrincipalResourcePermissions{{
				Principal: &awslftypes.DataLakePrincipal{DataLakePrincipalIdentifier: aws.String("p")},
				Resource:  &awslftypes.Resource{Database: &awslftypes.DatabaseResource{Name: aws.String("analytics")}},
				Permissions: []awslftypes.Permission{
					awslftypes.PermissionSelect,
					awslftypes.PermissionAll,
					awslftypes.PermissionDrop,
					awslftypes.PermissionAlter,
				},
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	permissions, err := adapter.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("ListPermissions() error = %v, want nil", err)
	}
	if got, want := permissions[0].Privileges, []string{"ALL", "ALTER", "DROP", "SELECT"}; !sliceEqual(got, want) {
		t.Fatalf("Privileges = %#v, want sorted %#v", got, want)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLakeFormation,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:lakeformation:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeLakeFormationAPI struct {
	settings        *awslf.GetDataLakeSettingsOutput
	resourcePages   []*awslf.ListResourcesOutput
	resourceCalls   int
	permissionPages []*awslf.ListPermissionsOutput
	permissionCalls int
}

func (f *fakeLakeFormationAPI) GetDataLakeSettings(
	_ context.Context,
	_ *awslf.GetDataLakeSettingsInput,
	_ ...func(*awslf.Options),
) (*awslf.GetDataLakeSettingsOutput, error) {
	if f.settings == nil {
		return &awslf.GetDataLakeSettingsOutput{}, nil
	}
	return f.settings, nil
}

func (f *fakeLakeFormationAPI) ListResources(
	_ context.Context,
	_ *awslf.ListResourcesInput,
	_ ...func(*awslf.Options),
) (*awslf.ListResourcesOutput, error) {
	if f.resourceCalls >= len(f.resourcePages) {
		return &awslf.ListResourcesOutput{}, nil
	}
	page := f.resourcePages[f.resourceCalls]
	f.resourceCalls++
	return page, nil
}

func (f *fakeLakeFormationAPI) ListPermissions(
	_ context.Context,
	_ *awslf.ListPermissionsInput,
	_ ...func(*awslf.Options),
) (*awslf.ListPermissionsOutput, error) {
	if f.permissionCalls >= len(f.permissionPages) {
		return &awslf.ListPermissionsOutput{}, nil
	}
	page := f.permissionPages[f.permissionCalls]
	f.permissionCalls++
	return page, nil
}

var _ apiClient = (*fakeLakeFormationAPI)(nil)
