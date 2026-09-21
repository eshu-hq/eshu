// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslicensemanager "github.com/aws/aws-sdk-go-v2/service/licensemanager"
	awslicensemanagertypes "github.com/aws/aws-sdk-go-v2/service/licensemanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsLicenseManagerMetadataOnly(t *testing.T) {
	configARN := "arn:aws:license-manager:us-east-1:123456789012:license-configuration:lic-0abc123"
	instanceARN := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"

	api := &fakeLicenseManagerAPI{
		configPages: []*awslicensemanager.ListLicenseConfigurationsOutput{{
			LicenseConfigurations: []awslicensemanagertypes.LicenseConfiguration{{
				LicenseConfigurationArn: aws.String(configARN),
				LicenseConfigurationId:  aws.String("lic-0abc123"),
				Name:                    aws.String("windows-server"),
				Status:                  aws.String("AVAILABLE"),
				LicenseCountingType:     awslicensemanagertypes.LicenseCountingTypeInstance,
				LicenseCount:            aws.Int64(100),
				LicenseCountHardLimit:   aws.Bool(true),
				ConsumedLicenses:        aws.Int64(12),
				OwnerAccountId:          aws.String("123456789012"),
				LicenseExpiry:           aws.Int64(1798761600),
				LicenseRules:            []string{"#minimumVcpus=2"},
				ProductInformationList: []awslicensemanagertypes.ProductInformation{{
					ResourceType: aws.String("SSM_MANAGED"),
				}},
			}},
		}},
		associationPages: map[string][]*awslicensemanager.ListAssociationsForLicenseConfigurationOutput{
			configARN: {{
				LicenseConfigurationAssociations: []awslicensemanagertypes.LicenseConfigurationAssociation{{
					ResourceArn:     aws.String(instanceARN),
					ResourceType:    awslicensemanagertypes.ResourceTypeEc2Instance,
					ResourceOwnerId: aws.String("123456789012"),
				}},
			}},
		},
		tags: map[string][]awslicensemanagertypes.Tag{
			configARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Configurations) != 1 {
		t.Fatalf("len(Configurations) = %d, want 1", len(snapshot.Configurations))
	}
	config := snapshot.Configurations[0]
	if config.ARN != configARN {
		t.Fatalf("config ARN = %q, want %q", config.ARN, configARN)
	}
	if config.LicenseCountingType != "Instance" {
		t.Fatalf("LicenseCountingType = %q, want Instance", config.LicenseCountingType)
	}
	if !config.LicenseCountConfigured || config.LicenseCount != 100 {
		t.Fatalf("LicenseCount = %d configured=%v, want 100 configured=true", config.LicenseCount, config.LicenseCountConfigured)
	}
	if config.ConsumedLicenses != 12 {
		t.Fatalf("ConsumedLicenses = %d, want 12", config.ConsumedLicenses)
	}
	if config.LicenseRuleCount != 1 {
		t.Fatalf("LicenseRuleCount = %d, want 1", config.LicenseRuleCount)
	}
	if config.ProductInformationCount != 1 {
		t.Fatalf("ProductInformationCount = %d, want 1", config.ProductInformationCount)
	}
	if config.LicenseExpiry.IsZero() {
		t.Fatalf("LicenseExpiry is zero, want a converted Unix timestamp")
	}
	if config.Tags["Environment"] != "prod" {
		t.Fatalf("tag Environment = %q, want prod", config.Tags["Environment"])
	}
	if len(config.Associations) != 1 {
		t.Fatalf("len(Associations) = %d, want 1", len(config.Associations))
	}
	association := config.Associations[0]
	if association.ResourceARN != instanceARN {
		t.Fatalf("association ResourceARN = %q, want %q", association.ResourceARN, instanceARN)
	}
	if association.ResourceType != "EC2_INSTANCE" {
		t.Fatalf("association ResourceType = %q, want EC2_INSTANCE", association.ResourceType)
	}
}

func TestClientLeavesLicenseCountUnconfiguredWhenNil(t *testing.T) {
	configARN := "arn:aws:license-manager:us-east-1:123456789012:license-configuration:lic-no-count"
	api := &fakeLicenseManagerAPI{
		configPages: []*awslicensemanager.ListLicenseConfigurationsOutput{{
			LicenseConfigurations: []awslicensemanagertypes.LicenseConfiguration{{
				LicenseConfigurationArn: aws.String(configARN),
				LicenseConfigurationId:  aws.String("lic-no-count"),
				Name:                    aws.String("no-count"),
			}},
		}},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	config := snapshot.Configurations[0]
	if config.LicenseCountConfigured {
		t.Fatalf("LicenseCountConfigured = true, want false for nil LicenseCount")
	}
}

type fakeLicenseManagerAPI struct {
	configPages      []*awslicensemanager.ListLicenseConfigurationsOutput
	configCall       int
	associationPages map[string][]*awslicensemanager.ListAssociationsForLicenseConfigurationOutput
	associationCalls map[string]int
	tags             map[string][]awslicensemanagertypes.Tag
}

func (f *fakeLicenseManagerAPI) ListLicenseConfigurations(
	_ context.Context,
	_ *awslicensemanager.ListLicenseConfigurationsInput,
	_ ...func(*awslicensemanager.Options),
) (*awslicensemanager.ListLicenseConfigurationsOutput, error) {
	if f.configCall >= len(f.configPages) {
		return &awslicensemanager.ListLicenseConfigurationsOutput{}, nil
	}
	page := f.configPages[f.configCall]
	f.configCall++
	return page, nil
}

func (f *fakeLicenseManagerAPI) ListAssociationsForLicenseConfiguration(
	_ context.Context,
	input *awslicensemanager.ListAssociationsForLicenseConfigurationInput,
	_ ...func(*awslicensemanager.Options),
) (*awslicensemanager.ListAssociationsForLicenseConfigurationOutput, error) {
	if f.associationCalls == nil {
		f.associationCalls = map[string]int{}
	}
	arn := aws.ToString(input.LicenseConfigurationArn)
	pages := f.associationPages[arn]
	idx := f.associationCalls[arn]
	if idx >= len(pages) {
		return &awslicensemanager.ListAssociationsForLicenseConfigurationOutput{}, nil
	}
	f.associationCalls[arn] = idx + 1
	return pages[idx], nil
}

func (f *fakeLicenseManagerAPI) ListTagsForResource(
	_ context.Context,
	input *awslicensemanager.ListTagsForResourceInput,
	_ ...func(*awslicensemanager.Options),
) (*awslicensemanager.ListTagsForResourceOutput, error) {
	return &awslicensemanager.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLicenseManager,
	}
}
