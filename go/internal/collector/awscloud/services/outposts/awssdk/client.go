// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsoutposts "github.com/aws/aws-sdk-go-v2/service/outposts"
	awsoutpoststypes "github.com/aws/aws-sdk-go-v2/service/outposts/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	outpostsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/outposts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Outposts API the adapter
// calls. It is deliberately limited to control-plane list/get reads for
// outposts, sites, assets, and resource tags. It exposes no Create/Update/
// Delete/Start/Cancel mutation and no order, billing, connection, or address
// read, so the adapter cannot mutate Outposts state or read physical site
// addresses or shipping/contact details. The exclusion_test reflects over this
// interface to enforce that contract at build time.
type apiClient interface {
	ListOutposts(
		context.Context,
		*awsoutposts.ListOutpostsInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.ListOutpostsOutput, error)
	GetOutpost(
		context.Context,
		*awsoutposts.GetOutpostInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.GetOutpostOutput, error)
	ListSites(
		context.Context,
		*awsoutposts.ListSitesInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.ListSitesOutput, error)
	GetSite(
		context.Context,
		*awsoutposts.GetSiteInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.GetSiteOutput, error)
	ListAssets(
		context.Context,
		*awsoutposts.ListAssetsInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.ListAssetsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsoutposts.ListTagsForResourceInput,
		...func(*awsoutposts.Options),
	) (*awsoutposts.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Outposts control-plane calls into scanner-owned
// metadata. It never reads physical site street addresses, shipping or contact
// details, free-form site notes, or rack physical-property logistics, and never
// calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Outposts SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsoutposts.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Outposts outpost, site, and asset metadata visible to the
// configured AWS credentials. Physical site addresses, shipping or contact
// details, free-form notes, and rack physical-property logistics are never read.
func (c *Client) Snapshot(ctx context.Context) (outpostsservice.Snapshot, error) {
	sites, err := c.listSites(ctx)
	if err != nil {
		return outpostsservice.Snapshot{}, err
	}
	outposts, err := c.listOutposts(ctx)
	if err != nil {
		return outpostsservice.Snapshot{}, err
	}
	for i := range outposts {
		assets, err := c.listAssets(ctx, identifierFor(outposts[i]))
		if err != nil {
			return outpostsservice.Snapshot{}, err
		}
		outposts[i].Assets = assets
	}
	return outpostsservice.Snapshot{Outposts: outposts, Sites: sites}, nil
}

func (c *Client) listOutposts(ctx context.Context) ([]outpostsservice.Outpost, error) {
	var outposts []outpostsservice.Outpost
	var nextToken *string
	for {
		var page *awsoutposts.ListOutpostsOutput
		err := c.recordAPICall(ctx, "ListOutposts", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListOutposts(callCtx, &awsoutposts.ListOutpostsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return outposts, nil
		}
		for i := range page.Outposts {
			mapped, err := c.mapOutpost(ctx, page.Outposts[i])
			if err != nil {
				return nil, err
			}
			outposts = append(outposts, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return outposts, nil
		}
	}
}

func (c *Client) mapOutpost(
	ctx context.Context,
	outpost awsoutpoststypes.Outpost,
) (outpostsservice.Outpost, error) {
	arn := strings.TrimSpace(aws.ToString(outpost.OutpostArn))
	// GetOutpost confirms the per-outpost control-plane record; the list view
	// already carries identity, so a confirmed record only enriches it.
	if id := identifierForType(outpost); id != "" {
		var detail *awsoutposts.GetOutpostOutput
		err := c.recordAPICall(ctx, "GetOutpost", func(callCtx context.Context) error {
			var err error
			detail, err = c.client.GetOutpost(callCtx, &awsoutposts.GetOutpostInput{
				OutpostId: aws.String(id),
			})
			return err
		})
		if err != nil {
			return outpostsservice.Outpost{}, err
		}
		if detail != nil && detail.Outpost != nil {
			outpost = *detail.Outpost
			arn = strings.TrimSpace(aws.ToString(outpost.OutpostArn))
		}
	}
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return outpostsservice.Outpost{}, err
	}
	if len(tags) == 0 {
		tags = cloneStringMap(outpost.Tags)
	}
	return outpostsservice.Outpost{
		ARN:                   arn,
		OutpostID:             strings.TrimSpace(aws.ToString(outpost.OutpostId)),
		Name:                  strings.TrimSpace(aws.ToString(outpost.Name)),
		Description:           strings.TrimSpace(aws.ToString(outpost.Description)),
		LifeCycleStatus:       strings.TrimSpace(aws.ToString(outpost.LifeCycleStatus)),
		AvailabilityZone:      strings.TrimSpace(aws.ToString(outpost.AvailabilityZone)),
		AvailabilityZoneID:    strings.TrimSpace(aws.ToString(outpost.AvailabilityZoneId)),
		OwnerID:               strings.TrimSpace(aws.ToString(outpost.OwnerId)),
		SiteID:                strings.TrimSpace(aws.ToString(outpost.SiteId)),
		SiteARN:               strings.TrimSpace(aws.ToString(outpost.SiteArn)),
		SupportedHardwareType: strings.TrimSpace(string(outpost.SupportedHardwareType)),
		Tags:                  tags,
	}, nil
}

func (c *Client) listSites(ctx context.Context) ([]outpostsservice.Site, error) {
	var sites []outpostsservice.Site
	var nextToken *string
	for {
		var page *awsoutposts.ListSitesOutput
		err := c.recordAPICall(ctx, "ListSites", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSites(callCtx, &awsoutposts.ListSitesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sites, nil
		}
		for i := range page.Sites {
			mapped, err := c.mapSite(ctx, page.Sites[i])
			if err != nil {
				return nil, err
			}
			sites = append(sites, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return sites, nil
		}
	}
}

func (c *Client) mapSite(
	ctx context.Context,
	site awsoutpoststypes.Site,
) (outpostsservice.Site, error) {
	arn := strings.TrimSpace(aws.ToString(site.SiteArn))
	id := strings.TrimSpace(aws.ToString(site.SiteId))
	// GetSite confirms the operational identity record. Only the name, id, and
	// account id are copied; AWS address, country code, notes, and rack
	// physical-property fields are intentionally never read into the model.
	if confirmID := firstNonEmpty(id, arn); confirmID != "" {
		var detail *awsoutposts.GetSiteOutput
		err := c.recordAPICall(ctx, "GetSite", func(callCtx context.Context) error {
			var err error
			detail, err = c.client.GetSite(callCtx, &awsoutposts.GetSiteInput{
				SiteId: aws.String(confirmID),
			})
			return err
		})
		if err != nil {
			return outpostsservice.Site{}, err
		}
		if detail != nil && detail.Site != nil {
			site = *detail.Site
			arn = strings.TrimSpace(aws.ToString(site.SiteArn))
			id = strings.TrimSpace(aws.ToString(site.SiteId))
		}
	}
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return outpostsservice.Site{}, err
	}
	if len(tags) == 0 {
		tags = cloneStringMap(site.Tags)
	}
	return outpostsservice.Site{
		ARN:       arn,
		SiteID:    id,
		Name:      strings.TrimSpace(aws.ToString(site.Name)),
		AccountID: strings.TrimSpace(aws.ToString(site.AccountId)),
		Tags:      tags,
	}, nil
}

func (c *Client) listAssets(ctx context.Context, outpostIdentifier string) ([]outpostsservice.Asset, error) {
	outpostIdentifier = strings.TrimSpace(outpostIdentifier)
	if outpostIdentifier == "" {
		return nil, nil
	}
	var assets []outpostsservice.Asset
	var nextToken *string
	for {
		var page *awsoutposts.ListAssetsOutput
		err := c.recordAPICall(ctx, "ListAssets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAssets(callCtx, &awsoutposts.ListAssetsInput{
				OutpostIdentifier: aws.String(outpostIdentifier),
				NextToken:         nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return assets, nil
		}
		for _, asset := range page.Assets {
			assets = append(assets, mapAsset(asset))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return assets, nil
		}
	}
}

// mapAsset copies only the asset identity, type, rack id, and compute lifecycle
// state plus the rack-unit elevation. It never reads instance family, host id,
// or capacity inventory; only operational asset identity is metadata.
func mapAsset(asset awsoutpoststypes.AssetInfo) outpostsservice.Asset {
	mapped := outpostsservice.Asset{
		AssetID:   strings.TrimSpace(aws.ToString(asset.AssetId)),
		AssetType: strings.TrimSpace(string(asset.AssetType)),
		RackID:    strings.TrimSpace(aws.ToString(asset.RackId)),
	}
	if asset.ComputeAttributes != nil {
		mapped.ComputeState = strings.TrimSpace(string(asset.ComputeAttributes.State))
	}
	if asset.AssetLocation != nil && asset.AssetLocation.RackElevation != nil {
		elevation := float64(*asset.AssetLocation.RackElevation)
		mapped.RackElevation = &elevation
	}
	return mapped
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsoutposts.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsoutposts.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return cloneStringMap(output.Tags), nil
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

// identifierFor returns the ListAssets OutpostIdentifier for an already-mapped
// outpost, preferring the ARN and falling back to the short id.
func identifierFor(outpost outpostsservice.Outpost) string {
	return firstNonEmpty(outpost.ARN, outpost.OutpostID)
}

// identifierForType returns the GetOutpost identifier for an SDK outpost,
// preferring the ARN and falling back to the short id.
func identifierForType(outpost awsoutpoststypes.Outpost) string {
	return firstNonEmpty(
		strings.TrimSpace(aws.ToString(outpost.OutpostArn)),
		strings.TrimSpace(aws.ToString(outpost.OutpostId)),
	)
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

var _ outpostsservice.Client = (*Client)(nil)

var _ apiClient = (*awsoutposts.Client)(nil)
