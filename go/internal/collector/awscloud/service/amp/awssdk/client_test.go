// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsamp "github.com/aws/aws-sdk-go-v2/service/amp"
	awsamptypes "github.com/aws/aws-sdk-go-v2/service/amp/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsAMPMetadataOnly(t *testing.T) {
	workspaceARN := "arn:aws:aps:us-east-1:123456789012:workspace/ws-1234"
	workspaceID := "ws-1234"
	namespaceARN := "arn:aws:aps:us-east-1:123456789012:rulegroupsnamespace/ws-1234/alerts"
	scraperARN := "arn:aws:aps:us-east-1:123456789012:scraper/s-5678"
	clusterARN := "arn:aws:eks:us-east-1:123456789012:cluster/prod"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeAMPAPI{
		workspacePages: []*awsamp.ListWorkspacesOutput{{
			Workspaces: []awsamptypes.WorkspaceSummary{{
				Arn:         aws.String(workspaceARN),
				WorkspaceId: aws.String(workspaceID),
				Alias:       aws.String("platform-metrics"),
				KmsKeyArn:   aws.String(kmsARN),
				CreatedAt:   aws.Time(createdAt),
				Status:      &awsamptypes.WorkspaceStatus{StatusCode: awsamptypes.WorkspaceStatusCodeActive},
				Tags:        map[string]string{"Environment": "prod"},
			}},
		}},
		namespacePages: map[string][]*awsamp.ListRuleGroupsNamespacesOutput{
			workspaceID: {{
				RuleGroupsNamespaces: []awsamptypes.RuleGroupsNamespaceSummary{{
					Arn:        aws.String(namespaceARN),
					Name:       aws.String("alerts"),
					CreatedAt:  aws.Time(createdAt),
					ModifiedAt: aws.Time(createdAt),
					Status:     &awsamptypes.RuleGroupsNamespaceStatus{StatusCode: awsamptypes.RuleGroupsNamespaceStatusCodeActive},
				}},
			}},
		},
		scraperPages: []*awsamp.ListScrapersOutput{{
			Scrapers: []awsamptypes.ScraperSummary{{
				Arn:       aws.String(scraperARN),
				ScraperId: aws.String("s-5678"),
				Alias:     aws.String("prod-collector"),
				RoleArn:   aws.String("arn:aws:iam::123456789012:role/aps-scraper"),
				CreatedAt: aws.Time(createdAt),
				Status:    &awsamptypes.ScraperStatus{StatusCode: awsamptypes.ScraperStatusCodeActive},
				Source: &awsamptypes.SourceMemberEksConfiguration{Value: awsamptypes.EksConfiguration{
					ClusterArn:       aws.String(clusterARN),
					SubnetIds:        []string{"subnet-aaaa1111", "subnet-bbbb2222"},
					SecurityGroupIds: []string{"sg-cccc3333"},
				}},
				Destination: &awsamptypes.DestinationMemberAmpConfiguration{Value: awsamptypes.AmpConfiguration{
					WorkspaceArn: aws.String(workspaceARN),
				}},
			}},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Workspaces) != 1 {
		t.Fatalf("len(Workspaces) = %d, want 1", len(snapshot.Workspaces))
	}
	workspace := snapshot.Workspaces[0]
	if workspace.ARN != workspaceARN {
		t.Fatalf("workspace ARN = %q, want %q", workspace.ARN, workspaceARN)
	}
	if workspace.Status != "ACTIVE" {
		t.Fatalf("workspace Status = %q, want ACTIVE", workspace.Status)
	}
	if workspace.KMSKeyARN != kmsARN {
		t.Fatalf("workspace KMSKeyARN = %q, want %q", workspace.KMSKeyARN, kmsARN)
	}
	if workspace.Tags["Environment"] != "prod" {
		t.Fatalf("workspace tag Environment = %q, want prod", workspace.Tags["Environment"])
	}
	if len(workspace.RuleGroupsNamespaces) != 1 {
		t.Fatalf("len(RuleGroupsNamespaces) = %d, want 1", len(workspace.RuleGroupsNamespaces))
	}
	namespace := workspace.RuleGroupsNamespaces[0]
	if namespace.Name != "alerts" {
		t.Fatalf("namespace Name = %q, want alerts", namespace.Name)
	}
	if namespace.Status != "ACTIVE" {
		t.Fatalf("namespace Status = %q, want ACTIVE", namespace.Status)
	}

	if len(snapshot.Scrapers) != 1 {
		t.Fatalf("len(Scrapers) = %d, want 1", len(snapshot.Scrapers))
	}
	scraper := snapshot.Scrapers[0]
	if scraper.ARN != scraperARN {
		t.Fatalf("scraper ARN = %q, want %q", scraper.ARN, scraperARN)
	}
	if scraper.SourceEKSClusterARN != clusterARN {
		t.Fatalf("scraper SourceEKSClusterARN = %q, want %q", scraper.SourceEKSClusterARN, clusterARN)
	}
	if scraper.DestinationWorkspaceARN != workspaceARN {
		t.Fatalf("scraper DestinationWorkspaceARN = %q, want %q", scraper.DestinationWorkspaceARN, workspaceARN)
	}
	if len(scraper.SubnetIDs) != 2 || scraper.SubnetIDs[0] != "subnet-aaaa1111" {
		t.Fatalf("scraper SubnetIDs = %#v, want [subnet-aaaa1111 subnet-bbbb2222]", scraper.SubnetIDs)
	}
	if len(scraper.SecurityGroupIDs) != 1 || scraper.SecurityGroupIDs[0] != "sg-cccc3333" {
		t.Fatalf("scraper SecurityGroupIDs = %#v, want [sg-cccc3333]", scraper.SecurityGroupIDs)
	}
}

func TestClientSnapshotsScraperWithoutEKSSource(t *testing.T) {
	// A non-EKS (MSK/VPC) scraper source carries no EKS cluster or EKS VPC
	// configuration ids, so the adapter yields empty source fields rather than
	// fabricating an EKS edge.
	api := &fakeAMPAPI{
		scraperPages: []*awsamp.ListScrapersOutput{{
			Scrapers: []awsamptypes.ScraperSummary{{
				Arn:       aws.String("arn:aws:aps:us-east-1:123456789012:scraper/s-msk"),
				ScraperId: aws.String("s-msk"),
				Status:    &awsamptypes.ScraperStatus{StatusCode: awsamptypes.ScraperStatusCodeActive},
				Source: &awsamptypes.SourceMemberVpcConfiguration{Value: awsamptypes.VpcConfiguration{
					SubnetIds:        []string{"subnet-zzzz"},
					SecurityGroupIds: []string{"sg-zzzz"},
				}},
				Destination: &awsamptypes.DestinationMemberAmpConfiguration{Value: awsamptypes.AmpConfiguration{
					WorkspaceArn: aws.String("arn:aws:aps:us-east-1:123456789012:workspace/ws-1234"),
				}},
			}},
		}},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Scrapers) != 1 {
		t.Fatalf("len(Scrapers) = %d, want 1", len(snapshot.Scrapers))
	}
	scraper := snapshot.Scrapers[0]
	if scraper.SourceEKSClusterARN != "" {
		t.Fatalf("scraper SourceEKSClusterARN = %q, want empty for non-EKS source", scraper.SourceEKSClusterARN)
	}
	if scraper.SubnetIDs != nil || scraper.SecurityGroupIDs != nil {
		t.Fatalf("scraper VPC ids = %#v/%#v, want nil for non-EKS source", scraper.SubnetIDs, scraper.SecurityGroupIDs)
	}
}

func TestClientReturnsCleanlyForEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeAMPAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Workspaces) != 0 || len(snapshot.Scrapers) != 0 {
		t.Fatalf("Snapshot() = %#v, want empty for empty account", snapshot)
	}
}

