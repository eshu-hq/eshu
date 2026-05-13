package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
)

func mapLoadBalancer(
	loadBalancer awselbv2types.LoadBalancer,
	tags map[string]string,
) elbv2service.LoadBalancer {
	value := elbv2service.LoadBalancer{
		ARN:                   aws.ToString(loadBalancer.LoadBalancerArn),
		Name:                  aws.ToString(loadBalancer.LoadBalancerName),
		DNSName:               aws.ToString(loadBalancer.DNSName),
		CanonicalHostedZoneID: aws.ToString(loadBalancer.CanonicalHostedZoneId),
		Scheme:                string(loadBalancer.Scheme),
		Type:                  string(loadBalancer.Type),
		VPCID:                 aws.ToString(loadBalancer.VpcId),
		IPAddressType:         string(loadBalancer.IpAddressType),
		CreatedAt:             aws.ToTime(loadBalancer.CreatedTime),
		AvailabilityZones:     mapAvailabilityZones(loadBalancer.AvailabilityZones),
		SecurityGroups:        cloneStrings(loadBalancer.SecurityGroups),
		Tags:                  tags,
	}
	if loadBalancer.State != nil {
		value.State = string(loadBalancer.State.Code)
	}
	return value
}

func mapListener(listener awselbv2types.Listener, tags map[string]string) elbv2service.Listener {
	return elbv2service.Listener{
		ARN:             aws.ToString(listener.ListenerArn),
		LoadBalancerARN: aws.ToString(listener.LoadBalancerArn),
		Protocol:        string(listener.Protocol),
		Port:            aws.ToInt32(listener.Port),
		SSLPolicy:       aws.ToString(listener.SslPolicy),
		Certificates:    mapCertificates(listener.Certificates),
		ALPNPolicy:      cloneStrings(listener.AlpnPolicy),
		DefaultActions:  mapActions(listener.DefaultActions),
		Tags:            tags,
	}
}

func mapRule(
	listenerARN string,
	rule awselbv2types.Rule,
	tags map[string]string,
) elbv2service.Rule {
	return elbv2service.Rule{
		ARN:         aws.ToString(rule.RuleArn),
		ListenerARN: listenerARN,
		Priority:    aws.ToString(rule.Priority),
		IsDefault:   aws.ToBool(rule.IsDefault),
		Conditions:  mapConditions(rule.Conditions),
		Actions:     mapActions(rule.Actions),
		Tags:        tags,
	}
}

func mapTargetGroup(
	targetGroup awselbv2types.TargetGroup,
	tags map[string]string,
) elbv2service.TargetGroup {
	return elbv2service.TargetGroup{
		ARN:              aws.ToString(targetGroup.TargetGroupArn),
		Name:             aws.ToString(targetGroup.TargetGroupName),
		Protocol:         string(targetGroup.Protocol),
		ProtocolVersion:  aws.ToString(targetGroup.ProtocolVersion),
		Port:             aws.ToInt32(targetGroup.Port),
		TargetType:       string(targetGroup.TargetType),
		VPCID:            aws.ToString(targetGroup.VpcId),
		IPAddressType:    string(targetGroup.IpAddressType),
		LoadBalancerARNs: cloneStrings(targetGroup.LoadBalancerArns),
		HealthCheck:      mapHealthCheck(targetGroup),
		Tags:             tags,
	}
}

func mapAvailabilityZones(zones []awselbv2types.AvailabilityZone) []elbv2service.AvailabilityZone {
	if len(zones) == 0 {
		return nil
	}
	output := make([]elbv2service.AvailabilityZone, 0, len(zones))
	for _, zone := range zones {
		output = append(output, elbv2service.AvailabilityZone{
			Name:     aws.ToString(zone.ZoneName),
			SubnetID: aws.ToString(zone.SubnetId),
		})
	}
	return output
}

func mapCertificates(certificates []awselbv2types.Certificate) []string {
	if len(certificates) == 0 {
		return nil
	}
	output := make([]string, 0, len(certificates))
	for _, certificate := range certificates {
		if arn := strings.TrimSpace(aws.ToString(certificate.CertificateArn)); arn != "" {
			output = append(output, arn)
		}
	}
	return output
}

