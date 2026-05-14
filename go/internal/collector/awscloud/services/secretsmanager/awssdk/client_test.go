package awssdk

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awssecretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsSecretsManagerMetadataOnly(t *testing.T) {
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:orders-db-a1b2c3"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	rotationARN := "arn:aws:lambda:us-east-1:123456789012:function:rotate-orders-db"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeSecretsManagerAPI{
		pages: []*awssecretsmanager.ListSecretsOutput{{
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:               aws.String(secretARN),
				Name:              aws.String("orders/db"),
				Description:       aws.String("database password"),
				KmsKeyId:          aws.String(kmsARN),
				RotationEnabled:   aws.Bool(true),
				RotationLambdaARN: aws.String(rotationARN),
				CreatedDate:       aws.Time(createdAt),
				LastChangedDate:   aws.Time(createdAt.Add(time.Hour)),
				LastRotatedDate:   aws.Time(createdAt.Add(2 * time.Hour)),
				NextRotationDate:  aws.Time(createdAt.Add(30 * 24 * time.Hour)),
				PrimaryRegion:     aws.String("us-east-1"),
				OwningService:     aws.String("rds"),
				Type:              aws.String("aws"),
				RotationRules: &awssecretsmanagertypes.RotationRulesType{
					AutomaticallyAfterDays: aws.Int64(30),
					Duration:               aws.String("2h"),
					ScheduleExpression:     aws.String("rate(30 days)"),
				},
				SecretVersionsToStages: map[string][]string{
					"version-id": {"AWSCURRENT"},
				},
				ExternalSecretRotationMetadata: []awssecretsmanagertypes.ExternalSecretRotationMetadataItem{{
					Key:   aws.String("partner"),
					Value: aws.String("payload"),
				}},
				ExternalSecretRotationRoleArn: aws.String("arn:aws:iam::123456789012:role/external-rotation"),
				Tags: []awssecretsmanagertypes.Tag{{
					Key:   aws.String("Environment"),
					Value: aws.String("prod"),
				}},
			}},
			NextToken: aws.String("secrets-next"),
		}, {
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:  aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:payments-db-d4e5f6"),
				Name: aws.String("payments/db"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	secrets, err := adapter.ListSecrets(context.Background())
	if err != nil {
		t.Fatalf("ListSecrets() error = %v, want nil", err)
	}

	if got, want := len(secrets), 2; got != want {
		t.Fatalf("len(secrets) = %d, want %d", got, want)
	}
	if got, want := api.maxResults, []int32{100, 100}; !slices.Equal(got, want) {
		t.Fatalf("ListSecrets MaxResults = %#v, want %#v", got, want)
	}
	if got, want := api.nextTokens, []string{"", "secrets-next"}; !slices.Equal(got, want) {
		t.Fatalf("ListSecrets NextToken = %#v, want %#v", got, want)
	}
	if got, want := api.includePlannedDeletion, []bool{true, true}; !slices.Equal(got, want) {
		t.Fatalf("ListSecrets IncludePlannedDeletion = %#v, want %#v", got, want)
	}
	secret := secrets[0]
	if secret.ARN != secretARN || secret.Name != "orders/db" {
		t.Fatalf("secret identity = %#v, want ARN and name", secret)
	}
	if !secret.DescriptionPresent {
		t.Fatalf("DescriptionPresent = false, want true")
	}
	if secret.KMSKeyID != kmsARN || secret.RotationLambdaARN != rotationARN {
		t.Fatalf("secret dependencies = %#v, want KMS and rotation Lambda", secret)
	}
	if !secret.RotationEnabled || secret.RotationEveryDays != 30 ||
		secret.RotationDuration != "2h" || secret.RotationSchedule != "rate(30 days)" {
		t.Fatalf("rotation metadata = %#v, want safe rotation settings", secret)
	}
	if secret.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want Environment tag", secret.Tags)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecretsManager,
	}
}

type fakeSecretsManagerAPI struct {
	pages                  []*awssecretsmanager.ListSecretsOutput
	calls                  int
	maxResults             []int32
	nextTokens             []string
	includePlannedDeletion []bool
}

func (f *fakeSecretsManagerAPI) ListSecrets(
	_ context.Context,
	input *awssecretsmanager.ListSecretsInput,
	_ ...func(*awssecretsmanager.Options),
) (*awssecretsmanager.ListSecretsOutput, error) {
	f.maxResults = append(f.maxResults, aws.ToInt32(input.MaxResults))
	f.nextTokens = append(f.nextTokens, aws.ToString(input.NextToken))
	f.includePlannedDeletion = append(f.includePlannedDeletion, aws.ToBool(input.IncludePlannedDeletion))
	if f.calls >= len(f.pages) {
		return &awssecretsmanager.ListSecretsOutput{}, nil
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}