// TestSnapshotWrapsEachListErrorWithItsOperation proves Snapshot reports the
// operation that actually failed. A failure while listing rule-groups
// namespaces or scrapers must not be mislabeled as a workspace-list failure, so
// an operator reading the error sees the real failing AMP API.
func TestSnapshotWrapsEachListErrorWithItsOperation(t *testing.T) {
	workspaceARN := "arn:aws:aps:us-east-1:123456789012:workspace/ws-1234"
	workspaceID := "ws-1234"
	boom := errors.New("boom")

	cases := []struct {
		name    string
		api     *fakeAMPAPI
		wantsub string
	}{
		{
			name:    "workspace list failure",
			api:     &fakeAMPAPI{workspaceErr: boom},
			wantsub: "list AMP workspaces",
		},
		{
			name: "namespace list failure",
			api: &fakeAMPAPI{
				workspacePages: []*awsamp.ListWorkspacesOutput{{
					Workspaces: []awsamptypes.WorkspaceSummary{{
						Arn:         aws.String(workspaceARN),
						WorkspaceId: aws.String(workspaceID),
					}},
				}},
				namespaceErr: boom,
			},
			wantsub: "list AMP rule-groups namespaces",
		},
		{
			name:    "scraper list failure",
			api:     &fakeAMPAPI{scraperErr: boom},
			wantsub: "list AMP scrapers",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{client: tc.api, boundary: testBoundary()}
			_, err := client.Snapshot(context.Background())
			if err == nil {
				t.Fatalf("Snapshot() error = nil, want %q", tc.wantsub)
			}
			if !strings.Contains(err.Error(), tc.wantsub) {
				t.Fatalf("Snapshot() error = %q, want it to contain %q", err.Error(), tc.wantsub)
			}
			if !errors.Is(err, boom) {
				t.Fatalf("Snapshot() error = %q, want it to wrap the underlying SDK error", err.Error())
			}
		})
	}
}

