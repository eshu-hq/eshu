// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssigner "github.com/aws/aws-sdk-go-v2/service/signer"
	awssignertypes "github.com/aws/aws-sdk-go-v2/service/signer/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	signerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/signer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Signer API the adapter calls.
// It is deliberately limited to the signing-profile and signing-platform list
// reads plus the per-profile metadata read. It exposes no StartSigningJob, no
// signing-job reads (DescribeSigningJob / ListSigningJobs), no SignPayload, and
// no Put/Add/Cancel/Revoke mutation, so the adapter cannot start a signing
// operation, read signing material, or read signed-object payloads. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	ListSigningProfiles(
		context.Context,
		*awssigner.ListSigningProfilesInput,
		...func(*awssigner.Options),
	) (*awssigner.ListSigningProfilesOutput, error)
	GetSigningProfile(
		context.Context,
		*awssigner.GetSigningProfileInput,
		...func(*awssigner.Options),
	) (*awssigner.GetSigningProfileOutput, error)
	ListSigningPlatforms(
		context.Context,
		*awssigner.ListSigningPlatformsInput,
		...func(*awssigner.Options),
	) (*awssigner.ListSigningPlatformsOutput, error)
}

// Client adapts AWS SDK Signer control-plane calls into scanner-owned metadata.
// It never starts a signing job, never reads signing material private keys, and
// never reads signed-object payloads.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Signer SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssigner.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Signer signing-profile and signing-platform metadata visible
// to the configured AWS credentials. Signing jobs, signing-material private
// keys, and signed-object payloads are never read.
func (c *Client) Snapshot(ctx context.Context) (signerservice.Snapshot, error) {
	platforms, err := c.listPlatforms(ctx)
	if err != nil {
		return signerservice.Snapshot{}, err
	}
	profiles, err := c.listProfiles(ctx)
	if err != nil {
		return signerservice.Snapshot{}, err
	}
	return signerservice.Snapshot{Profiles: profiles, Platforms: platforms}, nil
}

func (c *Client) listProfiles(ctx context.Context) ([]signerservice.SigningProfile, error) {
	var profiles []signerservice.SigningProfile
	var nextToken *string
	for {
		var page *awssigner.ListSigningProfilesOutput
		err := c.recordAPICall(ctx, "ListSigningProfiles", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSigningProfiles(callCtx, &awssigner.ListSigningProfilesInput{
				IncludeCanceled: true,
				NextToken:       nextToken,
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
	profile awssignertypes.SigningProfile,
) (signerservice.SigningProfile, error) {
	mapped := signerservice.SigningProfile{
		ARN:                   strings.TrimSpace(aws.ToString(profile.Arn)),
		ProfileVersionARN:     strings.TrimSpace(aws.ToString(profile.ProfileVersionArn)),
		Name:                  strings.TrimSpace(aws.ToString(profile.ProfileName)),
		ProfileVersion:        strings.TrimSpace(aws.ToString(profile.ProfileVersion)),
		PlatformID:            strings.TrimSpace(aws.ToString(profile.PlatformId)),
		PlatformDisplayName:   strings.TrimSpace(aws.ToString(profile.PlatformDisplayName)),
		Status:                strings.TrimSpace(string(profile.Status)),
		SigningParameterNames: parameterNames(profile.SigningParameters),
		CertificateARN:        signingMaterialARN(profile.SigningMaterial),
		Tags:                  trimTags(profile.Tags),
	}
	applyValidity(&mapped, profile.SignatureValidityPeriod)
	if err := c.enrichProfile(ctx, &mapped); err != nil {
		return signerservice.SigningProfile{}, err
	}
	return mapped, nil
}

// enrichProfile reads the per-profile metadata to resolve the signing image
// format the profile applies. The profile list omits the override; only
// GetSigningProfile reports it. It reads metadata only and never reads signing
// material or signed-object payloads.
func (c *Client) enrichProfile(ctx context.Context, profile *signerservice.SigningProfile) error {
	if profile.Name == "" {
		return nil
	}
	var output *awssigner.GetSigningProfileOutput
	err := c.recordAPICall(ctx, "GetSigningProfile", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetSigningProfile(callCtx, &awssigner.GetSigningProfileInput{
			ProfileName: aws.String(profile.Name),
		})
		return callErr
	})
	if err != nil || output == nil {
		return err
	}
	if output.Overrides != nil {
		profile.SigningImageFormat = strings.TrimSpace(string(output.Overrides.SigningImageFormat))
	}
	if profile.CertificateARN == "" {
		profile.CertificateARN = signingMaterialARN(output.SigningMaterial)
	}
	return nil
}

func (c *Client) listPlatforms(ctx context.Context) ([]signerservice.SigningPlatform, error) {
	var platforms []signerservice.SigningPlatform
	var nextToken *string
	for {
		var page *awssigner.ListSigningPlatformsOutput
		err := c.recordAPICall(ctx, "ListSigningPlatforms", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSigningPlatforms(callCtx, &awssigner.ListSigningPlatformsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return platforms, nil
		}
		for _, platform := range page.Platforms {
			platforms = append(platforms, mapPlatform(platform))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return platforms, nil
		}
	}
}

func mapPlatform(platform awssignertypes.SigningPlatform) signerservice.SigningPlatform {
	return signerservice.SigningPlatform{
		PlatformID:          strings.TrimSpace(aws.ToString(platform.PlatformId)),
		DisplayName:         strings.TrimSpace(aws.ToString(platform.DisplayName)),
		Category:            strings.TrimSpace(string(platform.Category)),
		Target:              strings.TrimSpace(aws.ToString(platform.Target)),
		Partner:             strings.TrimSpace(aws.ToString(platform.Partner)),
		MaxSizeInMB:         platform.MaxSizeInMB,
		RevocationSupported: platform.RevocationSupported,
	}
}

// signingMaterialARN returns the ACM certificate ARN reported by a signing
// material, or "" when none. Only the ARN reference is read; the certificate
// body and private key are never touched.
func signingMaterialARN(material *awssignertypes.SigningMaterial) string {
	if material == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(material.CertificateArn))
}

// applyValidity copies the signature validity unit and value onto the profile,
// leaving them unset when AWS reports no validity period.
func applyValidity(profile *signerservice.SigningProfile, period *awssignertypes.SignatureValidityPeriod) {
	if period == nil {
		return
	}
	profile.SignatureValidityType = strings.TrimSpace(string(period.Type))
	profile.SignatureValidityValue = period.Value
}

// parameterNames returns the sorted-by-insertion names of a profile's signing
// parameters. The values are intentionally dropped because they can carry
// user-supplied data; only the names are metadata.
func parameterNames(parameters map[string]string) []string {
	if len(parameters) == 0 {
		return nil
	}
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func trimTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
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

var _ signerservice.Client = (*Client)(nil)

var _ apiClient = (*awssigner.Client)(nil)
