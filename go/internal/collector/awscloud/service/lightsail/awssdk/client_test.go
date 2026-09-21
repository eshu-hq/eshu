// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslightsail "github.com/aws/aws-sdk-go-v2/service/lightsail"
	awslightsailtypes "github.com/aws/aws-sdk-go-v2/service/lightsail/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListInstancesMapsSafeMetadataAndPaginates(t *testing.T) {
	client := &fakeLightsailAPI{
		instancePages: []*awslightsail.GetInstancesOutput{
			{
				Instances: []awslightsailtypes.Instance{{
					Arn:              aws.String("arn:aws:lightsail:us-east-1:123456789012:Instance/abc"),
					Name:             aws.String("web-1"),
					BlueprintId:      aws.String("amazon_linux_2023"),
					BundleId:         aws.String("micro_3_0"),
					PublicIpAddress:  aws.String("203.0.113.10"),
					PrivateIpAddress: aws.String("172.26.0.10"),
					IsStaticIp:       aws.Bool(true),
					CreatedAt:        aws.Time(time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)),
					State:            &awslightsailtypes.InstanceState{Name: aws.String("running")},
					Location: &awslightsailtypes.ResourceLocation{
						AvailabilityZone: aws.String("us-east-1a"),
						RegionName:       awslightsailtypes.RegionNameUsEast1,
					},
					Tags: []awslightsailtypes.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
				}},
				NextPageToken: aws.String("page-2"),
			},
			{
				Instances: []awslightsailtypes.Instance{{
					Arn:   aws.String("arn:aws:lightsail:us-east-1:123456789012:Instance/def"),
					Name:  aws.String("web-2"),
					State: &awslightsailtypes.InstanceState{Name: aws.String("running")},
				}},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	instances, err := adapter.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances() error = %v, want nil", err)
	}
	if got, want := len(instances), 2; got != want {
		t.Fatalf("len(instances) = %d, want %d (two pages)", got, want)
	}
	first := instances[0]
	if first.Name != "web-1" {
		t.Fatalf("instances[0].Name = %q, want web-1", first.Name)
	}
	if first.State != "running" {
		t.Fatalf("instances[0].State = %q, want running", first.State)
	}
	if first.RegionName != "us-east-1" {
		t.Fatalf("instances[0].RegionName = %q, want us-east-1", first.RegionName)
	}
	if first.Tags["env"] != "prod" {
		t.Fatalf("instances[0].Tags[env] = %q, want prod", first.Tags["env"])
	}
}

