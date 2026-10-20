// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrolesanywhere "github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	awsrolesanywheretypes "github.com/aws/aws-sdk-go-v2/service/rolesanywhere/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsRolesAnywhereMetadataOnly(t *testing.T) {
	trustAnchorARN := "arn:aws:rolesanywhere:us-east-1:123456789012:trust-anchor/anchor1"
	profileARN := "arn:aws:rolesanywhere:us-east-1:123456789012:profile/profile1"
	crlARN := "arn:aws:rolesanywhere:us-east-1:123456789012:crl/crl1"
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/ca1"
	roleARN := "arn:aws:iam::123456789012:role/build-runner"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeRolesAnywhereAPI{
		trustAnchorPages: []*awsrolesanywhere.ListTrustAnchorsOutput{{
			TrustAnchors: []awsrolesanywheretypes.TrustAnchorDetail{{
				TrustAnchorArn: aws.String(trustAnchorARN),
				TrustAnchorId:  aws.String("anchor1"),
				Name:           aws.String("corp-pca"),
				Enabled:        aws.Bool(true),
				CreatedAt:      aws.Time(createdAt),
				Source: &awsrolesanywheretypes.Source{
					SourceType: awsrolesanywheretypes.TrustAnchorTypeAwsAcmPca,
					SourceData: &awsrolesanywheretypes.SourceDataMemberAcmPcaArn{Value: caARN},
				},
			}},
		}},
		profilePages: []*awsrolesanywhere.ListProfilesOutput{{
			Profiles: []awsrolesanywheretypes.ProfileDetail{{
				ProfileArn:            aws.String(profileARN),
				ProfileId:             aws.String("profile1"),
				Name:                  aws.String("ci-profile"),
				Enabled:               aws.Bool(true),
				DurationSeconds:       aws.Int32(3600),
				AcceptRoleSessionName: aws.Bool(true),
				RoleArns:              []string{roleARN},
				ManagedPolicyArns:     []string{"arn:aws:iam::aws:policy/ReadOnlyAccess"},
				SessionPolicy:         aws.String("{\"Version\":\"2012-10-17\"}"),
				AttributeMappings: []awsrolesanywheretypes.AttributeMapping{{
					CertificateField: awsrolesanywheretypes.CertificateFieldX509Subject,
				}},
				CreatedAt: aws.Time(createdAt),
			}},
		}},
		crlPages: []*awsrolesanywhere.ListCrlsOutput{{
			Crls: []awsrolesanywheretypes.CrlDetail{{
				CrlArn:         aws.String(crlARN),
				CrlId:          aws.String("crl1"),
				Name:           aws.String("corp-crl"),
				Enabled:        aws.Bool(true),
				TrustAnchorArn: aws.String(trustAnchorARN),
				CrlData:        []byte("SHOULD-NEVER-BE-PERSISTED"),
				CreatedAt:      aws.Time(createdAt),
			}},
		}},
		tags: map[string][]awsrolesanywheretypes.Tag{
			trustAnchorARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			profileARN:     {{Key: aws.String("Team"), Value: aws.String("platform")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.TrustAnchors) != 1 {
		t.Fatalf("len(TrustAnchors) = %d, want 1", len(snapshot.TrustAnchors))
	}
	anchor := snapshot.TrustAnchors[0]
	if anchor.ARN != trustAnchorARN {
		t.Fatalf("anchor ARN = %q, want %q", anchor.ARN, trustAnchorARN)
	}
	if anchor.SourceType != "AWS_ACM_PCA" {
		t.Fatalf("anchor SourceType = %q, want AWS_ACM_PCA", anchor.SourceType)
	}
	if anchor.ACMPCAArn != caARN {
		t.Fatalf("anchor ACMPCAArn = %q, want %q", anchor.ACMPCAArn, caARN)
	}
	if anchor.Tags["Environment"] != "prod" {
		t.Fatalf("anchor tag Environment = %q, want prod", anchor.Tags["Environment"])
	}

	if len(snapshot.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(snapshot.Profiles))
	}
	profile := snapshot.Profiles[0]
	if profile.ARN != profileARN {
		t.Fatalf("profile ARN = %q, want %q", profile.ARN, profileARN)
	}
	if len(profile.RoleARNs) != 1 || profile.RoleARNs[0] != roleARN {
		t.Fatalf("profile RoleARNs = %#v, want [%s]", profile.RoleARNs, roleARN)
	}
	if !profile.HasSessionPolicy {
		t.Fatalf("profile HasSessionPolicy = false, want true (session policy present)")
	}
	if profile.AttributeMappingCount != 1 {
		t.Fatalf("profile AttributeMappingCount = %d, want 1", profile.AttributeMappingCount)
	}
	if profile.DurationSeconds != 3600 {
		t.Fatalf("profile DurationSeconds = %d, want 3600", profile.DurationSeconds)
	}

	if len(snapshot.CRLs) != 1 {
		t.Fatalf("len(CRLs) = %d, want 1", len(snapshot.CRLs))
	}
	crl := snapshot.CRLs[0]
	if crl.ARN != crlARN {
		t.Fatalf("crl ARN = %q, want %q", crl.ARN, crlARN)
	}
	if crl.TrustAnchorARN != trustAnchorARN {
		t.Fatalf("crl TrustAnchorARN = %q, want %q", crl.TrustAnchorARN, trustAnchorARN)
	}
}

func TestTrustAnchorSourceIgnoresCertificateBundleData(t *testing.T) {
	source := &awsrolesanywheretypes.Source{
		SourceType: awsrolesanywheretypes.TrustAnchorTypeCertificateBundle,
		SourceData: &awsrolesanywheretypes.SourceDataMemberX509CertificateData{
			Value: "-----BEGIN CERTIFICATE-----SHOULD-NEVER-PERSIST-----END CERTIFICATE-----",
		},
	}
	sourceType, acmPcaARN := trustAnchorSource(source)
	if sourceType != "CERTIFICATE_BUNDLE" {
		t.Fatalf("sourceType = %q, want CERTIFICATE_BUNDLE", sourceType)
	}
	if acmPcaARN != "" {
		t.Fatalf("acmPcaARN = %q, want empty (certificate bundle data must never be persisted)", acmPcaARN)
	}
}

type fakeRolesAnywhereAPI struct {
	trustAnchorPages []*awsrolesanywhere.ListTrustAnchorsOutput
	trustAnchorCall  int
	profilePages     []*awsrolesanywhere.ListProfilesOutput
	profileCall      int
	crlPages         []*awsrolesanywhere.ListCrlsOutput
	crlCall          int
	tags             map[string][]awsrolesanywheretypes.Tag
}

func (f *fakeRolesAnywhereAPI) ListTrustAnchors(
	_ context.Context,
	_ *awsrolesanywhere.ListTrustAnchorsInput,
	_ ...func(*awsrolesanywhere.Options),
) (*awsrolesanywhere.ListTrustAnchorsOutput, error) {
	if f.trustAnchorCall >= len(f.trustAnchorPages) {
		return &awsrolesanywhere.ListTrustAnchorsOutput{}, nil
	}
	page := f.trustAnchorPages[f.trustAnchorCall]
	f.trustAnchorCall++
	return page, nil
}

func (f *fakeRolesAnywhereAPI) ListProfiles(
	_ context.Context,
	_ *awsrolesanywhere.ListProfilesInput,
	_ ...func(*awsrolesanywhere.Options),
) (*awsrolesanywhere.ListProfilesOutput, error) {
	if f.profileCall >= len(f.profilePages) {
		return &awsrolesanywhere.ListProfilesOutput{}, nil
	}
	page := f.profilePages[f.profileCall]
	f.profileCall++
	return page, nil
}

func (f *fakeRolesAnywhereAPI) ListCrls(
	_ context.Context,
	_ *awsrolesanywhere.ListCrlsInput,
	_ ...func(*awsrolesanywhere.Options),
) (*awsrolesanywhere.ListCrlsOutput, error) {
	if f.crlCall >= len(f.crlPages) {
		return &awsrolesanywhere.ListCrlsOutput{}, nil
	}
	page := f.crlPages[f.crlCall]
	f.crlCall++
	return page, nil
}

func (f *fakeRolesAnywhereAPI) ListTagsForResource(
	_ context.Context,
	input *awsrolesanywhere.ListTagsForResourceInput,
	_ ...func(*awsrolesanywhere.Options),
) (*awsrolesanywhere.ListTagsForResourceOutput, error) {
	return &awsrolesanywhere.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceRolesAnywhere,
	}
}
