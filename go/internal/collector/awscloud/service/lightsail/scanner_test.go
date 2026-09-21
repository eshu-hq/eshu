// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lightsail

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsLightsailMetadataResourcesAndRelationships(t *testing.T) {
	instanceName := "web-1"
	databaseName := "orders-db"
	loadBalancerName := "web-lb"
	diskName := "web-1-data"
	staticIPName := "web-1-ip"
	port := int32(3306)
	client := fakeClient{
		instances: []Instance{{
			ARN:              "arn:aws:lightsail:us-east-1:123456789012:Instance/abc",
			Name:             instanceName,
			BlueprintID:      "amazon_linux_2023",
			BlueprintName:    "Amazon Linux 2023",
			BundleID:         "micro_3_0",
			State:            "running",
			PublicIPAddress:  "203.0.113.10",
			PrivateIPAddress: "172.26.0.10",
			IPv6Addresses:    []string{"2600:1f18::1"},
			IsStaticIP:       true,
			AvailabilityZone: "us-east-1a",
			RegionName:       "us-east-1",
			SSHKeyName:       "default",
			CreatedAt:        time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			Tags:             map[string]string{"env": "prod"},
		}},
		databases: []Database{{
			ARN:                "arn:aws:lightsail:us-east-1:123456789012:RelationalDatabase/def",
			Name:               databaseName,
			Engine:             "mysql",
			EngineVersion:      "8.0.32",
			State:              "available",
			BlueprintID:        "mysql_8_0",
			BundleID:           "micro_1_0",
			MasterDatabaseName: "orders",
			MasterUsername:     "admin",
			EndpointAddress:    "orders-db.abc.us-east-1.rds.amazonaws.com",
			EndpointPort:       &port,
			PubliclyAccessible: false,
			BackupRetention:    true,
			AvailabilityZone:   "us-east-1a",
			RegionName:         "us-east-1",
			CreatedAt:          time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			Tags:               map[string]string{"env": "prod"},
		}},
		loadBalancers: []LoadBalancer{{
			ARN:              "arn:aws:lightsail:us-east-1:123456789012:LoadBalancer/ghi",
			Name:             loadBalancerName,
			State:            "active",
			DNSName:          "web-lb-123.us-east-1.elb.amazonaws.com",
			Protocol:         "HTTP_HTTPS",
			PublicPorts:      []int32{80, 443},
			IPAddressType:    "ipv4",
			HTTPSRedirection: true,
			AvailabilityZone: "us-east-1a",
			RegionName:       "us-east-1",
			CreatedAt:        time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			Attached:         []string{instanceName},
			Tags:             map[string]string{"env": "prod"},
		}},
		disks: []Disk{{
			ARN:          "arn:aws:lightsail:us-east-1:123456789012:Disk/jkl",
			Name:         diskName,
			State:        "in-use",
			Path:         "/dev/xvdf",
			SizeInGb:     int32Ptr(32),
			IsAttached:   true,
			IsSystemDisk: false,
			AttachedTo:   instanceName,
			RegionName:   "us-east-1",
			CreatedAt:    time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
		}},
		staticIPs: []StaticIP{{
			ARN:        "arn:aws:lightsail:us-east-1:123456789012:StaticIp/mno",
			Name:       staticIPName,
			IPAddress:  "203.0.113.10",
			IsAttached: true,
			AttachedTo: instanceName,
			RegionName: "us-east-1",
			CreatedAt:  time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	instance := resourceByType(t, envelopes, awscloud.ResourceTypeLightsailInstance)
	if got, want := instance.Payload["resource_id"], instanceName; got != want {
		t.Fatalf("instance resource_id = %#v, want %q", got, want)
	}
	if got, want := instance.Payload["arn"], "arn:aws:lightsail:us-east-1:123456789012:Instance/abc"; got != want {
		t.Fatalf("instance arn = %#v, want %q", got, want)
	}
	if got, want := instance.Payload["state"], "running"; got != want {
		t.Fatalf("instance state = %#v, want %q", got, want)
	}
	instanceAttributes := attributesOf(t, instance)
	if got, want := instanceAttributes["bundle_id"], "micro_3_0"; got != want {
		t.Fatalf("instance bundle_id = %#v, want %q", got, want)
	}
	if got, want := instanceAttributes["public_ip_address"], "203.0.113.10"; got != want {
		t.Fatalf("instance public_ip_address = %#v, want %q", got, want)
	}

	database := resourceByType(t, envelopes, awscloud.ResourceTypeLightsailDatabase)
	if got, want := database.Payload["resource_id"], databaseName; got != want {
		t.Fatalf("database resource_id = %#v, want %q", got, want)
	}
	databaseAttributes := attributesOf(t, database)
	if got, want := databaseAttributes["engine"], "mysql"; got != want {
		t.Fatalf("database engine = %#v, want %q", got, want)
	}

	loadBalancer := resourceByType(t, envelopes, awscloud.ResourceTypeLightsailLoadBalancer)
	if got, want := loadBalancer.Payload["resource_id"], loadBalancerName; got != want {
		t.Fatalf("load_balancer resource_id = %#v, want %q", got, want)
	}

	disk := resourceByType(t, envelopes, awscloud.ResourceTypeLightsailDisk)
	if got, want := disk.Payload["resource_id"], diskName; got != want {
		t.Fatalf("disk resource_id = %#v, want %q", got, want)
	}

	staticIP := resourceByType(t, envelopes, awscloud.ResourceTypeLightsailStaticIP)
	if got, want := staticIP.Payload["resource_id"], staticIPName; got != want {
		t.Fatalf("static_ip resource_id = %#v, want %q", got, want)
	}

	// load-balancer -> instance: source must equal the load balancer node
	// resource_id; target must equal the instance node resource_id.
	lbInstance := relationshipByType(t, envelopes, awscloud.RelationshipLightsailLoadBalancerTargetsInstance)
	if got, want := lbInstance.Payload["source_resource_id"], loadBalancerName; got != want {
		t.Fatalf("lb->instance source_resource_id = %#v, want %q (load balancer node resource_id)", got, want)
	}
	if got, want := lbInstance.Payload["target_resource_id"], instanceName; got != want {
		t.Fatalf("lb->instance target_resource_id = %#v, want %q (instance node resource_id)", got, want)
	}
	if got, want := lbInstance.Payload["target_type"], awscloud.ResourceTypeLightsailInstance; got != want {
		t.Fatalf("lb->instance target_type = %#v, want %q", got, want)
	}
	if got := lbInstance.Payload["target_arn"]; got != "" {
		t.Fatalf("lb->instance target_arn = %#v, want empty (bare-name keyed edge)", got)
	}

	// instance -> disk: source must equal the instance node resource_id; target
	// must equal the disk node resource_id.
	instanceDisk := relationshipByType(t, envelopes, awscloud.RelationshipLightsailInstanceAttachedToDisk)
	if got, want := instanceDisk.Payload["source_resource_id"], instanceName; got != want {
		t.Fatalf("instance->disk source_resource_id = %#v, want %q (instance node resource_id)", got, want)
	}
	if got, want := instanceDisk.Payload["target_resource_id"], diskName; got != want {
		t.Fatalf("instance->disk target_resource_id = %#v, want %q (disk node resource_id)", got, want)
	}
	if got, want := instanceDisk.Payload["target_type"], awscloud.ResourceTypeLightsailDisk; got != want {
		t.Fatalf("instance->disk target_type = %#v, want %q", got, want)
	}

	// instance -> static IP: source must equal the instance node resource_id;
	// target must equal the static IP node resource_id.
	instanceStaticIP := relationshipByType(t, envelopes, awscloud.RelationshipLightsailInstanceAttachedToStaticIP)
	if got, want := instanceStaticIP.Payload["source_resource_id"], instanceName; got != want {
		t.Fatalf("instance->static_ip source_resource_id = %#v, want %q (instance node resource_id)", got, want)
	}
	if got, want := instanceStaticIP.Payload["target_resource_id"], staticIPName; got != want {
		t.Fatalf("instance->static_ip target_resource_id = %#v, want %q (static IP node resource_id)", got, want)
	}
	if got, want := instanceStaticIP.Payload["target_type"], awscloud.ResourceTypeLightsailStaticIP; got != want {
		t.Fatalf("instance->static_ip target_type = %#v, want %q", got, want)
	}

	// Every emitted edge must satisfy the runtime graph-join contract.
	relguard.AssertObservations(
		t,
		loadBalancerInstanceRelationships(testBoundary(), client.loadBalancers[0])[0],
		*instanceDiskRelationship(testBoundary(), client.disks[0]),
		*instanceStaticIPRelationship(testBoundary(), client.staticIPs[0]),
	)
}

func TestScannerKeepsEdgeKeysConsistentWithNodeResourceIDs(t *testing.T) {
	instanceName := "app-1"
	diskName := "app-1-disk"
	staticIPName := "app-1-ip"
	loadBalancerName := "app-lb"
	client := fakeClient{
		instances:     []Instance{{ARN: "arn:aws:lightsail:us-east-1:123456789012:Instance/i", Name: instanceName, State: "running"}},
		disks:         []Disk{{ARN: "arn:aws:lightsail:us-east-1:123456789012:Disk/d", Name: diskName, AttachedTo: instanceName}},
		staticIPs:     []StaticIP{{ARN: "arn:aws:lightsail:us-east-1:123456789012:StaticIp/s", Name: staticIPName, AttachedTo: instanceName}},
		loadBalancers: []LoadBalancer{{ARN: "arn:aws:lightsail:us-east-1:123456789012:LoadBalancer/l", Name: loadBalancerName, Attached: []string{instanceName}}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	nodeIDs := resourceIDsByType(envelopes)
	edges := []struct {
		relationship string
		wantSource   string
		wantTarget   string
		sourceType   string
		targetType   string
	}{
		{awscloud.RelationshipLightsailLoadBalancerTargetsInstance, loadBalancerName, instanceName, awscloud.ResourceTypeLightsailLoadBalancer, awscloud.ResourceTypeLightsailInstance},
		{awscloud.RelationshipLightsailInstanceAttachedToDisk, instanceName, diskName, awscloud.ResourceTypeLightsailInstance, awscloud.ResourceTypeLightsailDisk},
		{awscloud.RelationshipLightsailInstanceAttachedToStaticIP, instanceName, staticIPName, awscloud.ResourceTypeLightsailInstance, awscloud.ResourceTypeLightsailStaticIP},
	}
	for _, edge := range edges {
		rel := relationshipByType(t, envelopes, edge.relationship)
		source, _ := rel.Payload["source_resource_id"].(string)
		target, _ := rel.Payload["target_resource_id"].(string)
		if source != edge.wantSource {
			t.Fatalf("%s source_resource_id = %q, want %q", edge.relationship, source, edge.wantSource)
		}
		if target != edge.wantTarget {
			t.Fatalf("%s target_resource_id = %q, want %q", edge.relationship, target, edge.wantTarget)
		}
		if _, ok := nodeIDs[edge.sourceType][source]; !ok {
			t.Fatalf("%s source %q does not match any %s node resource_id; the edge would dangle", edge.relationship, source, edge.sourceType)
		}
		if _, ok := nodeIDs[edge.targetType][target]; !ok {
			t.Fatalf("%s target %q does not match any %s node resource_id; the edge would dangle", edge.relationship, target, edge.targetType)
		}
	}
}

func TestScannerOmitsRelationshipsWhenNotAttached(t *testing.T) {
	client := fakeClient{
		disks:         []Disk{{ARN: "arn:aws:lightsail:us-east-1:123456789012:Disk/d", Name: "free-disk", AttachedTo: ""}},
		staticIPs:     []StaticIP{{ARN: "arn:aws:lightsail:us-east-1:123456789012:StaticIp/s", Name: "free-ip", AttachedTo: ""}},
		loadBalancers: []LoadBalancer{{ARN: "arn:aws:lightsail:us-east-1:123456789012:LoadBalancer/l", Name: "empty-lb"}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationship := range []string{
		awscloud.RelationshipLightsailInstanceAttachedToDisk,
		awscloud.RelationshipLightsailInstanceAttachedToStaticIP,
		awscloud.RelationshipLightsailLoadBalancerTargetsInstance,
	} {
		if got := countRelationships(envelopes, relationship); got != 0 {
			t.Fatalf("%s relationship count = %d, want 0 for unattached resources", relationship, got)
		}
	}
}

func TestScannerEmitsOneLoadBalancerEdgePerDistinctInstance(t *testing.T) {
	client := fakeClient{loadBalancers: []LoadBalancer{{
		Name:     "lb",
		Attached: []string{"web-1", "web-2", "web-1"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipLightsailLoadBalancerTargetsInstance); got != 2 {
		t.Fatalf("lb->instance relationship count = %d, want 2 (duplicate instance collapses)", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func int32Ptr(value int32) *int32 { return &value }

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

type fakeClient struct {
	instances     []Instance
	databases     []Database
	loadBalancers []LoadBalancer
	disks         []Disk
	staticIPs     []StaticIP
}

func (c fakeClient) ListInstances(context.Context) ([]Instance, error) { return c.instances, nil }
func (c fakeClient) ListDatabases(context.Context) ([]Database, error) { return c.databases, nil }
func (c fakeClient) ListLoadBalancers(context.Context) ([]LoadBalancer, error) {
	return c.loadBalancers, nil
}
func (c fakeClient) ListDisks(context.Context) ([]Disk, error)         { return c.disks, nil }
func (c fakeClient) ListStaticIPs(context.Context) ([]StaticIP, error) { return c.staticIPs, nil }

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func resourceIDsByType(envelopes []facts.Envelope) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resourceType, _ := envelope.Payload["resource_type"].(string)
		resourceID, _ := envelope.Payload["resource_id"].(string)
		if resourceType == "" || resourceID == "" {
			continue
		}
		if out[resourceType] == nil {
			out[resourceType] = make(map[string]struct{})
		}
		out[resourceType][resourceID] = struct{}{}
	}
	return out
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
