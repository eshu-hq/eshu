package ecs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsECSResourcesWithRedactedTaskDefinitionEnvironment(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	client := fakeClient{
		clusters: []Cluster{{
			ARN:    "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
			Name:   "prod",
			Status: "ACTIVE",
		}},
		services: map[string][]Service{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:               "arn:aws:ecs:us-east-1:123456789012:service/prod/api",
					Name:              "api",
					ClusterARN:        "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					Status:            "ACTIVE",
					TaskDefinitionARN: "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7",
					LoadBalancers: []LoadBalancer{{
						TargetGroupARN: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/abc123",
						ContainerName:  "api",
						ContainerPort:  8080,
					}},
				},
			},
		},
		taskDefinitions: map[string]TaskDefinition{
			"arn:aws:ecs:us-east-1:123456789012:task-definition/api:7": {
				ARN:       "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7",
				Family:    "api",
				Revision:  7,
				Status:    "ACTIVE",
				TaskRole:  "arn:aws:iam::123456789012:role/api-task",
				ExecRole:  "arn:aws:iam::123456789012:role/api-exec",
				Network:   "awsvpc",
				CPU:       "256",
				Memory:    "512",
				CreatedAt: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
				Containers: []Container{{
					Name:      "api",
					Image:     "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod",
					Essential: true,
					Environment: []EnvironmentVariable{{
						Name:  "DATABASE_URL",
						Value: "postgres://user:password@example.internal/app",
					}},
					Secrets: []SecretReference{{
						Name:      "API_TOKEN",
						ValueFrom: "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token",
					}},
				}},
			},
			"arn:aws:ecs:us-east-1:123456789012:task-definition/worker:3": {
				ARN:      "arn:aws:ecs:us-east-1:123456789012:task-definition/worker:3",
				Family:   "worker",
				Revision: 3,
				Status:   "ACTIVE",
				Containers: []Container{{
					Name:  "worker",
					Image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/worker:prod",
				}},
			},
		},
		taskDefinitionARNs: []string{
			"arn:aws:ecs:us-east-1:123456789012:task-definition/api:7",
			"arn:aws:ecs:us-east-1:123456789012:task-definition/worker:3",
		},
		tasks: map[string][]Task{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:               "arn:aws:ecs:us-east-1:123456789012:task/prod/task-1",
					ClusterARN:        "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					TaskDefinitionARN: "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7",
					LastStatus:        "RUNNING",
					DesiredStatus:     "RUNNING",
					LaunchType:        "FARGATE",
					NetworkInterfaces: []TaskNetworkInterface{{
						NetworkInterfaceID: "eni-123",
						SubnetID:           "subnet-123",
						PrivateIPv4Address: "10.0.1.10",
						MACAddress:         "02:00:00:00:00:01",
					}},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 5 {
		t.Fatalf("aws_resource count = %d, want 5", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 5 {
		t.Fatalf("aws_relationship count = %d, want 5", counts[facts.AWSRelationshipFactKind])
	}
	assertResourceType(t, envelopes, awscloud.ResourceTypeECSCluster)
	assertResourceType(t, envelopes, awscloud.ResourceTypeECSService)
	taskDefinition := assertResourceType(t, envelopes, awscloud.ResourceTypeECSTaskDefinition)
	task := assertResourceType(t, envelopes, awscloud.ResourceTypeECSTask)
	assertTaskDefinitionRedaction(t, taskDefinition)
	assertTaskNetworkInterfaces(t, task)
	assertRelationship(t, envelopes, awscloud.RelationshipECSServiceUsesTaskDefinition)
	assertRelationship(t, envelopes, awscloud.RelationshipECSTaskDefinitionUsesImage)
	assertRelationship(t, envelopes, awscloud.RelationshipECSServiceTargetsLoadBalancer)
	assertRelationship(t, envelopes, awscloud.RelationshipECSTaskUsesNetworkInterface)
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err = Scanner{Client: fakeClient{}, RedactionKey: key}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerAggregatesContainerNamesForSharedImageRelationship(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	image := "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"
	taskDefinitionARN := "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7"
	client := fakeClient{
		taskDefinitions: map[string]TaskDefinition{
			taskDefinitionARN: {
				ARN:      taskDefinitionARN,
				Family:   "api",
				Revision: 7,
				Containers: []Container{
					{Name: "api", Image: image},
					{Name: "sidecar", Image: image},
				},
			},
		},
		taskDefinitionARNs: []string{taskDefinitionARN},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	relationships := relationshipsByType(envelopes, awscloud.RelationshipECSTaskDefinitionUsesImage)
	if len(relationships) != 1 {
		t.Fatalf("image relationship count = %d, want 1: %#v", len(relationships), relationships)
	}
	attributes, ok := relationships[0].Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", relationships[0].Payload["attributes"])
	}
	names, ok := attributes["container_names"].([]string)
	if !ok || strings.Join(names, ",") != "api,sidecar" {
		t.Fatalf("container_names = %#v, want api and sidecar", attributes["container_names"])
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceECS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ecs:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters           []Cluster
	services           map[string][]Service
	taskDefinitions    map[string]TaskDefinition
	taskDefinitionARNs []string
	tasks              map[string][]Task
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListServices(_ context.Context, cluster Cluster) ([]Service, error) {
	return c.services[cluster.ARN], nil
}

func (c fakeClient) DescribeTaskDefinition(_ context.Context, arn string) (*TaskDefinition, error) {
	taskDefinition, ok := c.taskDefinitions[arn]
	if !ok {
		return nil, nil
	}
	return &taskDefinition, nil
}

func (c fakeClient) ListTaskDefinitions(context.Context) ([]string, error) {
	return c.taskDefinitionARNs, nil
}

func (c fakeClient) ListTasks(_ context.Context, cluster Cluster) ([]Task, error) {
	return c.tasks[cluster.ARN], nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if len(relationshipsByType(envelopes, relationshipType)) > 0 {
		return
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var relationships []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			relationships = append(relationships, envelope)
		}
	}
	return relationships
}

func assertTaskDefinitionRedaction(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	containers, ok := attributes["containers"].([]map[string]any)
	if !ok || len(containers) != 1 {
		t.Fatalf("containers = %#v, want one container map", attributes["containers"])
	}
	environment, ok := containers[0]["environment"].([]map[string]any)
	if !ok || len(environment) != 1 {
		t.Fatalf("environment = %#v, want one env map", containers[0]["environment"])
	}
	if got := environment[0]["value"]; strings.Contains(fmt.Sprint(got), "password") {
		t.Fatalf("environment value leaked secret: %#v", got)
	}
	value, ok := environment[0]["value"].(map[string]any)
	if !ok {
		t.Fatalf("environment value = %#v, want redaction marker map", environment[0]["value"])
	}
	marker, ok := value["marker"].(string)
	if !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("environment marker = %#v, want HMAC redaction marker", value["marker"])
	}
	if got := value["ruleset_version"]; got != awscloud.RedactionPolicyVersion {
		t.Fatalf("environment ruleset_version = %#v, want %q", got, awscloud.RedactionPolicyVersion)
	}
	if got := value["reason"]; got != redact.ReasonKnownSensitiveKey {
		t.Fatalf("environment reason = %#v, want %q", got, redact.ReasonKnownSensitiveKey)
	}
	secrets, ok := containers[0]["secrets"].([]map[string]string)
	if !ok || len(secrets) != 1 {
		t.Fatalf("secrets = %#v, want one secret reference", containers[0]["secrets"])
	}
	if got := secrets[0]["value_from"]; got != "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token" {
		t.Fatalf("secret value_from = %q, want ARN preserved", got)
	}
}

func assertTaskNetworkInterfaces(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	networkInterfaces, ok := attributes["network_interfaces"].([]map[string]any)
	if !ok || len(networkInterfaces) != 1 {
		t.Fatalf("network_interfaces = %#v, want one ENI", attributes["network_interfaces"])
	}
	if got, _ := networkInterfaces[0]["network_interface_id"].(string); got != "eni-123" {
		t.Fatalf("network_interface_id = %q, want eni-123", got)
	}
	if got, _ := networkInterfaces[0]["subnet_id"].(string); got != "subnet-123" {
		t.Fatalf("subnet_id = %q, want subnet-123", got)
	}
}
