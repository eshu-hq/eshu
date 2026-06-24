// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslf "github.com/aws/aws-sdk-go-v2/service/lakeformation"
	awslftypes "github.com/aws/aws-sdk-go-v2/service/lakeformation/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	lfservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lakeformation"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK Lake Formation read surface the adapter
// consumes. It deliberately lists only the three metadata read operations the
// scanner contract allows. Grant/revoke, register/deregister, settings-put, and
// every other mutation are absent by construction, which the reflection guard
// test asserts so a future SDK refactor cannot quietly broaden the contract.
type apiClient interface {
	GetDataLakeSettings(context.Context, *awslf.GetDataLakeSettingsInput, ...func(*awslf.Options)) (*awslf.GetDataLakeSettingsOutput, error)
	ListResources(context.Context, *awslf.ListResourcesInput, ...func(*awslf.Options)) (*awslf.ListResourcesOutput, error)
	ListPermissions(context.Context, *awslf.ListPermissionsInput, ...func(*awslf.Options)) (*awslf.ListPermissionsOutput, error)
}

// Client adapts AWS SDK Lake Formation reads into scanner-owned metadata. The
// adapter never grants, revokes, registers, deregisters, or puts settings, and
// it never carries a permission condition expression, an LF-Tag value, or a
// policy body: only grant identities, principal identifiers, and resource ARNs
// survive.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Lake Formation SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awslf.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// GetDataLakeSettings reads the data-lake administrator and read-only
// administrator principal identifiers for the boundary. Default-database and
// default-table permission bodies, session-tag values, and external-data-access
// flags stay outside the scanner contract.
func (c *Client) GetDataLakeSettings(ctx context.Context) (lfservice.Settings, error) {
	var output *awslf.GetDataLakeSettingsOutput
	err := c.recordAPICall(ctx, "GetDataLakeSettings", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetDataLakeSettings(callCtx, &awslf.GetDataLakeSettingsInput{})
		return err
	})
	if err != nil {
		return lfservice.Settings{}, err
	}
	if output == nil || output.DataLakeSettings == nil {
		return lfservice.Settings{}, nil
	}
	return lfservice.Settings{
		Admins:         principalIdentifiers(output.DataLakeSettings.DataLakeAdmins),
		ReadOnlyAdmins: principalIdentifiers(output.DataLakeSettings.ReadOnlyAdmins),
	}, nil
}

