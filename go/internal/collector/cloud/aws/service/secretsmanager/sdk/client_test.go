// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"slices"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awssecretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsSecretsManagerMetadataOnly(t *testing.T) {
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:orders-db-a1b2c3"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	rotationARN := "arn:aws:lambda:us-east-1:123456789012:function:rotate-orders-db"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeSecretsManagerAPI{
		pages: []*awssecretsmanager.ListSecretsOutput{{
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:               awsv2.String(secretARN),
				Name:              awsv2.String("orders/db"),
				Description:       awsv2.String("database password"),
				KmsKeyId:          awsv2.String(kmsARN),
				RotationEnabled:   awsv2.Bool(true),
				RotationLambdaARN: awsv2.String(rotationARN),
				CreatedDate:       awsv2.Time(createdAt),
				LastChangedDate:   awsv2.Time(createdAt.Add(time.Hour)),
				LastRotatedDate:   awsv2.Time(createdAt.Add(2 * time.Hour)),
				NextRotationDate:  awsv2.Time(createdAt.Add(30 * 24 * time.Hour)),
				PrimaryRegion:     awsv2.String("us-east-1"),
				OwningService:     awsv2.String("rds"),
				Type:              awsv2.String("aws"),
				RotationRules: &awssecretsmanagertypes.RotationRulesType{
					AutomaticallyAfterDays: awsv2.Int64(30),
					Duration:               awsv2.String("2h"),
					ScheduleExpression:     awsv2.String("rate(30 days)"),
				},
				SecretVersionsToStages: map[string][]string{
					"version-id": {"AWSCURRENT"},
				},
				ExternalSecretRotationMetadata: []awssecretsmanagertypes.ExternalSecretRotationMetadataItem{{
					Key:   awsv2.String("partner"),
					Value: awsv2.String("payload"),
				}},
				ExternalSecretRotationRoleArn: awsv2.String("arn:aws:iam::123456789012:role/external-rotation"),
				Tags: []awssecretsmanagertypes.Tag{{
					Key:   awsv2.String("Environment"),
					Value: awsv2.String("prod"),
				}},
			}},
			NextToken: awsv2.String("secrets-next"),
		}, {
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:  awsv2.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:payments-db-d4e5f6"),
				Name: awsv2.String("payments/db"),
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

// TestMapSecretNilRotationRulesProducesZeroRotationFields proves that when
// RotationRules is nil (a secret with no configured rotation) the adapter
// emits zero values for RotationEveryDays, RotationDuration, and
// RotationSchedule rather than panicking or fabricating a value. This
// exercises the nil-guard on line 34 of mapper.go.
func TestMapSecretNilRotationRulesProducesZeroRotationFields(t *testing.T) {
	api := &fakeSecretsManagerAPI{
		pages: []*awssecretsmanager.ListSecretsOutput{{
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:             awsv2.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:no-rotation-a1b2c3"),
				Name:            awsv2.String("no-rotation"),
				RotationEnabled: awsv2.Bool(false),
				// RotationRules intentionally absent.
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	secrets, err := adapter.ListSecrets(context.Background())
	if err != nil {
		t.Fatalf("ListSecrets() error = %v, want nil", err)
	}
	if got, want := len(secrets), 1; got != want {
		t.Fatalf("len(secrets) = %d, want %d", got, want)
	}
	secret := secrets[0]
	if secret.RotationEnabled {
		t.Fatalf("RotationEnabled = true, want false")
	}
	if secret.RotationEveryDays != 0 {
		t.Fatalf("RotationEveryDays = %d, want 0 when RotationRules is nil", secret.RotationEveryDays)
	}
	if secret.RotationDuration != "" {
		t.Fatalf("RotationDuration = %q, want empty when RotationRules is nil", secret.RotationDuration)
	}
	if secret.RotationSchedule != "" {
		t.Fatalf("RotationSchedule = %q, want empty when RotationRules is nil", secret.RotationSchedule)
	}
}

// TestMapSecretTagsWithBlankKeySkipsEntry proves the tags helper (mapper.go
// line 49–64) silently drops Tag entries whose Key is nil, empty, or
// whitespace-only, so blank-keyed AWS SDK responses never produce map entries
// with empty-string keys in the scanner-owned Secret.
func TestMapSecretTagsWithBlankKeySkipsEntry(t *testing.T) {
	api := &fakeSecretsManagerAPI{
		pages: []*awssecretsmanager.ListSecretsOutput{{
			SecretList: []awssecretsmanagertypes.SecretListEntry{{
				ARN:  awsv2.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:tags-test-a1b2c3"),
				Name: awsv2.String("tags-test"),
				Tags: []awssecretsmanagertypes.Tag{
					{Key: awsv2.String(""), Value: awsv2.String("should-be-dropped")},
					{Key: awsv2.String("  "), Value: awsv2.String("whitespace-key-dropped")},
					{Key: nil, Value: awsv2.String("nil-key-dropped")},
					{Key: awsv2.String("Environment"), Value: awsv2.String("staging")},
				},
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	secrets, err := adapter.ListSecrets(context.Background())
	if err != nil {
		t.Fatalf("ListSecrets() error = %v, want nil", err)
	}
	if got, want := len(secrets), 1; got != want {
		t.Fatalf("len(secrets) = %d, want %d", got, want)
	}
	tags := secrets[0].Tags
	if got, want := len(tags), 1; got != want {
		t.Fatalf("len(tags) = %d, want %d (blank-keyed entries must be dropped)", got, want)
	}
	if tags["Environment"] != "staging" {
		t.Fatalf("tags[Environment] = %q, want staging", tags["Environment"])
	}
	if _, ok := tags[""]; ok {
		t.Fatalf("tags contains empty-string key, want it dropped")
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceSecretsManager,
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
	f.maxResults = append(f.maxResults, awsv2.ToInt32(input.MaxResults))
	f.nextTokens = append(f.nextTokens, awsv2.ToString(input.NextToken))
	f.includePlannedDeletion = append(f.includePlannedDeletion, awsv2.ToBool(input.IncludePlannedDeletion))
	if f.calls >= len(f.pages) {
		return &awssecretsmanager.ListSecretsOutput{}, nil
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}
