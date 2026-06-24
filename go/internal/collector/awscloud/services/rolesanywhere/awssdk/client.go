// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrolesanywhere "github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	awsrolesanywheretypes "github.com/aws/aws-sdk-go-v2/service/rolesanywhere/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	rolesanywhereservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rolesanywhere"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS IAM Roles Anywhere API the
// adapter calls. It is deliberately limited to the trust-anchor, profile, and
// CRL list reads and resource-tag reads. It exposes no GetCrl (which returns the
// CRL body bytes), no GetSubject/ListSubjects (which expose vended session
// credentials), and no Create/Update/Delete/Import/Enable/Disable mutation, so
// the adapter cannot read certificate private material, CRL bodies, or session
// credentials, and cannot write Roles Anywhere state. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListTrustAnchors(
		context.Context,
		*awsrolesanywhere.ListTrustAnchorsInput,
		...func(*awsrolesanywhere.Options),
	) (*awsrolesanywhere.ListTrustAnchorsOutput, error)
	ListProfiles(
		context.Context,
		*awsrolesanywhere.ListProfilesInput,
		...func(*awsrolesanywhere.Options),
	) (*awsrolesanywhere.ListProfilesOutput, error)
	ListCrls(
		context.Context,
		*awsrolesanywhere.ListCrlsInput,
		...func(*awsrolesanywhere.Options),
	) (*awsrolesanywhere.ListCrlsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsrolesanywhere.ListTagsForResourceInput,
		...func(*awsrolesanywhere.Options),
	) (*awsrolesanywhere.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Roles Anywhere control-plane calls into scanner-owned
// metadata. It never reads certificate private material, PEM certificate
// bundles, CRL body bytes, session policy documents, or vended session
// credentials, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Roles Anywhere SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsrolesanywhere.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Roles Anywhere trust anchor, profile, and CRL metadata
// visible to the configured AWS credentials. Certificate private material, CRL
// body bytes, session policy documents, and vended session credentials are
// never read.
func (c *Client) Snapshot(ctx context.Context) (rolesanywhereservice.Snapshot, error) {
	anchors, err := c.listTrustAnchors(ctx)
	if err != nil {
		return rolesanywhereservice.Snapshot{}, err
	}
	profiles, err := c.listProfiles(ctx)
	if err != nil {
		return rolesanywhereservice.Snapshot{}, err
	}
	crls, err := c.listCRLs(ctx)
	if err != nil {
		return rolesanywhereservice.Snapshot{}, err
	}
	return rolesanywhereservice.Snapshot{
		TrustAnchors: anchors,
		Profiles:     profiles,
		CRLs:         crls,
	}, nil
}

func (c *Client) listTrustAnchors(ctx context.Context) ([]rolesanywhereservice.TrustAnchor, error) {
	var anchors []rolesanywhereservice.TrustAnchor
	var nextToken *string
	for {
		var page *awsrolesanywhere.ListTrustAnchorsOutput
		err := c.recordAPICall(ctx, "ListTrustAnchors", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTrustAnchors(callCtx, &awsrolesanywhere.ListTrustAnchorsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return anchors, nil
		}
		for _, anchor := range page.TrustAnchors {
			mapped, err := c.mapTrustAnchor(ctx, anchor)
			if err != nil {
				return nil, err
			}
			anchors = append(anchors, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return anchors, nil
		}
	}
}

func (c *Client) mapTrustAnchor(
	ctx context.Context,
	anchor awsrolesanywheretypes.TrustAnchorDetail,
) (rolesanywhereservice.TrustAnchor, error) {
	arn := strings.TrimSpace(aws.ToString(anchor.TrustAnchorArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return rolesanywhereservice.TrustAnchor{}, err
	}
	sourceType, acmPcaARN := trustAnchorSource(anchor.Source)
	return rolesanywhereservice.TrustAnchor{
		ARN:           arn,
		TrustAnchorID: strings.TrimSpace(aws.ToString(anchor.TrustAnchorId)),
		Name:          strings.TrimSpace(aws.ToString(anchor.Name)),
		Enabled:       aws.ToBool(anchor.Enabled),
		SourceType:    sourceType,
		ACMPCAArn:     acmPcaARN,
		CreatedAt:     aws.ToTime(anchor.CreatedAt),
		UpdatedAt:     aws.ToTime(anchor.UpdatedAt),
		Tags:          tags,
	}, nil
}

// trustAnchorSource extracts the source type and, for AWS_ACM_PCA trust anchors,
// the backing CA ARN. It deliberately ignores the PEM x509 certificate bundle
// carried for CERTIFICATE_BUNDLE trust anchors so no certificate material is
// persisted.
func trustAnchorSource(source *awsrolesanywheretypes.Source) (sourceType, acmPcaARN string) {
	if source == nil {
		return "", ""
	}
	sourceType = strings.TrimSpace(string(source.SourceType))
	if data, ok := source.SourceData.(*awsrolesanywheretypes.SourceDataMemberAcmPcaArn); ok {
		acmPcaARN = strings.TrimSpace(data.Value)
	}
	return sourceType, acmPcaARN
}

func (c *Client) listProfiles(ctx context.Context) ([]rolesanywhereservice.Profile, error) {
	var profiles []rolesanywhereservice.Profile
	var nextToken *string
	for {
		var page *awsrolesanywhere.ListProfilesOutput
		err := c.recordAPICall(ctx, "ListProfiles", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListProfiles(callCtx, &awsrolesanywhere.ListProfilesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return profiles, nil
		}
		for _, profile := range page.Profiles {
			mapped, err := c.mapProfile(ctx, profile)
			if err != nil {
				return nil, err
			}
			profiles = append(profiles, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return profiles, nil
		}
	}
}

func (c *Client) mapProfile(
	ctx context.Context,
	profile awsrolesanywheretypes.ProfileDetail,
) (rolesanywhereservice.Profile, error) {
	arn := strings.TrimSpace(aws.ToString(profile.ProfileArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return rolesanywhereservice.Profile{}, err
	}
	return rolesanywhereservice.Profile{
		ARN:                       arn,
		ProfileID:                 strings.TrimSpace(aws.ToString(profile.ProfileId)),
		Name:                      strings.TrimSpace(aws.ToString(profile.Name)),
		Enabled:                   aws.ToBool(profile.Enabled),
		DurationSeconds:           aws.ToInt32(profile.DurationSeconds),
		AcceptRoleSessionName:     aws.ToBool(profile.AcceptRoleSessionName),
		RequireInstanceProperties: aws.ToBool(profile.RequireInstanceProperties),
		HasSessionPolicy:          strings.TrimSpace(aws.ToString(profile.SessionPolicy)) != "",
		AttributeMappingCount:     len(profile.AttributeMappings),
		RoleARNs:                  trimmedARNs(profile.RoleArns),
		ManagedPolicyARNs:         trimmedARNs(profile.ManagedPolicyArns),
		CreatedAt:                 aws.ToTime(profile.CreatedAt),
		UpdatedAt:                 aws.ToTime(profile.UpdatedAt),
		Tags:                      tags,
	}, nil
}

func (c *Client) listCRLs(ctx context.Context) ([]rolesanywhereservice.CRL, error) {
	var crls []rolesanywhereservice.CRL
	var nextToken *string
	for {
		var page *awsrolesanywhere.ListCrlsOutput
		err := c.recordAPICall(ctx, "ListCrls", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCrls(callCtx, &awsrolesanywhere.ListCrlsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return crls, nil
		}
		for _, crl := range page.Crls {
			mapped, err := c.mapCRL(ctx, crl)
			if err != nil {
				return nil, err
			}
			crls = append(crls, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return crls, nil
		}
	}
}

func (c *Client) mapCRL(
	ctx context.Context,
	crl awsrolesanywheretypes.CrlDetail,
) (rolesanywhereservice.CRL, error) {
	arn := strings.TrimSpace(aws.ToString(crl.CrlArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return rolesanywhereservice.CRL{}, err
	}
	// CrlData (the CRL body bytes) is intentionally never copied.
	return rolesanywhereservice.CRL{
		ARN:            arn,
		CRLID:          strings.TrimSpace(aws.ToString(crl.CrlId)),
		Name:           strings.TrimSpace(aws.ToString(crl.Name)),
		Enabled:        aws.ToBool(crl.Enabled),
		TrustAnchorARN: strings.TrimSpace(aws.ToString(crl.TrustAnchorArn)),
		CreatedAt:      aws.ToTime(crl.CreatedAt),
		UpdatedAt:      aws.ToTime(crl.UpdatedAt),
		Tags:           tags,
	}, nil
}

func trimmedARNs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsrolesanywhere.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsrolesanywhere.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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

var _ rolesanywhereservice.Client = (*Client)(nil)

var _ apiClient = (*awsrolesanywhere.Client)(nil)
