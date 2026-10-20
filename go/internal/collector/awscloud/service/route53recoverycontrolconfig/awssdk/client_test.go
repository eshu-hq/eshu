// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarc "github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig"
	awsarctypes "github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsRecoveryControlMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:route53-recovery-control::123456789012:cluster/abcd1234"
	panelARN := "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678"
	controlARN := "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678/routingcontrol/ijkl9012"
	ruleARN := "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678/safetyrule/mnop3456"

	api := &fakeARCAPI{
		clusterPages: []*awsarc.ListClustersOutput{{
			Clusters: []awsarctypes.Cluster{{
				ClusterArn:  aws.String(clusterARN),
				Name:        aws.String("prod-failover"),
				Status:      awsarctypes.StatusDeployed,
				NetworkType: awsarctypes.NetworkTypeDualstack,
				Owner:       aws.String("123456789012"),
				ClusterEndpoints: []awsarctypes.ClusterEndpoint{
					{Endpoint: aws.String("https://secret-endpoint.example"), Region: aws.String("us-east-1")},
					{Region: aws.String("us-west-2")},
				},
			}},
		}},
		panelPages: map[string][]*awsarc.ListControlPanelsOutput{
			clusterARN: {{
				ControlPanels: []awsarctypes.ControlPanel{{
					ControlPanelArn:     aws.String(panelARN),
					ClusterArn:          aws.String(clusterARN),
					Name:                aws.String("main-panel"),
					Status:              awsarctypes.StatusDeployed,
					DefaultControlPanel: aws.Bool(true),
					RoutingControlCount: aws.Int32(1),
					Owner:               aws.String("123456789012"),
				}},
			}},
		},
		controlPages: map[string][]*awsarc.ListRoutingControlsOutput{
			panelARN: {{
				RoutingControls: []awsarctypes.RoutingControl{{
					RoutingControlArn: aws.String(controlARN),
					ControlPanelArn:   aws.String(panelARN),
					Name:              aws.String("us-east-1-control"),
					Status:            awsarctypes.StatusDeployed,
					Owner:             aws.String("123456789012"),
				}},
			}},
		},
		rulePages: map[string][]*awsarc.ListSafetyRulesOutput{
			panelARN: {{
				SafetyRules: []awsarctypes.Rule{{
					ASSERTION: &awsarctypes.AssertionRule{
						SafetyRuleArn:    aws.String(ruleARN),
						ControlPanelArn:  aws.String(panelARN),
						Name:             aws.String("min-one-region"),
						Status:           awsarctypes.StatusDeployed,
						WaitPeriodMs:     aws.Int32(5000),
						AssertedControls: []string{controlARN, controlARN + "-2"},
						RuleConfig: &awsarctypes.RuleConfig{
							Type:      awsarctypes.RuleTypeAtleast,
							Threshold: aws.Int32(1),
							Inverted:  aws.Bool(false),
						},
					},
				}},
			}},
		},
		tags: map[string]map[string]string{
			clusterARN: {"Environment": "prod"},
			panelARN:   {"Team": "sre"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Clusters) != 1 {
		t.Fatalf("len(Clusters) = %d, want 1", len(snapshot.Clusters))
	}
	cluster := snapshot.Clusters[0]
	if cluster.ARN != clusterARN {
		t.Fatalf("cluster ARN = %q, want %q", cluster.ARN, clusterARN)
	}
	if cluster.Status != "DEPLOYED" {
		t.Fatalf("cluster Status = %q, want DEPLOYED", cluster.Status)
	}
	if cluster.NetworkType != "DUALSTACK" {
		t.Fatalf("cluster NetworkType = %q, want DUALSTACK", cluster.NetworkType)
	}
	if len(cluster.EndpointRegions) != 2 ||
		cluster.EndpointRegions[0] != "us-east-1" || cluster.EndpointRegions[1] != "us-west-2" {
		t.Fatalf("cluster EndpointRegions = %#v, want [us-east-1 us-west-2]", cluster.EndpointRegions)
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster tag Environment = %q, want prod", cluster.Tags["Environment"])
	}
	if len(cluster.ControlPanels) != 1 {
		t.Fatalf("len(ControlPanels) = %d, want 1", len(cluster.ControlPanels))
	}
	panel := cluster.ControlPanels[0]
	if panel.ARN != panelARN || panel.ClusterARN != clusterARN {
		t.Fatalf("panel identity = (%q,%q), want (%q,%q)", panel.ARN, panel.ClusterARN, panelARN, clusterARN)
	}
	if !panel.DefaultControlPanel || panel.RoutingControlCount != 1 {
		t.Fatalf("panel default=%v count=%d, want true/1", panel.DefaultControlPanel, panel.RoutingControlCount)
	}
	if len(panel.RoutingControls) != 1 || panel.RoutingControls[0].ARN != controlARN {
		t.Fatalf("panel routing controls = %#v, want one %q", panel.RoutingControls, controlARN)
	}
	if len(panel.SafetyRules) != 1 {
		t.Fatalf("len(SafetyRules) = %d, want 1", len(panel.SafetyRules))
	}
	rule := panel.SafetyRules[0]
	if rule.ARN != ruleARN || rule.RuleKind != "ASSERTION" {
		t.Fatalf("rule identity = (%q,%q), want (%q,ASSERTION)", rule.ARN, rule.RuleKind, ruleARN)
	}
	if rule.RuleConfigType != "ATLEAST" || rule.RuleConfigThreshold != 1 || rule.AssertedControlCount != 2 {
		t.Fatalf("rule config = (%q,%d,%d), want (ATLEAST,1,2)",
			rule.RuleConfigType, rule.RuleConfigThreshold, rule.AssertedControlCount)
	}
	if rule.WaitPeriodMs != 5000 {
		t.Fatalf("rule WaitPeriodMs = %d, want 5000", rule.WaitPeriodMs)
	}
}

func TestClientMapsGatingRule(t *testing.T) {
	panelARN := "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678"
	ruleARN := "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678/safetyrule/gate"
	clusterARN := "arn:aws:route53-recovery-control::123456789012:cluster/abcd1234"
	api := &fakeARCAPI{
		clusterPages: []*awsarc.ListClustersOutput{{
			Clusters: []awsarctypes.Cluster{{ClusterArn: aws.String(clusterARN), Name: aws.String("prod")}},
		}},
		panelPages: map[string][]*awsarc.ListControlPanelsOutput{
			clusterARN: {{ControlPanels: []awsarctypes.ControlPanel{{
				ControlPanelArn: aws.String(panelARN), ClusterArn: aws.String(clusterARN), Name: aws.String("panel"),
			}}}},
		},
		rulePages: map[string][]*awsarc.ListSafetyRulesOutput{
			panelARN: {{SafetyRules: []awsarctypes.Rule{{
				GATING: &awsarctypes.GatingRule{
					SafetyRuleArn:   aws.String(ruleARN),
					ControlPanelArn: aws.String(panelARN),
					Name:            aws.String("gate"),
					Status:          awsarctypes.StatusDeployed,
					GatingControls:  []string{"c1"},
					TargetControls:  []string{"t1", "t2", "t3"},
					RuleConfig:      &awsarctypes.RuleConfig{Type: awsarctypes.RuleTypeAnd, Inverted: aws.Bool(true)},
				},
			}}}},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	rule := snapshot.Clusters[0].ControlPanels[0].SafetyRules[0]
	if rule.RuleKind != "GATING" || rule.GatingControlCount != 1 || rule.TargetControlCount != 3 {
		t.Fatalf("gating rule = (%q,%d,%d), want (GATING,1,3)",
			rule.RuleKind, rule.GatingControlCount, rule.TargetControlCount)
	}
	if rule.RuleConfigType != "AND" || !rule.RuleConfigInverted {
		t.Fatalf("gating rule config = (%q,%v), want (AND,true)", rule.RuleConfigType, rule.RuleConfigInverted)
	}
}

func TestClientHandlesEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeARCAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Clusters) != 0 {
		t.Fatalf("len(Clusters) = %d, want 0 for empty account", len(snapshot.Clusters))
	}
}

type fakeARCAPI struct {
	clusterPages []*awsarc.ListClustersOutput
	clusterCall  int
	panelPages   map[string][]*awsarc.ListControlPanelsOutput
	panelCalls   map[string]int
	controlPages map[string][]*awsarc.ListRoutingControlsOutput
	controlCalls map[string]int
	rulePages    map[string][]*awsarc.ListSafetyRulesOutput
	ruleCalls    map[string]int
	tags         map[string]map[string]string
}

func (f *fakeARCAPI) ListClusters(
	_ context.Context,
	_ *awsarc.ListClustersInput,
	_ ...func(*awsarc.Options),
) (*awsarc.ListClustersOutput, error) {
	if f.clusterCall >= len(f.clusterPages) {
		return &awsarc.ListClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCall]
	f.clusterCall++
	return page, nil
}

func (f *fakeARCAPI) ListControlPanels(
	_ context.Context,
	input *awsarc.ListControlPanelsInput,
	_ ...func(*awsarc.Options),
) (*awsarc.ListControlPanelsOutput, error) {
	if f.panelCalls == nil {
		f.panelCalls = map[string]int{}
	}
	name := aws.ToString(input.ClusterArn)
	pages := f.panelPages[name]
	idx := f.panelCalls[name]
	if idx >= len(pages) {
		return &awsarc.ListControlPanelsOutput{}, nil
	}
	f.panelCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeARCAPI) ListRoutingControls(
	_ context.Context,
	input *awsarc.ListRoutingControlsInput,
	_ ...func(*awsarc.Options),
) (*awsarc.ListRoutingControlsOutput, error) {
	if f.controlCalls == nil {
		f.controlCalls = map[string]int{}
	}
	name := aws.ToString(input.ControlPanelArn)
	pages := f.controlPages[name]
	idx := f.controlCalls[name]
	if idx >= len(pages) {
		return &awsarc.ListRoutingControlsOutput{}, nil
	}
	f.controlCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeARCAPI) ListSafetyRules(
	_ context.Context,
	input *awsarc.ListSafetyRulesInput,
	_ ...func(*awsarc.Options),
) (*awsarc.ListSafetyRulesOutput, error) {
	if f.ruleCalls == nil {
		f.ruleCalls = map[string]int{}
	}
	name := aws.ToString(input.ControlPanelArn)
	pages := f.rulePages[name]
	idx := f.ruleCalls[name]
	if idx >= len(pages) {
		return &awsarc.ListSafetyRulesOutput{}, nil
	}
	f.ruleCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeARCAPI) ListTagsForResource(
	_ context.Context,
	input *awsarc.ListTagsForResourceInput,
	_ ...func(*awsarc.Options),
) (*awsarc.ListTagsForResourceOutput, error) {
	return &awsarc.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-west-2",
		ServiceKind: awscloud.ServiceRoute53RecoveryControlConfig,
	}
}
