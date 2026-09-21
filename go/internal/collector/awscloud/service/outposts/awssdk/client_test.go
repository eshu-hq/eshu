// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsoutposts "github.com/aws/aws-sdk-go-v2/service/outposts"
	awsoutpoststypes "github.com/aws/aws-sdk-go-v2/service/outposts/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsOutpostsMetadataOnly(t *testing.T) {
	outpostARN := "arn:aws:outposts:us-east-1:123456789012:outpost/op-0123456789abcdef0"
	siteARN := "arn:aws:outposts:us-east-1:123456789012:site/os-0123456789abcdef0"

	api := &fakeOutpostsAPI{
		outpostPages: []*awsoutposts.ListOutpostsOutput{{
			Outposts: []awsoutpoststypes.Outpost{{
				OutpostArn:            aws.String(outpostARN),
				OutpostId:             aws.String("op-0123456789abcdef0"),
				Name:                  aws.String("edge-rack-1"),
				LifeCycleStatus:       aws.String("ACTIVE"),
				AvailabilityZone:      aws.String("us-east-1a"),
				AvailabilityZoneId:    aws.String("use1-az1"),
				OwnerId:               aws.String("123456789012"),
				SiteId:                aws.String("os-0123456789abcdef0"),
				SiteArn:               aws.String(siteARN),
				SupportedHardwareType: awsoutpoststypes.SupportedHardwareTypeRack,
			}},
		}},
		sitePages: []*awsoutposts.ListSitesOutput{{
			Sites: []awsoutpoststypes.Site{{
				SiteArn:   aws.String(siteARN),
				SiteId:    aws.String("os-0123456789abcdef0"),
				Name:      aws.String("datacenter-east"),
				AccountId: aws.String("123456789012"),
				// Address/notes fields are present in the API record but must be
				// dropped by the adapter.
				OperatingAddressCity:        aws.String("Seattle"),
				OperatingAddressCountryCode: aws.String("US"),
				Notes:                       aws.String("loading dock B"),
			}},
		}},
		assetPages: map[string][]*awsoutposts.ListAssetsOutput{
			outpostARN: {{
				Assets: []awsoutpoststypes.AssetInfo{{
					AssetId:   aws.String("asset-1234"),
					AssetType: awsoutpoststypes.AssetTypeCompute,
					RackId:    aws.String("rack-5678"),
					ComputeAttributes: &awsoutpoststypes.ComputeAttributes{
						State: awsoutpoststypes.ComputeAssetStateActive,
					},
					AssetLocation: &awsoutpoststypes.AssetLocation{
						RackElevation: aws.Float32(14),
					},
				}},
			}},
		},
		tags: map[string]map[string]string{
			outpostARN: {"Environment": "prod"},
			siteARN:    {"Team": "infra"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Outposts) != 1 {
		t.Fatalf("len(Outposts) = %d, want 1", len(snapshot.Outposts))
	}
	outpost := snapshot.Outposts[0]
	if outpost.ARN != outpostARN {
		t.Fatalf("outpost ARN = %q, want %q", outpost.ARN, outpostARN)
	}
	if outpost.SiteARN != siteARN {
		t.Fatalf("outpost SiteARN = %q, want %q", outpost.SiteARN, siteARN)
	}
	if outpost.SupportedHardwareType != "RACK" {
		t.Fatalf("outpost SupportedHardwareType = %q, want RACK", outpost.SupportedHardwareType)
	}
	if outpost.Tags["Environment"] != "prod" {
		t.Fatalf("outpost tag Environment = %q, want prod", outpost.Tags["Environment"])
	}
	if len(outpost.Assets) != 1 {
		t.Fatalf("len(Assets) = %d, want 1", len(outpost.Assets))
	}
	asset := outpost.Assets[0]
	if asset.AssetID != "asset-1234" {
		t.Fatalf("asset AssetID = %q, want asset-1234", asset.AssetID)
	}
	if asset.AssetType != "COMPUTE" {
		t.Fatalf("asset AssetType = %q, want COMPUTE", asset.AssetType)
	}
	if asset.ComputeState != "ACTIVE" {
		t.Fatalf("asset ComputeState = %q, want ACTIVE", asset.ComputeState)
	}
	if asset.RackElevation == nil || *asset.RackElevation != 14 {
		t.Fatalf("asset RackElevation = %v, want 14", asset.RackElevation)
	}

	if len(snapshot.Sites) != 1 {
		t.Fatalf("len(Sites) = %d, want 1", len(snapshot.Sites))
	}
	site := snapshot.Sites[0]
	if site.ARN != siteARN {
		t.Fatalf("site ARN = %q, want %q", site.ARN, siteARN)
	}
	if site.Name != "datacenter-east" {
		t.Fatalf("site Name = %q, want datacenter-east", site.Name)
	}
	if site.AccountID != "123456789012" {
		t.Fatalf("site AccountID = %q, want 123456789012", site.AccountID)
	}
	if site.Tags["Team"] != "infra" {
		t.Fatalf("site tag Team = %q, want infra", site.Tags["Team"])
	}
	// The scanner-owned Site type has no field for address/notes, so the adapter
	// cannot carry them forward. This assertion documents that GetSite ran and
	// the address-bearing record was reduced to operational identity only.
	if api.getSiteCalls == 0 {
		t.Fatalf("GetSite was not called; site identity confirmation must run")
	}
	if api.getOutpostCalls == 0 {
		t.Fatalf("GetOutpost was not called; outpost identity confirmation must run")
	}
}

func TestClientPaginatesOutposts(t *testing.T) {
	first := "arn:aws:outposts:us-east-1:123456789012:outpost/op-1111111111111111a"
	second := "arn:aws:outposts:us-east-1:123456789012:outpost/op-2222222222222222b"
	api := &fakeOutpostsAPI{
		outpostPages: []*awsoutposts.ListOutpostsOutput{
			{
				Outposts:  []awsoutpoststypes.Outpost{{OutpostArn: aws.String(first), OutpostId: aws.String("op-1")}},
				NextToken: aws.String("page-2"),
			},
			{
				Outposts: []awsoutpoststypes.Outpost{{OutpostArn: aws.String(second), OutpostId: aws.String("op-2")}},
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Outposts) != 2 {
		t.Fatalf("len(Outposts) = %d, want 2 (pagination must drain both pages)", len(snapshot.Outposts))
	}
}

func TestClientReturnsEmptyForEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeOutpostsAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Outposts) != 0 || len(snapshot.Sites) != 0 {
		t.Fatalf("empty account snapshot = %#v, want no outposts or sites", snapshot)
	}
}

type fakeOutpostsAPI struct {
	outpostPages    []*awsoutposts.ListOutpostsOutput
	outpostCall     int
	getOutpostCalls int
	sitePages       []*awsoutposts.ListSitesOutput
	siteCall        int
	getSiteCalls    int
	assetPages      map[string][]*awsoutposts.ListAssetsOutput
	assetCalls      map[string]int
	tags            map[string]map[string]string
}

func (f *fakeOutpostsAPI) ListOutposts(
	_ context.Context,
	_ *awsoutposts.ListOutpostsInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.ListOutpostsOutput, error) {
	if f.outpostCall >= len(f.outpostPages) {
		return &awsoutposts.ListOutpostsOutput{}, nil
	}
	page := f.outpostPages[f.outpostCall]
	f.outpostCall++
	return page, nil
}

func (f *fakeOutpostsAPI) GetOutpost(
	_ context.Context,
	input *awsoutposts.GetOutpostInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.GetOutpostOutput, error) {
	f.getOutpostCalls++
	id := aws.ToString(input.OutpostId)
	for _, page := range f.outpostPages {
		for i := range page.Outposts {
			o := page.Outposts[i]
			if aws.ToString(o.OutpostArn) == id || aws.ToString(o.OutpostId) == id {
				return &awsoutposts.GetOutpostOutput{Outpost: &o}, nil
			}
		}
	}
	return &awsoutposts.GetOutpostOutput{}, nil
}

func (f *fakeOutpostsAPI) ListSites(
	_ context.Context,
	_ *awsoutposts.ListSitesInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.ListSitesOutput, error) {
	if f.siteCall >= len(f.sitePages) {
		return &awsoutposts.ListSitesOutput{}, nil
	}
	page := f.sitePages[f.siteCall]
	f.siteCall++
	return page, nil
}

func (f *fakeOutpostsAPI) GetSite(
	_ context.Context,
	input *awsoutposts.GetSiteInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.GetSiteOutput, error) {
	f.getSiteCalls++
	id := aws.ToString(input.SiteId)
	for _, page := range f.sitePages {
		for i := range page.Sites {
			s := page.Sites[i]
			if aws.ToString(s.SiteArn) == id || aws.ToString(s.SiteId) == id {
				return &awsoutposts.GetSiteOutput{Site: &s}, nil
			}
		}
	}
	return &awsoutposts.GetSiteOutput{}, nil
}

func (f *fakeOutpostsAPI) ListAssets(
	_ context.Context,
	input *awsoutposts.ListAssetsInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.ListAssetsOutput, error) {
	if f.assetCalls == nil {
		f.assetCalls = map[string]int{}
	}
	id := aws.ToString(input.OutpostIdentifier)
	pages := f.assetPages[id]
	idx := f.assetCalls[id]
	if idx >= len(pages) {
		return &awsoutposts.ListAssetsOutput{}, nil
	}
	f.assetCalls[id] = idx + 1
	return pages[idx], nil
}

func (f *fakeOutpostsAPI) ListTagsForResource(
	_ context.Context,
	input *awsoutposts.ListTagsForResourceInput,
	_ ...func(*awsoutposts.Options),
) (*awsoutposts.ListTagsForResourceOutput, error) {
	return &awsoutposts.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceOutposts,
	}
}
