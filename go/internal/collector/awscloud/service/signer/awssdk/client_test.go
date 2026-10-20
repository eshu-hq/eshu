// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssigner "github.com/aws/aws-sdk-go-v2/service/signer"
	awssignertypes "github.com/aws/aws-sdk-go-v2/service/signer/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsSignerMetadataOnly(t *testing.T) {
	profileARN := "arn:aws:signer:us-east-1:123456789012:/signing-profiles/lambda_release"
	profileVersionARN := profileARN + "/AbCdEf123456"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/abcd"

	api := &fakeSignerAPI{
		platformPages: []*awssigner.ListSigningPlatformsOutput{{
			Platforms: []awssignertypes.SigningPlatform{{
				PlatformId:          aws.String("AWSLambda-SHA384-ECDSA"),
				DisplayName:         aws.String("AWS Lambda"),
				Category:            awssignertypes.CategoryAWSIoT,
				Target:              aws.String("Lambda"),
				MaxSizeInMB:         250,
				RevocationSupported: true,
			}},
		}},
		profilePages: []*awssigner.ListSigningProfilesOutput{{
			Profiles: []awssignertypes.SigningProfile{{
				Arn:                 aws.String(profileARN),
				ProfileVersionArn:   aws.String(profileVersionARN),
				ProfileName:         aws.String("lambda_release"),
				ProfileVersion:      aws.String("AbCdEf123456"),
				PlatformId:          aws.String("AWSLambda-SHA384-ECDSA"),
				PlatformDisplayName: aws.String("AWS Lambda"),
				Status:              awssignertypes.SigningProfileStatusActive,
				SignatureValidityPeriod: &awssignertypes.SignatureValidityPeriod{
					Type:  awssignertypes.ValidityTypeDays,
					Value: 135,
				},
				SigningMaterial:   &awssignertypes.SigningMaterial{CertificateArn: aws.String(certificateARN)},
				SigningParameters: map[string]string{"release-channel": "stable-secret-value"},
				Tags:              map[string]string{"Environment": "prod"},
			}},
		}},
		profileDetail: map[string]*awssigner.GetSigningProfileOutput{
			"lambda_release": {
				Overrides: &awssignertypes.SigningPlatformOverrides{
					SigningImageFormat: awssignertypes.ImageFormatJSONEmbedded,
				},
			},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Platforms) != 1 {
		t.Fatalf("len(Platforms) = %d, want 1", len(snapshot.Platforms))
	}
	platform := snapshot.Platforms[0]
	if platform.PlatformID != "AWSLambda-SHA384-ECDSA" {
		t.Fatalf("platform id = %q, want AWSLambda-SHA384-ECDSA", platform.PlatformID)
	}
	if platform.Category != "AWSIoT" {
		t.Fatalf("platform category = %q, want AWSIoT", platform.Category)
	}
	if platform.MaxSizeInMB != 250 || !platform.RevocationSupported {
		t.Fatalf("platform max/revocation = %d/%v, want 250/true", platform.MaxSizeInMB, platform.RevocationSupported)
	}

	if len(snapshot.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(snapshot.Profiles))
	}
	profile := snapshot.Profiles[0]
	if profile.ARN != profileARN {
		t.Fatalf("profile ARN = %q, want %q", profile.ARN, profileARN)
	}
	if profile.PlatformID != "AWSLambda-SHA384-ECDSA" {
		t.Fatalf("profile platform id = %q, want AWSLambda-SHA384-ECDSA", profile.PlatformID)
	}
	if profile.Status != "Active" {
		t.Fatalf("profile status = %q, want Active", profile.Status)
	}
	if profile.SignatureValidityType != "DAYS" || profile.SignatureValidityValue != 135 {
		t.Fatalf("validity = %q/%d, want DAYS/135", profile.SignatureValidityType, profile.SignatureValidityValue)
	}
	if profile.CertificateARN != certificateARN {
		t.Fatalf("profile certificate ARN = %q, want %q", profile.CertificateARN, certificateARN)
	}
	if profile.SigningImageFormat != "JSONEmbedded" {
		t.Fatalf("signing image format = %q, want JSONEmbedded", profile.SigningImageFormat)
	}
	if len(profile.SigningParameterNames) != 1 || profile.SigningParameterNames[0] != "release-channel" {
		t.Fatalf("signing parameter names = %#v, want [release-channel]", profile.SigningParameterNames)
	}
	if profile.Tags["Environment"] != "prod" {
		t.Fatalf("profile tag Environment = %q, want prod", profile.Tags["Environment"])
	}
}

type fakeSignerAPI struct {
	platformPages []*awssigner.ListSigningPlatformsOutput
	platformCall  int
	profilePages  []*awssigner.ListSigningProfilesOutput
	profileCall   int
	profileDetail map[string]*awssigner.GetSigningProfileOutput
}

func (f *fakeSignerAPI) ListSigningProfiles(
	_ context.Context,
	_ *awssigner.ListSigningProfilesInput,
	_ ...func(*awssigner.Options),
) (*awssigner.ListSigningProfilesOutput, error) {
	if f.profileCall >= len(f.profilePages) {
		return &awssigner.ListSigningProfilesOutput{}, nil
	}
	page := f.profilePages[f.profileCall]
	f.profileCall++
	return page, nil
}

func (f *fakeSignerAPI) GetSigningProfile(
	_ context.Context,
	input *awssigner.GetSigningProfileInput,
	_ ...func(*awssigner.Options),
) (*awssigner.GetSigningProfileOutput, error) {
	if detail, ok := f.profileDetail[aws.ToString(input.ProfileName)]; ok {
		return detail, nil
	}
	return &awssigner.GetSigningProfileOutput{}, nil
}

func (f *fakeSignerAPI) ListSigningPlatforms(
	_ context.Context,
	_ *awssigner.ListSigningPlatformsInput,
	_ ...func(*awssigner.Options),
) (*awssigner.ListSigningPlatformsOutput, error) {
	if f.platformCall >= len(f.platformPages) {
		return &awssigner.ListSigningPlatformsOutput{}, nil
	}
	page := f.platformPages[f.platformCall]
	f.platformCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSigner,
	}
}
