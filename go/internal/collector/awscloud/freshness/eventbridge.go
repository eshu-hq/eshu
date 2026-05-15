package freshness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
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
	case awscloud.ServiceIAM, awscloud.ServiceRoute53, awscloud.ServiceCloudFront:
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
	"AWS::ApiGateway::RestApi":                  awscloud.ServiceAPIGateway,
	"AWS::ApiGateway::Stage":                    awscloud.ServiceAPIGateway,
	"AWS::ApiGatewayV2::Api":                    awscloud.ServiceAPIGateway,
	"AWS::ApiGatewayV2::Stage":                  awscloud.ServiceAPIGateway,
	"AWS::CloudFront::Distribution":             awscloud.ServiceCloudFront,
	"AWS::DynamoDB::Table":                      awscloud.ServiceDynamoDB,
	"AWS::EC2::NetworkInterface":                awscloud.ServiceEC2,
	"AWS::EC2::SecurityGroup":                   awscloud.ServiceEC2,
	"AWS::EC2::Subnet":                          awscloud.ServiceEC2,
	"AWS::EC2::VPC":                             awscloud.ServiceEC2,
	"AWS::ECR::Repository":                      awscloud.ServiceECR,
	"AWS::ECS::Cluster":                         awscloud.ServiceECS,
	"AWS::ECS::Service":                         awscloud.ServiceECS,
	"AWS::ECS::TaskDefinition":                  awscloud.ServiceECS,
	"AWS::EKS::Cluster":                         awscloud.ServiceEKS,
	"AWS::ElasticLoadBalancingV2::Listener":     awscloud.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::LoadBalancer": awscloud.ServiceELBv2,
	"AWS::ElasticLoadBalancingV2::TargetGroup":  awscloud.ServiceELBv2,
	"AWS::Events::EventBus":                     awscloud.ServiceEventBridge,
	"AWS::Events::Rule":                         awscloud.ServiceEventBridge,
	"AWS::IAM::InstanceProfile":                 awscloud.ServiceIAM,
	"AWS::IAM::ManagedPolicy":                   awscloud.ServiceIAM,
	"AWS::IAM::Policy":                          awscloud.ServiceIAM,
	"AWS::IAM::Role":                            awscloud.ServiceIAM,
	"AWS::Lambda::Function":                     awscloud.ServiceLambda,
	"AWS::Logs::LogGroup":                       awscloud.ServiceCloudWatchLogs,
	"AWS::RDS::DBCluster":                       awscloud.ServiceRDS,
	"AWS::RDS::DBInstance":                      awscloud.ServiceRDS,
	"AWS::RDS::DBSubnetGroup":                   awscloud.ServiceRDS,
	"AWS::Route53::HostedZone":                  awscloud.ServiceRoute53,
	"AWS::S3::Bucket":                           awscloud.ServiceS3,
	"AWS::SecretsManager::Secret":               awscloud.ServiceSecretsManager,
	"AWS::SNS::Topic":                           awscloud.ServiceSNS,
	"AWS::SQS::Queue":                           awscloud.ServiceSQS,
	"AWS::SSM::Parameter":                       awscloud.ServiceSSM,
}

var cloudTrailSourceServices = map[string]string{
	"apigateway.amazonaws.com":           awscloud.ServiceAPIGateway,
	"apigatewayv2.amazonaws.com":         awscloud.ServiceAPIGateway,
	"cloudfront.amazonaws.com":           awscloud.ServiceCloudFront,
	"dynamodb.amazonaws.com":             awscloud.ServiceDynamoDB,
	"ec2.amazonaws.com":                  awscloud.ServiceEC2,
	"ecr.amazonaws.com":                  awscloud.ServiceECR,
	"ecs.amazonaws.com":                  awscloud.ServiceECS,
	"eks.amazonaws.com":                  awscloud.ServiceEKS,
	"elasticloadbalancing.amazonaws.com": awscloud.ServiceELBv2,
	"events.amazonaws.com":               awscloud.ServiceEventBridge,
	"iam.amazonaws.com":                  awscloud.ServiceIAM,
	"lambda.amazonaws.com":               awscloud.ServiceLambda,
	"logs.amazonaws.com":                 awscloud.ServiceCloudWatchLogs,
	"rds.amazonaws.com":                  awscloud.ServiceRDS,
	"route53.amazonaws.com":              awscloud.ServiceRoute53,
	"s3.amazonaws.com":                   awscloud.ServiceS3,
	"secretsmanager.amazonaws.com":       awscloud.ServiceSecretsManager,
	"sns.amazonaws.com":                  awscloud.ServiceSNS,
	"sqs.amazonaws.com":                  awscloud.ServiceSQS,
	"ssm.amazonaws.com":                  awscloud.ServiceSSM,
}

var cloudTrailResourceIDKeys = map[string][]string{
	awscloud.ServiceEC2:            {"groupId", "vpcId", "subnetId", "networkInterfaceId", "instanceId"},
	awscloud.ServiceECR:            {"repositoryName"},
	awscloud.ServiceECS:            {"service", "cluster", "taskDefinition", "task"},
	awscloud.ServiceEKS:            {"name", "clusterName", "nodegroupName"},
	awscloud.ServiceELBv2:          {"loadBalancerArn", "listenerArn", "targetGroupArn", "ruleArn"},
	awscloud.ServiceLambda:         {"functionName", "uuid"},
	awscloud.ServiceRDS:            {"dBInstanceIdentifier", "dBClusterIdentifier", "dBSubnetGroupName"},
	awscloud.ServiceS3:             {"bucketName"},
	awscloud.ServiceSecretsManager: {"secretId"},
	awscloud.ServiceSSM:            {"name"},
}

var genericCloudTrailResourceIDKeys = []string{"resourceId", "resourceName", "id", "name"}