func TestClientListLoadBalancersMapsAttachedInstanceNames(t *testing.T) {
	client := &fakeLightsailAPI{
		loadBalancerPages: []*awslightsail.GetLoadBalancersOutput{{
			LoadBalancers: []awslightsailtypes.LoadBalancer{{
				Arn:   aws.String("arn:aws:lightsail:us-east-1:123456789012:LoadBalancer/ghi"),
				Name:  aws.String("web-lb"),
				State: awslightsailtypes.LoadBalancerStateActive,
				InstanceHealthSummary: []awslightsailtypes.InstanceHealthSummary{
					{InstanceName: aws.String("web-1"), InstanceHealth: awslightsailtypes.InstanceHealthStateHealthy},
					{InstanceName: aws.String("web-2"), InstanceHealth: awslightsailtypes.InstanceHealthStateHealthy},
					{InstanceName: aws.String("")},
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	loadBalancers, err := adapter.ListLoadBalancers(context.Background())
	if err != nil {
		t.Fatalf("ListLoadBalancers() error = %v, want nil", err)
	}
	if got, want := len(loadBalancers), 1; got != want {
		t.Fatalf("len(loadBalancers) = %d, want %d", got, want)
	}
	if got, want := loadBalancers[0].Attached, []string{"web-1", "web-2"}; !sliceEqual(got, want) {
		t.Fatalf("loadBalancers[0].Attached = %#v, want %#v (blank instance name dropped)", got, want)
	}
}

func TestClientListDisksAndStaticIPsMapAttachment(t *testing.T) {
	client := &fakeLightsailAPI{
		diskPages: []*awslightsail.GetDisksOutput{{
			Disks: []awslightsailtypes.Disk{{
				Arn:        aws.String("arn:aws:lightsail:us-east-1:123456789012:Disk/jkl"),
				Name:       aws.String("web-1-data"),
				State:      awslightsailtypes.DiskStateInUse,
				SizeInGb:   aws.Int32(32),
				IsAttached: aws.Bool(true),
				AttachedTo: aws.String("web-1"),
			}},
		}},
		staticIPPages: []*awslightsail.GetStaticIpsOutput{{
			StaticIps: []awslightsailtypes.StaticIp{{
				Arn:        aws.String("arn:aws:lightsail:us-east-1:123456789012:StaticIp/mno"),
				Name:       aws.String("web-1-ip"),
				IpAddress:  aws.String("203.0.113.10"),
				IsAttached: aws.Bool(true),
				AttachedTo: aws.String("web-1"),
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	disks, err := adapter.ListDisks(context.Background())
	if err != nil {
		t.Fatalf("ListDisks() error = %v, want nil", err)
	}
	if got, want := disks[0].AttachedTo, "web-1"; got != want {
		t.Fatalf("disks[0].AttachedTo = %q, want %q", got, want)
	}

	staticIPs, err := adapter.ListStaticIPs(context.Background())
	if err != nil {
		t.Fatalf("ListStaticIPs() error = %v, want nil", err)
	}
	if got, want := staticIPs[0].AttachedTo, "web-1"; got != want {
		t.Fatalf("staticIPs[0].AttachedTo = %q, want %q", got, want)
	}
}

func TestClientListDatabasesMapsEndpointWithoutMasterPassword(t *testing.T) {
	client := &fakeLightsailAPI{
		databasePages: []*awslightsail.GetRelationalDatabasesOutput{{
			RelationalDatabases: []awslightsailtypes.RelationalDatabase{{
				Arn:            aws.String("arn:aws:lightsail:us-east-1:123456789012:RelationalDatabase/def"),
				Name:           aws.String("orders-db"),
				Engine:         aws.String("mysql"),
				EngineVersion:  aws.String("8.0.32"),
				State:          aws.String("available"),
				MasterUsername: aws.String("admin"),
				MasterEndpoint: &awslightsailtypes.RelationalDatabaseEndpoint{
					Address: aws.String("orders-db.abc.us-east-1.rds.amazonaws.com"),
					Port:    aws.Int32(3306),
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	databases, err := adapter.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases() error = %v, want nil", err)
	}
	database := databases[0]
	if database.Engine != "mysql" {
		t.Fatalf("databases[0].Engine = %q, want mysql", database.Engine)
	}
	if database.EndpointAddress != "orders-db.abc.us-east-1.rds.amazonaws.com" {
		t.Fatalf("databases[0].EndpointAddress = %q", database.EndpointAddress)
	}
	if got, want := aws.ToInt32(database.EndpointPort), int32(3306); got != want {
		t.Fatalf("databases[0].EndpointPort = %d, want %d", got, want)
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
		ServiceKind:         awscloud.ServiceLightsail,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:lightsail:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeLightsailAPI struct {
	instancePages     []*awslightsail.GetInstancesOutput
	instanceCalls     int
	databasePages     []*awslightsail.GetRelationalDatabasesOutput
	databaseCalls     int
	loadBalancerPages []*awslightsail.GetLoadBalancersOutput
	loadBalancerCalls int
	diskPages         []*awslightsail.GetDisksOutput
	diskCalls         int
	staticIPPages     []*awslightsail.GetStaticIpsOutput
	staticIPCalls     int
}

func (f *fakeLightsailAPI) GetInstances(
	_ context.Context,
	_ *awslightsail.GetInstancesInput,
	_ ...func(*awslightsail.Options),
) (*awslightsail.GetInstancesOutput, error) {
	if f.instanceCalls >= len(f.instancePages) {
		return &awslightsail.GetInstancesOutput{}, nil
	}
	page := f.instancePages[f.instanceCalls]
	f.instanceCalls++
	return page, nil
}

func (f *fakeLightsailAPI) GetRelationalDatabases(
	_ context.Context,
	_ *awslightsail.GetRelationalDatabasesInput,
	_ ...func(*awslightsail.Options),
) (*awslightsail.GetRelationalDatabasesOutput, error) {
	if f.databaseCalls >= len(f.databasePages) {
		return &awslightsail.GetRelationalDatabasesOutput{}, nil
	}
	page := f.databasePages[f.databaseCalls]
	f.databaseCalls++
	return page, nil
}

func (f *fakeLightsailAPI) GetLoadBalancers(
	_ context.Context,
	_ *awslightsail.GetLoadBalancersInput,
	_ ...func(*awslightsail.Options),
) (*awslightsail.GetLoadBalancersOutput, error) {
	if f.loadBalancerCalls >= len(f.loadBalancerPages) {
		return &awslightsail.GetLoadBalancersOutput{}, nil
	}
	page := f.loadBalancerPages[f.loadBalancerCalls]
	f.loadBalancerCalls++
	return page, nil
}

func (f *fakeLightsailAPI) GetDisks(
	_ context.Context,
	_ *awslightsail.GetDisksInput,
	_ ...func(*awslightsail.Options),
) (*awslightsail.GetDisksOutput, error) {
	if f.diskCalls >= len(f.diskPages) {
		return &awslightsail.GetDisksOutput{}, nil
	}
	page := f.diskPages[f.diskCalls]
	f.diskCalls++
	return page, nil
}

func (f *fakeLightsailAPI) GetStaticIps(
	_ context.Context,
	_ *awslightsail.GetStaticIpsInput,
	_ ...func(*awslightsail.Options),
) (*awslightsail.GetStaticIpsOutput, error) {
	if f.staticIPCalls >= len(f.staticIPPages) {
		return &awslightsail.GetStaticIpsOutput{}, nil
	}
	page := f.staticIPPages[f.staticIPCalls]
	f.staticIPCalls++
	return page, nil
}

var _ apiClient = (*fakeLightsailAPI)(nil)