type fakeAMPAPI struct {
	workspacePages []*awsamp.ListWorkspacesOutput
	workspaceCall  int
	workspaceErr   error
	namespacePages map[string][]*awsamp.ListRuleGroupsNamespacesOutput
	namespaceCalls map[string]int
	namespaceErr   error
	scraperPages   []*awsamp.ListScrapersOutput
	scraperCall    int
	scraperErr     error
}

func (f *fakeAMPAPI) ListWorkspaces(
	_ context.Context,
	_ *awsamp.ListWorkspacesInput,
	_ ...func(*awsamp.Options),
) (*awsamp.ListWorkspacesOutput, error) {
	if f.workspaceErr != nil {
		return nil, f.workspaceErr
	}
	if f.workspaceCall >= len(f.workspacePages) {
		return &awsamp.ListWorkspacesOutput{}, nil
	}
	page := f.workspacePages[f.workspaceCall]
	f.workspaceCall++
	return page, nil
}

func (f *fakeAMPAPI) ListRuleGroupsNamespaces(
	_ context.Context,
	input *awsamp.ListRuleGroupsNamespacesInput,
	_ ...func(*awsamp.Options),
) (*awsamp.ListRuleGroupsNamespacesOutput, error) {
	if f.namespaceErr != nil {
		return nil, f.namespaceErr
	}
	if f.namespaceCalls == nil {
		f.namespaceCalls = map[string]int{}
	}
	id := aws.ToString(input.WorkspaceId)
	pages := f.namespacePages[id]
	idx := f.namespaceCalls[id]
	if idx >= len(pages) {
		return &awsamp.ListRuleGroupsNamespacesOutput{}, nil
	}
	f.namespaceCalls[id] = idx + 1
	return pages[idx], nil
}

func (f *fakeAMPAPI) ListScrapers(
	_ context.Context,
	_ *awsamp.ListScrapersInput,
	_ ...func(*awsamp.Options),
) (*awsamp.ListScrapersOutput, error) {
	if f.scraperErr != nil {
		return nil, f.scraperErr
	}
	if f.scraperCall >= len(f.scraperPages) {
		return &awsamp.ListScrapersOutput{}, nil
	}
	page := f.scraperPages[f.scraperCall]
	f.scraperCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceAMP,
	}
}
