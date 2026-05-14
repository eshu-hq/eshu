package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	secretsmanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/secretsmanager"
)

func mapSecret(raw awssecretsmanagertypes.SecretListEntry) secretsmanagerservice.Secret {
	secret := secretsmanagerservice.Secret{
		ARN:                strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:               strings.TrimSpace(aws.ToString(raw.Name)),
		DescriptionPresent: strings.TrimSpace(aws.ToString(raw.Description)) != "",
		KMSKeyID:           strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		RotationEnabled:    aws.ToBool(raw.RotationEnabled),
		RotationLambdaARN:  strings.TrimSpace(aws.ToString(raw.RotationLambdaARN)),
		CreatedAt:          timeValue(raw.CreatedDate),
		DeletedAt:          timeValue(raw.DeletedDate),
		LastChangedAt:      timeValue(raw.LastChangedDate),
		LastRotatedAt:      timeValue(raw.LastRotatedDate),
		NextRotationAt:     timeValue(raw.NextRotationDate),
		PrimaryRegion:      strings.TrimSpace(aws.ToString(raw.PrimaryRegion)),
		OwningService:      strings.TrimSpace(aws.ToString(raw.OwningService)),
		SecretType:         strings.TrimSpace(aws.ToString(raw.Type)),
		Tags:               tags(raw.Tags),
	}
	if raw.RotationRules != nil {
		secret.RotationEveryDays = aws.ToInt64(raw.RotationRules.AutomaticallyAfterDays)
		secret.RotationDuration = strings.TrimSpace(aws.ToString(raw.RotationRules.Duration))
		secret.RotationSchedule = strings.TrimSpace(aws.ToString(raw.RotationRules.ScheduleExpression))
	}
	return secret
}

func timeValue(value *time.Time) time.Time {
	if value == nil || value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func tags(input []awssecretsmanagertypes.Tag) map[string]string {
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
