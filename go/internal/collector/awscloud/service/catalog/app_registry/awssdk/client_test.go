// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappregistry "github.com/aws/aws-sdk-go-v2/service/servicecatalogappregistry"
	awsappregistrytypes "github.com/aws/aws-sdk-go-v2/service/servicecatalogappregistry/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsAppRegistryMetadataOnly(t *testing.T) {
	appARN := "arn:aws:servicecatalog:us-east-1:123456789012:/applications/app-0abc123"
	groupARN := "arn:aws:servicecatalog:us-east-1:123456789012:/attribute-groups/ag-0def456"
	stackARN := "arn:aws:cloudformation:us-east-1:123456789012:stack/prod-network/abc-123"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeAppRegistryAPI{
		applicationPages: []*awsappregistry.ListApplicationsOutput{{
			Applications: []awsappregistrytypes.ApplicationSummary{{
				Id:             aws.String("app-0abc123"),
				Arn:            aws.String(appARN),
				Name:           aws.String("payments"),
				Description:    aws.String("Payments application"),
				CreationTime:   aws.Time(createdAt),
				LastUpdateTime: aws.Time(createdAt),
			}},
		}},
		attributeGroupPages: []*awsappregistry.ListAttributeGroupsOutput{{
			AttributeGroups: []awsappregistrytypes.AttributeGroupSummary{{
				Id:             aws.String("ag-0def456"),
				Arn:            aws.String(groupARN),
				Name:           aws.String("cost-center"),
				Description:    aws.String("Cost center metadata"),
				CreationTime:   aws.Time(createdAt),
				LastUpdateTime: aws.Time(createdAt),
			}},
		}},
		appGroupPages: map[string][]*awsappregistry.ListAttributeGroupsForApplicationOutput{
			"app-0abc123": {{
				AttributeGroupsDetails: []awsappregistrytypes.AttributeGroupDetails{{
					Id:  aws.String("ag-0def456"),
					Arn: aws.String(groupARN),
				}},
			}},
		},
		associatedPages: map[string][]*awsappregistry.ListAssociatedResourcesOutput{
			"app-0abc123": {{
				Resources: []awsappregistrytypes.ResourceInfo{{
					Arn:          aws.String(stackARN),
					Name:         aws.String("prod-network"),
					ResourceType: awsappregistrytypes.ResourceTypeCfnStack,
					// ResourceDetails carries a tag value that must never be read.
					ResourceDetails: &awsappregistrytypes.ResourceDetails{
						TagValue: aws.String("super-secret-tag-value"),
					},
				}},
			}},
		},
		tags: map[string]map[string]string{
			appARN:   {"Environment": "prod"},
			groupARN: {"Team": "platform"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.AttributeGroups) != 1 {
		t.Fatalf("len(AttributeGroups) = %d, want 1", len(snapshot.AttributeGroups))
	}
	group := snapshot.AttributeGroups[0]
	if group.ARN != groupARN {
		t.Fatalf("group ARN = %q, want %q", group.ARN, groupARN)
	}
	if group.Tags["Team"] != "platform" {
		t.Fatalf("group tag Team = %q, want platform", group.Tags["Team"])
	}
	if len(snapshot.Applications) != 1 {
		t.Fatalf("len(Applications) = %d, want 1", len(snapshot.Applications))
	}
	app := snapshot.Applications[0]
	if app.ARN != appARN {
		t.Fatalf("application ARN = %q, want %q", app.ARN, appARN)
	}
	if app.Tags["Environment"] != "prod" {
		t.Fatalf("application tag Environment = %q, want prod", app.Tags["Environment"])
	}
	if len(app.AttributeGroupARNs) != 1 || app.AttributeGroupARNs[0] != groupARN {
		t.Fatalf("AttributeGroupARNs = %#v, want [%q]", app.AttributeGroupARNs, groupARN)
	}
	if len(app.AssociatedResources) != 1 {
		t.Fatalf("len(AssociatedResources) = %d, want 1", len(app.AssociatedResources))
	}
	resource := app.AssociatedResources[0]
	if resource.ARN != stackARN {
		t.Fatalf("associated resource ARN = %q, want %q", resource.ARN, stackARN)
	}
	if resource.ResourceType != "CFN_STACK" {
		t.Fatalf("associated resource type = %q, want CFN_STACK", resource.ResourceType)
	}
}

type fakeAppRegistryAPI struct {
	applicationPages    []*awsappregistry.ListApplicationsOutput
	applicationCall     int
	attributeGroupPages []*awsappregistry.ListAttributeGroupsOutput
	attributeGroupCall  int
	appGroupPages       map[string][]*awsappregistry.ListAttributeGroupsForApplicationOutput
	appGroupCalls       map[string]int
	associatedPages     map[string][]*awsappregistry.ListAssociatedResourcesOutput
	associatedCalls     map[string]int
	tags                map[string]map[string]string
}

func (f *fakeAppRegistryAPI) ListApplications(
	_ context.Context,
	_ *awsappregistry.ListApplicationsInput,
	_ ...func(*awsappregistry.Options),
) (*awsappregistry.ListApplicationsOutput, error) {
	if f.applicationCall >= len(f.applicationPages) {
		return &awsappregistry.ListApplicationsOutput{}, nil
	}
	page := f.applicationPages[f.applicationCall]
	f.applicationCall++
	return page, nil
}

func (f *fakeAppRegistryAPI) ListAttributeGroups(
	_ context.Context,
	_ *awsappregistry.ListAttributeGroupsInput,
	_ ...func(*awsappregistry.Options),
) (*awsappregistry.ListAttributeGroupsOutput, error) {
	if f.attributeGroupCall >= len(f.attributeGroupPages) {
		return &awsappregistry.ListAttributeGroupsOutput{}, nil
	}
	page := f.attributeGroupPages[f.attributeGroupCall]
	f.attributeGroupCall++
	return page, nil
}

func (f *fakeAppRegistryAPI) ListAttributeGroupsForApplication(
	_ context.Context,
	input *awsappregistry.ListAttributeGroupsForApplicationInput,
	_ ...func(*awsappregistry.Options),
) (*awsappregistry.ListAttributeGroupsForApplicationOutput, error) {
	if f.appGroupCalls == nil {
		f.appGroupCalls = map[string]int{}
	}
	name := aws.ToString(input.Application)
	pages := f.appGroupPages[name]
	idx := f.appGroupCalls[name]
	if idx >= len(pages) {
		return &awsappregistry.ListAttributeGroupsForApplicationOutput{}, nil
	}
	f.appGroupCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeAppRegistryAPI) ListAssociatedResources(
	_ context.Context,
	input *awsappregistry.ListAssociatedResourcesInput,
	_ ...func(*awsappregistry.Options),
) (*awsappregistry.ListAssociatedResourcesOutput, error) {
	if f.associatedCalls == nil {
		f.associatedCalls = map[string]int{}
	}
	name := aws.ToString(input.Application)
	pages := f.associatedPages[name]
	idx := f.associatedCalls[name]
	if idx >= len(pages) {
		return &awsappregistry.ListAssociatedResourcesOutput{}, nil
	}
	f.associatedCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeAppRegistryAPI) ListTagsForResource(
	_ context.Context,
	input *awsappregistry.ListTagsForResourceInput,
	_ ...func(*awsappregistry.Options),
) (*awsappregistry.ListTagsForResourceOutput, error) {
	return &awsappregistry.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceServiceCatalogAppRegistry,
	}
}