func mapActions(actions []awselbv2types.Action) []elbv2service.Action {
	if len(actions) == 0 {
		return nil
	}
	output := make([]elbv2service.Action, 0, len(actions))
	for _, action := range actions {
		output = append(output, elbv2service.Action{
			Type:                string(action.Type),
			Order:               aws.ToInt32(action.Order),
			TargetGroupARN:      aws.ToString(action.TargetGroupArn),
			ForwardTargetGroups: mapForwardTargetGroups(action.ForwardConfig),
			Redirect:            mapRedirectAction(action.RedirectConfig),
			FixedResponse:       mapFixedResponseAction(action.FixedResponseConfig),
		})
	}
	return output
}

func mapForwardTargetGroups(config *awselbv2types.ForwardActionConfig) []elbv2service.WeightedTargetGroup {
	if config == nil || len(config.TargetGroups) == 0 {
		return nil
	}
	output := make([]elbv2service.WeightedTargetGroup, 0, len(config.TargetGroups))
	for _, targetGroup := range config.TargetGroups {
		output = append(output, elbv2service.WeightedTargetGroup{
			ARN:    aws.ToString(targetGroup.TargetGroupArn),
			Weight: aws.ToInt32(targetGroup.Weight),
		})
	}
	return output
}

func mapRedirectAction(config *awselbv2types.RedirectActionConfig) *elbv2service.RedirectAction {
	if config == nil {
		return nil
	}
	return &elbv2service.RedirectAction{
		StatusCode: string(config.StatusCode),
		Host:       aws.ToString(config.Host),
		Path:       aws.ToString(config.Path),
		Port:       aws.ToString(config.Port),
		Protocol:   aws.ToString(config.Protocol),
		Query:      aws.ToString(config.Query),
	}
}

func mapFixedResponseAction(
	config *awselbv2types.FixedResponseActionConfig,
) *elbv2service.FixedResponseAction {
	if config == nil {
		return nil
	}
	return &elbv2service.FixedResponseAction{
		StatusCode:  aws.ToString(config.StatusCode),
		ContentType: aws.ToString(config.ContentType),
		MessageBody: aws.ToString(config.MessageBody),
	}
}

func mapConditions(conditions []awselbv2types.RuleCondition) []elbv2service.Condition {
	if len(conditions) == 0 {
		return nil
	}
	output := make([]elbv2service.Condition, 0, len(conditions))
	for _, condition := range conditions {
		output = append(output, elbv2service.Condition{
			Field:              aws.ToString(condition.Field),
			Values:             cloneStrings(condition.Values),
			HostHeaderValues:   hostHeaderValues(condition),
			HTTPHeaderName:     httpHeaderName(condition),
			HTTPHeaderValues:   httpHeaderValues(condition),
			HTTPRequestMethods: httpRequestMethods(condition),
			PathPatternValues:  pathPatternValues(condition),
			QueryStrings:       queryStrings(condition),
			SourceIPValues:     sourceIPValues(condition),
		})
	}
	return output
}

func mapHealthCheck(targetGroup awselbv2types.TargetGroup) elbv2service.HealthCheck {
	matcher := ""
	if targetGroup.Matcher != nil {
		matcher = firstNonEmpty(aws.ToString(targetGroup.Matcher.HttpCode), aws.ToString(targetGroup.Matcher.GrpcCode))
	}
	return elbv2service.HealthCheck{
		Enabled:            aws.ToBool(targetGroup.HealthCheckEnabled),
		Protocol:           string(targetGroup.HealthCheckProtocol),
		Path:               aws.ToString(targetGroup.HealthCheckPath),
		Port:               aws.ToString(targetGroup.HealthCheckPort),
		IntervalSeconds:    aws.ToInt32(targetGroup.HealthCheckIntervalSeconds),
		TimeoutSeconds:     aws.ToInt32(targetGroup.HealthCheckTimeoutSeconds),
		HealthyThreshold:   aws.ToInt32(targetGroup.HealthyThresholdCount),
		UnhealthyThreshold: aws.ToInt32(targetGroup.UnhealthyThresholdCount),
		Matcher:            matcher,
	}
}

func mapTags(tags []awselbv2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	return output
}
