package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awselbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
)

func hostHeaderValues(condition awselbv2types.RuleCondition) []string {
	if condition.HostHeaderConfig == nil {
		return nil
	}
	return cloneStrings(condition.HostHeaderConfig.Values)
}

func httpHeaderName(condition awselbv2types.RuleCondition) string {
	if condition.HttpHeaderConfig == nil {
		return ""
	}
	return aws.ToString(condition.HttpHeaderConfig.HttpHeaderName)
}

func httpHeaderValues(condition awselbv2types.RuleCondition) []string {
	if condition.HttpHeaderConfig == nil {
		return nil
	}
	return cloneStrings(condition.HttpHeaderConfig.Values)
}

func httpRequestMethods(condition awselbv2types.RuleCondition) []string {
	if condition.HttpRequestMethodConfig == nil {
		return nil
	}
	return cloneStrings(condition.HttpRequestMethodConfig.Values)
}

func pathPatternValues(condition awselbv2types.RuleCondition) []string {
	if condition.PathPatternConfig == nil {
		return nil
	}
	return cloneStrings(condition.PathPatternConfig.Values)
}

func queryStrings(condition awselbv2types.RuleCondition) []elbv2service.QueryStringCondition {
	if condition.QueryStringConfig == nil || len(condition.QueryStringConfig.Values) == 0 {
		return nil
	}
	output := make([]elbv2service.QueryStringCondition, 0, len(condition.QueryStringConfig.Values))
	for _, value := range condition.QueryStringConfig.Values {
		output = append(output, elbv2service.QueryStringCondition{
			Key:   aws.ToString(value.Key),
			Value: aws.ToString(value.Value),
		})
	}
	return output
}

func sourceIPValues(condition awselbv2types.RuleCondition) []string {
	if condition.SourceIpConfig == nil {
		return nil
	}
	return cloneStrings(condition.SourceIpConfig.Values)
}
