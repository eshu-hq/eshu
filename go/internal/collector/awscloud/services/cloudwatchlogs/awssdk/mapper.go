package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	cloudwatchlogsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs"
)

func mapLogGroup(
	raw awscloudwatchlogstypes.LogGroup,
	tags map[string]string,
) cloudwatchlogsservice.LogGroup {
	return cloudwatchlogsservice.LogGroup{
		ARN:                  logGroupARN(raw),
		Name:                 strings.TrimSpace(aws.ToString(raw.LogGroupName)),
		CreationTime:         unixMillisTime(raw.CreationTime),
		RetentionInDays:      aws.ToInt32(raw.RetentionInDays),
		StoredBytes:          aws.ToInt64(raw.StoredBytes),
		MetricFilterCount:    aws.ToInt32(raw.MetricFilterCount),
		LogGroupClass:        string(raw.LogGroupClass),
		DataProtectionStatus: string(raw.DataProtectionStatus),
		InheritedProperties:  inheritedProperties(raw.InheritedProperties),
		KMSKeyID:             strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		DeletionProtected:    aws.ToBool(raw.DeletionProtectionEnabled),
		BearerTokenAuth:      aws.ToBool(raw.BearerTokenAuthenticationEnabled),
		Tags:                 tags,
	}
}

func logGroupARN(raw awscloudwatchlogstypes.LogGroup) string {
	if arn := strings.TrimSpace(aws.ToString(raw.LogGroupArn)); arn != "" {
		return arn
	}
	return trimLogGroupWildcardARN(aws.ToString(raw.Arn))
}

func tagResourceARN(raw awscloudwatchlogstypes.LogGroup) string {
	return logGroupARN(raw)
}

func trimLogGroupWildcardARN(arn string) string {
	return strings.TrimSuffix(strings.TrimSpace(arn), ":*")
}

func unixMillisTime(value *int64) time.Time {
	millis := aws.ToInt64(value)
	if millis == 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}

func inheritedProperties(properties []awscloudwatchlogstypes.InheritedProperty) []string {
	var output []string
	for _, property := range properties {
		if value := strings.TrimSpace(string(property)); value != "" {
			output = append(output, value)
		}
	}
	return output
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for key, value := range tags {
		output[key] = value
	}
	return output
}
