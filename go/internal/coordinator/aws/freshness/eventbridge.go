// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

const (
	eventBridgeDetailTypeConfigChange  = "Config Configuration Item Change"
	eventBridgeDetailTypeCloudTrailAPI = "AWS API Call via CloudTrail"
	awsGlobalRegion                    = "aws-global"
)

type eventBridgeEnvelope struct {
	ID         string          `json:"id"`
	Source     string          `json:"source"`
	DetailType string          `json:"detail-type"`
	Account    string          `json:"account"`
	Region     string          `json:"region"`
	Time       time.Time       `json:"time"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

type configChangeDetail struct {
	ConfigurationItem configItem `json:"configurationItem"`
}

type configItem struct {
	AWSAccountID                 string    `json:"awsAccountId"`
	AWSRegion                    string    `json:"awsRegion"`
	ResourceType                 string    `json:"resourceType"`
	ResourceID                   string    `json:"resourceId"`
	ConfigurationItemCaptureTime time.Time `json:"configurationItemCaptureTime"`
}

type cloudTrailDetail struct {
	EventID           string         `json:"eventID"`
	EventSource       string         `json:"eventSource"`
	EventName         string         `json:"eventName"`
	RequestParameters map[string]any `json:"requestParameters"`
}

// NormalizeEventBridge maps one AWS EventBridge delivery into a bounded AWS
// freshness trigger. It performs no AWS API calls and treats the event as a
// wake-up signal only.
func NormalizeEventBridge(payload []byte) (Trigger, error) {
	var event eventBridgeEnvelope
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, fmt.Errorf("decode AWS EventBridge event: %w", err)
	}
	switch strings.TrimSpace(event.DetailType) {
	case eventBridgeDetailTypeConfigChange:
		return normalizeConfigChangeEvent(event)
	case eventBridgeDetailTypeCloudTrailAPI:
		return normalizeCloudTrailAPIEvent(event)
	default:
		return Trigger{}, fmt.Errorf("unsupported AWS EventBridge detail-type %q", event.DetailType)
	}
}

func normalizeConfigChangeEvent(event eventBridgeEnvelope) (Trigger, error) {
	var detail configChangeDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return Trigger{}, fmt.Errorf("decode AWS Config change detail: %w", err)
	}
	item := detail.ConfigurationItem
	serviceKind, err := serviceKindFromConfigResourceType(item.ResourceType)
	if err != nil {
		return Trigger{}, err
	}
	observedAt := firstTime(item.ConfigurationItemCaptureTime, event.Time)
	trigger := Trigger{
		EventID:      event.ID,
		Kind:         EventKindConfigChange,
		AccountID:    firstNonEmpty(item.AWSAccountID, event.Account),
		Region:       normalizeTriggerRegion(serviceKind, firstNonEmpty(item.AWSRegion, event.Region)),
		ServiceKind:  serviceKind,
		ResourceType: item.ResourceType,
		ResourceID:   item.ResourceID,
		ObservedAt:   observedAt,
	}
	if err := trigger.Validate(); err != nil {
		return Trigger{}, err
	}
	return trigger.normalized(), nil
}

func normalizeCloudTrailAPIEvent(event eventBridgeEnvelope) (Trigger, error) {
	var detail cloudTrailDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return Trigger{}, fmt.Errorf("decode AWS CloudTrail detail: %w", err)
	}
	serviceKind, err := serviceKindFromCloudTrail(event.Source, detail.EventSource)
	if err != nil {
		return Trigger{}, err
	}
	trigger := Trigger{
		EventID:      firstNonEmpty(event.ID, detail.EventID),
		Kind:         EventKindCloudTrailAPI,
		AccountID:    event.Account,
		Region:       normalizeTriggerRegion(serviceKind, event.Region),
		ServiceKind:  serviceKind,
		ResourceType: strings.TrimSpace(detail.EventSource + ":" + detail.EventName),
		ResourceID:   cloudTrailResourceID(serviceKind, detail.RequestParameters, event.Resources),
		ObservedAt:   event.Time,
	}
	if err := trigger.Validate(); err != nil {
		return Trigger{}, err
	}
	return trigger.normalized(), nil
}

func serviceKindFromConfigResourceType(resourceType string) (string, error) {
	cleaned := strings.TrimSpace(resourceType)
	if service, ok := configResourceTypeServices[cleaned]; ok {
		return service, nil
	}
	return "", fmt.Errorf("unsupported AWS Config resource type %q", resourceType)
}

func serviceKindFromCloudTrail(source string, eventSource string) (string, error) {
	candidates := []string{strings.TrimSpace(eventSource), strings.TrimSpace(source)}
	for _, candidate := range candidates {
		if service, ok := cloudTrailSourceServices[strings.ToLower(candidate)]; ok {
			return service, nil
		}
		if strings.HasPrefix(candidate, "aws.") {
			service := strings.TrimPrefix(candidate, "aws.")
			if awsruntime.SupportsServiceKind(service) {
				return service, nil
			}
		}
	}
	return "", fmt.Errorf("unsupported AWS CloudTrail event source %q", firstNonEmpty(eventSource, source))
}

func normalizeTriggerRegion(serviceKind string, region string) string {
	if isGlobalService(serviceKind) {
		return awsGlobalRegion
	}
	return strings.TrimSpace(region)
}

func isGlobalService(serviceKind string) bool {
	switch serviceKind {
	case catalog.ServiceIAM, catalog.ServiceRoute53, catalog.ServiceCloudFront:
		return true
	default:
		return false
	}
}

func cloudTrailResourceID(serviceKind string, params map[string]any, resources []string) string {
	for _, key := range cloudTrailResourceIDKeys[serviceKind] {
		if value := stringParameter(params, key); value != "" {
			return value
		}
	}
	for _, key := range genericCloudTrailResourceIDKeys {
		if value := stringParameter(params, key); value != "" {
			return value
		}
	}
	if len(resources) > 0 {
		return strings.TrimSpace(resources[0])
	}
	return ""
}

func stringParameter(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return stringParameter(typed, key)
	default:
		return ""
	}
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var configResourceTypeServices = map[string]string{
	"AWS::ApiGateway::RestApi":                  catalog.ServiceAPIGateway,
	"AWS::ApiGateway::Stage":                    catalog.ServiceAPIGateway,
	"AWS::ApiGatewayV2::Api":                    catalog.ServiceAPIGatewayV2,
	"AWS::ApiGatewayV2::Stage":                  catalog.ServiceAPIGatewayV2,
	"AWS::CloudFront::Distribution":             catalog.ServiceCloudFront,
	"AWS::DynamoDB::Table":                      catalog.ServiceDynamoDB,
	"AWS::EC2::NetworkInterface":                catalog.ServiceEC2,
	"AWS::EC2::SecurityGroup":                   catalog.ServiceEC2,
	"AWS::EC2::Subnet":                          catalog.ServiceEC2,
	"AWS::EC2::VPC":                             catalog.ServiceEC2,
	"AWS::ECR::Repository":                      catalog.ServiceECR,
	"AWS::ECS::Cluster":                         catalog.ServiceECS,
	"AWS::ECS::Service":                         catalog.ServiceECS,
	"AWS::ECS::TaskDefinition":                  catalog.ServiceECS,
	"AWS::EKS::Cluster":                         catalog.ServiceEKS,
	"AWS::ElasticLoadBalancingV2::Listener":     catalog.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::LoadBalancer": catalog.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::TargetGroup":  catalog.ServiceELBv2,
	"AWS::Events::EventBus":                     catalog.ServiceEventBridge,
	"AWS::Events::Rule":                         catalog.ServiceEventBridge,
	"AWS::Glue::Connection":                     catalog.ServiceGlue,
	"AWS::Glue::Crawler":                        catalog.ServiceGlue,
	"AWS::Glue::Database":                       catalog.ServiceGlue,
	"AWS::Glue::Job":                            catalog.ServiceGlue,
	"AWS::Glue::Table":                          catalog.ServiceGlue,
	"AWS::Glue::Trigger":                        catalog.ServiceGlue,
	"AWS::Glue::Workflow":                       catalog.ServiceGlue,
	"AWS::IAM::InstanceProfile":                 catalog.ServiceIAM,
	"AWS::IAM::ManagedPolicy":                   catalog.ServiceIAM,
	"AWS::IAM::Policy":                          catalog.ServiceIAM,
	"AWS::IAM::Role":                            catalog.ServiceIAM,
	"AWS::Lambda::Function":                     catalog.ServiceLambda,
	"AWS::Logs::LogGroup":                       catalog.ServiceCloudWatchLogs,
	"AWS::RDS::DBCluster":                       catalog.ServiceRDS,
	"AWS::RDS::DBInstance":                      catalog.ServiceRDS,
	"AWS::RDS::DBSubnetGroup":                   catalog.ServiceRDS,
	"AWS::Redshift::Cluster":                    catalog.ServiceRedshift,
	"AWS::Redshift::ClusterParameterGroup":      catalog.ServiceRedshift,
	"AWS::Redshift::ClusterSnapshot":            catalog.ServiceRedshift,
	"AWS::Redshift::ClusterSubnetGroup":         catalog.ServiceRedshift,
	"AWS::RedshiftServerless::Namespace":        catalog.ServiceRedshift,
	"AWS::RedshiftServerless::Workgroup":        catalog.ServiceRedshift,
	"AWS::Route53::HostedZone":                  catalog.ServiceRoute53,
	"AWS::S3::Bucket":                           catalog.ServiceS3,
	"AWS::SecretsManager::Secret":               catalog.ServiceSecretsManager,
	"AWS::SecurityHub::Hub":                     catalog.ServiceSecurityHub,
	"AWS::SNS::Topic":                           catalog.ServiceSNS,
	"AWS::SQS::Queue":                           catalog.ServiceSQS,
	"AWS::SSM::Parameter":                       catalog.ServiceSSM,
}

var cloudTrailSourceServices = map[string]string{
	"apigateway.amazonaws.com":           catalog.ServiceAPIGateway,
	"apigatewayv2.amazonaws.com":         catalog.ServiceAPIGatewayV2,
	"cloudfront.amazonaws.com":           catalog.ServiceCloudFront,
	"dynamodb.amazonaws.com":             catalog.ServiceDynamoDB,
	"ec2.amazonaws.com":                  catalog.ServiceEC2,
	"ecr.amazonaws.com":                  catalog.ServiceECR,
	"ecs.amazonaws.com":                  catalog.ServiceECS,
	"eks.amazonaws.com":                  catalog.ServiceEKS,
	"elasticloadbalancing.amazonaws.com": catalog.ServiceELBv2,
	"events.amazonaws.com":               catalog.ServiceEventBridge,
	"glue.amazonaws.com":                 catalog.ServiceGlue,
	"iam.amazonaws.com":                  catalog.ServiceIAM,
	"lambda.amazonaws.com":               catalog.ServiceLambda,
	"logs.amazonaws.com":                 catalog.ServiceCloudWatchLogs,
	"rds.amazonaws.com":                  catalog.ServiceRDS,
	"redshift.amazonaws.com":             catalog.ServiceRedshift,
	"redshift-serverless.amazonaws.com":  catalog.ServiceRedshift,
	"route53.amazonaws.com":              catalog.ServiceRoute53,
	"s3.amazonaws.com":                   catalog.ServiceS3,
	"secretsmanager.amazonaws.com":       catalog.ServiceSecretsManager,
	"securityhub.amazonaws.com":          catalog.ServiceSecurityHub,
	"sns.amazonaws.com":                  catalog.ServiceSNS,
	"sqs.amazonaws.com":                  catalog.ServiceSQS,
	"ssm.amazonaws.com":                  catalog.ServiceSSM,
}

var cloudTrailResourceIDKeys = map[string][]string{
	catalog.ServiceEC2:    {"groupId", "vpcId", "subnetId", "networkInterfaceId", "instanceId"},
	catalog.ServiceECR:    {"repositoryName"},
	catalog.ServiceECS:    {"service", "cluster", "taskDefinition", "task"},
	catalog.ServiceEKS:    {"name", "clusterName", "nodegroupName"},
	catalog.ServiceELBv2:  {"loadBalancerArn", "listenerArn", "targetGroupArn", "ruleArn"},
	catalog.ServiceLambda: {"functionName", "uuid"},
	catalog.ServiceRDS:    {"dBInstanceIdentifier", "dBClusterIdentifier", "dBSubnetGroupName"},
	catalog.ServiceRedshift: {
		"clusterIdentifier",
		"parameterGroupName",
		"clusterSubnetGroupName",
		"snapshotIdentifier",
		"namespaceName",
		"namespaceArn",
		"workgroupName",
		"workgroupArn",
		"scheduledActionName",
	},
	catalog.ServiceS3:             {"bucketName"},
	catalog.ServiceSecretsManager: {"secretId"},
	catalog.ServiceSecurityHub:    {"hubArn", "standardsSubscriptionArn", "standardsControlArn", "insightArn", "actionTargetArn", "accountId"},
	catalog.ServiceSSM:            {"name"},
	catalog.ServiceGlue:           {"databaseName", "tableName", "crawlerName", "jobName", "triggerName", "workflowName", "connectionName"},
}

var genericCloudTrailResourceIDKeys = []string{"resourceId", "resourceName", "id", "name"}
