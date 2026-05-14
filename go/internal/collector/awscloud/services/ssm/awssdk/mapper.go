package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	ssmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssm"
)

func mapParameter(raw awsssmtypes.ParameterMetadata) ssmservice.Parameter {
	return ssmservice.Parameter{
		ARN:                   strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:                  strings.TrimSpace(aws.ToString(raw.Name)),
		Type:                  strings.TrimSpace(string(raw.Type)),
		Tier:                  strings.TrimSpace(string(raw.Tier)),
		DataType:              strings.TrimSpace(aws.ToString(raw.DataType)),
		KeyID:                 strings.TrimSpace(aws.ToString(raw.KeyId)),
		LastModifiedAt:        timeValue(raw.LastModifiedDate),
		DescriptionPresent:    strings.TrimSpace(aws.ToString(raw.Description)) != "",
		AllowedPatternPresent: strings.TrimSpace(aws.ToString(raw.AllowedPattern)) != "",
		Policies:              policies(raw.Policies),
	}
}

func policies(input []awsssmtypes.ParameterInlinePolicy) []ssmservice.PolicyMetadata {
	if len(input) == 0 {
		return nil
	}
	output := make([]ssmservice.PolicyMetadata, 0, len(input))
	for _, policy := range input {
		metadata := ssmservice.PolicyMetadata{
			Type:   strings.TrimSpace(aws.ToString(policy.PolicyType)),
			Status: strings.TrimSpace(aws.ToString(policy.PolicyStatus)),
		}
		if metadata.Type == "" && metadata.Status == "" {
			continue
		}
		output = append(output, metadata)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeValue(value *time.Time) time.Time {
	if value == nil || value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func tags(input []awsssmtypes.Tag) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for _, tag := range input {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