// ListResources reads the registered data-location entries for the boundary.
func (c *Client) ListResources(ctx context.Context) ([]lfservice.RegisteredResource, error) {
	var resources []lfservice.RegisteredResource
	var nextToken *string
	for {
		var page *awslf.ListResourcesOutput
		err := c.recordAPICall(ctx, "ListResources", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListResources(callCtx, &awslf.ListResourcesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return resources, nil
		}
		for _, info := range page.ResourceInfoList {
			resources = append(resources, mapRegisteredResource(info))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return resources, nil
		}
	}
}

// ListPermissions reads the principal/resource permission grants for the
// boundary. The adapter drops the condition expression and any LF-Tag value;
// only the principal identifier, the governed resource reference, and the
// bounded privilege enum names survive.
func (c *Client) ListPermissions(ctx context.Context) ([]lfservice.Permission, error) {
	var permissions []lfservice.Permission
	var nextToken *string
	for {
		var page *awslf.ListPermissionsOutput
		err := c.recordAPICall(ctx, "ListPermissions", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPermissions(callCtx, &awslf.ListPermissionsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return permissions, nil
		}
		for _, grant := range page.PrincipalResourcePermissions {
			permissions = append(permissions, mapPermission(grant))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return permissions, nil
		}
	}
}

func mapRegisteredResource(info awslftypes.ResourceInfo) lfservice.RegisteredResource {
	return lfservice.RegisteredResource{
		ResourceARN:                  strings.TrimSpace(aws.ToString(info.ResourceArn)),
		RoleARN:                      strings.TrimSpace(aws.ToString(info.RoleArn)),
		HybridAccessEnabled:          aws.ToBool(info.HybridAccessEnabled),
		WithFederation:               aws.ToBool(info.WithFederation),
		WithPrivilegedAccess:         aws.ToBool(info.WithPrivilegedAccess),
		VerificationStatus:           strings.TrimSpace(string(info.VerificationStatus)),
		ExpectedResourceOwnerAccount: strings.TrimSpace(aws.ToString(info.ExpectedResourceOwnerAccount)),
		LastModified:                 aws.ToTime(info.LastModified),
	}
}

// mapPermission projects one grant into the scanner-owned view, dropping the
// condition expression, LF-Tag values, and AdditionalDetails (a RAM share ARN)
// so no policy body or LF-Tag value leaves the adapter.
func mapPermission(grant awslftypes.PrincipalResourcePermissions) lfservice.Permission {
	permission := lfservice.Permission{
		Privileges:          permissionNames(grant.Permissions),
		GrantablePrivileges: permissionNames(grant.PermissionsWithGrantOption),
		LastUpdated:         aws.ToTime(grant.LastUpdated),
	}
	if grant.Principal != nil {
		permission.PrincipalID = strings.TrimSpace(aws.ToString(grant.Principal.DataLakePrincipalIdentifier))
	}
	applyResource(&permission, grant.Resource)
	return permission
}

// applyResource sets the governed-resource fields and resource kind from the
// grant's Resource reference. It reads only resource identity (names, ARNs,
// catalog id); it never reads LF-Tag values or LF-Tag policy expressions.
func applyResource(permission *lfservice.Permission, resource *awslftypes.Resource) {
	if resource == nil {
		return
	}
	switch {
	case resource.Table != nil:
		permission.ResourceKind = "table"
		permission.DatabaseName = strings.TrimSpace(aws.ToString(resource.Table.DatabaseName))
		permission.TableName = strings.TrimSpace(aws.ToString(resource.Table.Name))
		permission.CatalogID = strings.TrimSpace(aws.ToString(resource.Table.CatalogId))
		if resource.Table.TableWildcard != nil {
			permission.TableWildcard = true
		}
	case resource.TableWithColumns != nil:
		permission.ResourceKind = "table"
		permission.DatabaseName = strings.TrimSpace(aws.ToString(resource.TableWithColumns.DatabaseName))
		permission.TableName = strings.TrimSpace(aws.ToString(resource.TableWithColumns.Name))
		permission.CatalogID = strings.TrimSpace(aws.ToString(resource.TableWithColumns.CatalogId))
	case resource.Database != nil:
		permission.ResourceKind = "database"
		permission.DatabaseName = strings.TrimSpace(aws.ToString(resource.Database.Name))
		permission.CatalogID = strings.TrimSpace(aws.ToString(resource.Database.CatalogId))
	case resource.DataLocation != nil:
		permission.ResourceKind = "data_location"
		permission.DataLocationARN = strings.TrimSpace(aws.ToString(resource.DataLocation.ResourceArn))
		permission.CatalogID = strings.TrimSpace(aws.ToString(resource.DataLocation.CatalogId))
	case resource.Catalog != nil:
		permission.ResourceKind = "catalog"
	case resource.LFTag != nil || resource.LFTagPolicy != nil || resource.LFTagExpression != nil:
		// LF-Tag and LF-Tag policy resources carry tag keys and values that may
		// be sensitive; only the kind is recorded, never the tag value or the
		// policy expression.
		permission.ResourceKind = "lf_tag"
	case resource.DataCellsFilter != nil:
		permission.ResourceKind = "data_cells_filter"
	}
}

// permissionNames returns the trimmed, lexicographically sorted set of AWS
// privilege enum names. Sorting is required because the order AWS returns
// privileges in is not stable, which would otherwise make the `privileges`
// fact payload vary across scans of identical Lake Formation state.
func permissionNames(input []awslftypes.Permission) []string {
	if len(input) == 0 {
		return nil
	}
	names := make([]string, 0, len(input))
	for _, value := range input {
		if name := strings.TrimSpace(string(value)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return names
}

// principalIdentifiers returns the trimmed administrator principal identifiers.
// Only the identifier string survives; no other principal metadata is read.
func principalIdentifiers(principals []awslftypes.DataLakePrincipal) []string {
	if len(principals) == 0 {
		return nil
	}
	identifiers := make([]string, 0, len(principals))
	for _, principal := range principals {
		if id := strings.TrimSpace(aws.ToString(principal.DataLakePrincipalIdentifier)); id != "" {
			identifiers = append(identifiers, id)
		}
	}
	if len(identifiers) == 0 {
		return nil
	}
	return identifiers
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ lfservice.Client = (*Client)(nil)

var _ apiClient = (*awslf.Client)(nil)
