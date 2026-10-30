// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
			if SupportsServiceKind(service) {
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
	return IsGlobalServiceKind(serviceKind)
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
	"AWS::ApiGateway::RestApi":                  serviceAPIGateway,
	"AWS::ApiGateway::Stage":                    serviceAPIGateway,
	"AWS::ApiGatewayV2::Api":                    serviceAPIGatewayV2,
	"AWS::ApiGatewayV2::Stage":                  serviceAPIGatewayV2,
	"AWS::CloudFront::Distribution":             serviceCloudFront,
	"AWS::DynamoDB::Table":                      serviceDynamoDB,
	"AWS::EC2::NetworkInterface":                serviceEC2,
	"AWS::EC2::SecurityGroup":                   serviceEC2,
	"AWS::EC2::Subnet":                          serviceEC2,
	"AWS::EC2::VPC":                             serviceEC2,
	"AWS::ECR::Repository":                      serviceECR,
	"AWS::ECS::Cluster":                         serviceECS,
	"AWS::ECS::Service":                         serviceECS,
	"AWS::ECS::TaskDefinition":                  serviceECS,
	"AWS::EKS::Cluster":                         serviceEKS,
	"AWS::ElasticLoadBalancingV2::Listener":     serviceELBv2,
	"AWS::ElasticLoadBalancingV2::LoadBalancer": serviceELBv2,
	"AWS::ElasticLoadBalancingV2::TargetGroup":  serviceELBv2,
	"AWS::Events::EventBus":                     serviceEventBridge,
	"AWS::Events::Rule":                         serviceEventBridge,
	"AWS::Glue::Connection":                     serviceGlue,
	"AWS::Glue::Crawler":                        serviceGlue,
	"AWS::Glue::Database":                       serviceGlue,
	"AWS::Glue::Job":                            serviceGlue,
	"AWS::Glue::Table":                          serviceGlue,
	"AWS::Glue::Trigger":                        serviceGlue,
	"AWS::Glue::Workflow":                       serviceGlue,
	"AWS::IAM::InstanceProfile":                 serviceIAM,
	"AWS::IAM::ManagedPolicy":                   serviceIAM,
	"AWS::IAM::Policy":                          serviceIAM,
	"AWS::IAM::Role":                            serviceIAM,
	"AWS::Lambda::Function":                     serviceLambda,
	"AWS::Logs::LogGroup":                       serviceCloudWatchLogs,
	"AWS::RDS::DBCluster":                       serviceRDS,
	"AWS::RDS::DBInstance":                      serviceRDS,
	"AWS::RDS::DBSubnetGroup":                   serviceRDS,
	"AWS::Redshift::Cluster":                    serviceRedshift,
	"AWS::Redshift::ClusterParameterGroup":      serviceRedshift,
	"AWS::Redshift::ClusterSnapshot":            serviceRedshift,
	"AWS::Redshift::ClusterSubnetGroup":         serviceRedshift,
	"AWS::RedshiftServerless::Namespace":        serviceRedshift,
	"AWS::RedshiftServerless::Workgroup":        serviceRedshift,
	"AWS::Route53::HostedZone":                  serviceRoute53,
	"AWS::S3::Bucket":                           serviceS3,
	"AWS::SecretsManager::Secret":               serviceSecretsManager,
	"AWS::SecurityHub::Hub":                     serviceSecurityHub,
	"AWS::SNS::Topic":                           serviceSNS,
	"AWS::SQS::Queue":                           serviceSQS,
	"AWS::SSM::Parameter":                       serviceSSM,
}

var cloudTrailSourceServices = map[string]string{
	"apigateway.amazonaws.com":           serviceAPIGateway,
	"apigatewayv2.amazonaws.com":         serviceAPIGatewayV2,
	"cloudfront.amazonaws.com":           serviceCloudFront,
	"dynamodb.amazonaws.com":             serviceDynamoDB,
	"ec2.amazonaws.com":                  serviceEC2,
	"ecr.amazonaws.com":                  serviceECR,
	"ecs.amazonaws.com":                  serviceECS,
	"eks.amazonaws.com":                  serviceEKS,
	"elasticloadbalancing.amazonaws.com": serviceELBv2,
	"events.amazonaws.com":               serviceEventBridge,
	"glue.amazonaws.com":                 serviceGlue,
	"iam.amazonaws.com":                  serviceIAM,
	"lambda.amazonaws.com":               serviceLambda,
	"logs.amazonaws.com":                 serviceCloudWatchLogs,
	"rds.amazonaws.com":                  serviceRDS,
	"redshift.amazonaws.com":             serviceRedshift,
	"redshift-serverless.amazonaws.com":  serviceRedshift,
	"route53.amazonaws.com":              serviceRoute53,
	"s3.amazonaws.com":                   serviceS3,
	"secretsmanager.amazonaws.com":       serviceSecretsManager,
	"securityhub.amazonaws.com":          serviceSecurityHub,
	"sns.amazonaws.com":                  serviceSNS,
	"sqs.amazonaws.com":                  serviceSQS,
	"ssm.amazonaws.com":                  serviceSSM,
}

var cloudTrailResourceIDKeys = map[string][]string{
	serviceEC2:    {"groupId", "vpcId", "subnetId", "networkInterfaceId", "instanceId"},
	serviceECR:    {"repositoryName"},
	serviceECS:    {"service", "cluster", "taskDefinition", "task"},
	serviceEKS:    {"name", "clusterName", "nodegroupName"},
	serviceELBv2:  {"loadBalancerArn", "listenerArn", "targetGroupArn", "ruleArn"},
	serviceLambda: {"functionName", "uuid"},
	serviceRDS:    {"dBInstanceIdentifier", "dBClusterIdentifier", "dBSubnetGroupName"},
	serviceRedshift: {
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
	serviceS3:             {"bucketName"},
	serviceSecretsManager: {"secretId"},
	serviceSecurityHub:    {"hubArn", "standardsSubscriptionArn", "standardsControlArn", "insightArn", "actionTargetArn", "accountId"},
	serviceSSM:            {"name"},
	serviceGlue:           {"databaseName", "tableName", "crawlerName", "jobName", "triggerName", "workflowName", "connectionName"},
}

var genericCloudTrailResourceIDKeys = []string{"resourceId", "resourceName", "id", "name"}
