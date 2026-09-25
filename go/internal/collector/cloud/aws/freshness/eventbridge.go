// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
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
			if runtime.SupportsServiceKind(service) {
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
	case aws.ServiceIAM, aws.ServiceRoute53, aws.ServiceCloudFront:
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
	"AWS::ApiGateway::RestApi":                  aws.ServiceAPIGateway,
	"AWS::ApiGateway::Stage":                    aws.ServiceAPIGateway,
	"AWS::ApiGatewayV2::Api":                    aws.ServiceAPIGatewayV2,
	"AWS::ApiGatewayV2::Stage":                  aws.ServiceAPIGatewayV2,
	"AWS::CloudFront::Distribution":             aws.ServiceCloudFront,
	"AWS::DynamoDB::Table":                      aws.ServiceDynamoDB,
	"AWS::EC2::NetworkInterface":                aws.ServiceEC2,
	"AWS::EC2::SecurityGroup":                   aws.ServiceEC2,
	"AWS::EC2::Subnet":                          aws.ServiceEC2,
	"AWS::EC2::VPC":                             aws.ServiceEC2,
	"AWS::ECR::Repository":                      aws.ServiceECR,
	"AWS::ECS::Cluster":                         aws.ServiceECS,
	"AWS::ECS::Service":                         aws.ServiceECS,
	"AWS::ECS::TaskDefinition":                  aws.ServiceECS,
	"AWS::EKS::Cluster":                         aws.ServiceEKS,
	"AWS::ElasticLoadBalancingV2::Listener":     aws.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::LoadBalancer": aws.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::TargetGroup":  aws.ServiceELBv2,
	"AWS::Events::EventBus":                     aws.ServiceEventBridge,
	"AWS::Events::Rule":                         aws.ServiceEventBridge,
	"AWS::Glue::Connection":                     aws.ServiceGlue,
	"AWS::Glue::Crawler":                        aws.ServiceGlue,
	"AWS::Glue::Database":                       aws.ServiceGlue,
	"AWS::Glue::Job":                            aws.ServiceGlue,
	"AWS::Glue::Table":                          aws.ServiceGlue,
	"AWS::Glue::Trigger":                        aws.ServiceGlue,
	"AWS::Glue::Workflow":                       aws.ServiceGlue,
	"AWS::IAM::InstanceProfile":                 aws.ServiceIAM,
	"AWS::IAM::ManagedPolicy":                   aws.ServiceIAM,
	"AWS::IAM::Policy":                          aws.ServiceIAM,
	"AWS::IAM::Role":                            aws.ServiceIAM,
	"AWS::Lambda::Function":                     aws.ServiceLambda,
	"AWS::Logs::LogGroup":                       aws.ServiceCloudWatchLogs,
	"AWS::RDS::DBCluster":                       aws.ServiceRDS,
	"AWS::RDS::DBInstance":                      aws.ServiceRDS,
	"AWS::RDS::DBSubnetGroup":                   aws.ServiceRDS,
	"AWS::Redshift::Cluster":                    aws.ServiceRedshift,
	"AWS::Redshift::ClusterParameterGroup":      aws.ServiceRedshift,
	"AWS::Redshift::ClusterSnapshot":            aws.ServiceRedshift,
	"AWS::Redshift::ClusterSubnetGroup":         aws.ServiceRedshift,
	"AWS::RedshiftServerless::Namespace":        aws.ServiceRedshift,
	"AWS::RedshiftServerless::Workgroup":        aws.ServiceRedshift,
	"AWS::Route53::HostedZone":                  aws.ServiceRoute53,
	"AWS::S3::Bucket":                           aws.ServiceS3,
	"AWS::SecretsManager::Secret":               aws.ServiceSecretsManager,
	"AWS::SecurityHub::Hub":                     aws.ServiceSecurityHub,
	"AWS::SNS::Topic":                           aws.ServiceSNS,
	"AWS::SQS::Queue":                           aws.ServiceSQS,
	"AWS::SSM::Parameter":                       aws.ServiceSSM,
}

var cloudTrailSourceServices = map[string]string{
	"apigateway.amazonaws.com":           aws.ServiceAPIGateway,
	"apigatewayv2.amazonaws.com":         aws.ServiceAPIGatewayV2,
	"cloudfront.amazonaws.com":           aws.ServiceCloudFront,
	"dynamodb.amazonaws.com":             aws.ServiceDynamoDB,
	"ec2.amazonaws.com":                  aws.ServiceEC2,
	"ecr.amazonaws.com":                  aws.ServiceECR,
	"ecs.amazonaws.com":                  aws.ServiceECS,
	"eks.amazonaws.com":                  aws.ServiceEKS,
	"elasticloadbalancing.amazonaws.com": aws.ServiceELBv2,
	"events.amazonaws.com":               aws.ServiceEventBridge,
	"glue.amazonaws.com":                 aws.ServiceGlue,
	"iam.amazonaws.com":                  aws.ServiceIAM,
	"lambda.amazonaws.com":               aws.ServiceLambda,
	"logs.amazonaws.com":                 aws.ServiceCloudWatchLogs,
	"rds.amazonaws.com":                  aws.ServiceRDS,
	"redshift.amazonaws.com":             aws.ServiceRedshift,
	"redshift-serverless.amazonaws.com":  aws.ServiceRedshift,
	"route53.amazonaws.com":              aws.ServiceRoute53,
	"s3.amazonaws.com":                   aws.ServiceS3,
	"secretsmanager.amazonaws.com":       aws.ServiceSecretsManager,
	"securityhub.amazonaws.com":          aws.ServiceSecurityHub,
	"sns.amazonaws.com":                  aws.ServiceSNS,
	"sqs.amazonaws.com":                  aws.ServiceSQS,
	"ssm.amazonaws.com":                  aws.ServiceSSM,
}

var cloudTrailResourceIDKeys = map[string][]string{
	aws.ServiceEC2:    {"groupId", "vpcId", "subnetId", "networkInterfaceId", "instanceId"},
	aws.ServiceECR:    {"repositoryName"},
	aws.ServiceECS:    {"service", "cluster", "taskDefinition", "task"},
	aws.ServiceEKS:    {"name", "clusterName", "nodegroupName"},
	aws.ServiceELBv2:  {"loadBalancerArn", "listenerArn", "targetGroupArn", "ruleArn"},
	aws.ServiceLambda: {"functionName", "uuid"},
	aws.ServiceRDS:    {"dBInstanceIdentifier", "dBClusterIdentifier", "dBSubnetGroupName"},
	aws.ServiceRedshift: {
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
	aws.ServiceS3:             {"bucketName"},
	aws.ServiceSecretsManager: {"secretId"},
	aws.ServiceSecurityHub:    {"hubArn", "standardsSubscriptionArn", "standardsControlArn", "insightArn", "actionTargetArn", "accountId"},
	aws.ServiceSSM:            {"name"},
	aws.ServiceGlue:           {"databaseName", "tableName", "crawlerName", "jobName", "triggerName", "workflowName", "connectionName"},
}

var genericCloudTrailResourceIDKeys = []string{"resourceId", "resourceName", "id", "name"}
