// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaccessanalyzer "github.com/aws/aws-sdk-go-v2/service/accessanalyzer"
	awsaccessanalyzertypes "github.com/aws/aws-sdk-go-v2/service/accessanalyzer/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	accessanalyzerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/accessanalyzer"
)

func TestClientListAnalyzersRedactsFindingBodiesArchiveFiltersAndUnusedActions(t *testing.T) {
	externalARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/account-external"
	unusedARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/org-unused"
	api := &fakeAccessAnalyzerAPI{
		analyzerPages: []*awsaccessanalyzer.ListAnalyzersOutput{{
			Analyzers: []awsaccessanalyzertypes.AnalyzerSummary{{
				Arn:                    aws.String(externalARN),
				Name:                   aws.String("account-external"),
				Type:                   awsaccessanalyzertypes.TypeAccount,
				Status:                 awsaccessanalyzertypes.AnalyzerStatusActive,
				CreatedAt:              aws.Time(time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)),
				LastResourceAnalyzed:   aws.String("arn:aws:s3:::prod-bucket"),
				LastResourceAnalyzedAt: aws.Time(time.Date(2026, 5, 27, 10, 15, 0, 0, time.UTC)),
				Tags:                   map[string]string{"Environment": "prod"},
			}, {
				Arn:                    aws.String(unusedARN),
				Name:                   aws.String("org-unused"),
				Type:                   awsaccessanalyzertypes.TypeOrganizationUnusedAccess,
				Status:                 awsaccessanalyzertypes.AnalyzerStatusActive,
				CreatedAt:              aws.Time(time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)),
				LastResourceAnalyzedAt: aws.Time(time.Date(2026, 5, 27, 11, 15, 0, 0, time.UTC)),
			}},
		}},
		archiveRulePages: []*awsaccessanalyzer.ListArchiveRulesOutput{{
			ArchiveRules: []awsaccessanalyzertypes.ArchiveRuleSummary{{
				RuleName:  aws.String("archive-known-cross-account"),
				CreatedAt: aws.Time(time.Date(2026, 5, 27, 10, 20, 0, 0, time.UTC)),
				UpdatedAt: aws.Time(time.Date(2026, 5, 27, 10, 30, 0, 0, time.UTC)),
				Filter: map[string]awsaccessanalyzertypes.Criterion{
					"principal.AWS": {Eq: []string{"arn:aws:iam::999999999999:root"}},
				},
			}},
		}},
		findingPages: []*awsaccessanalyzer.ListFindingsOutput{{
			Findings: []awsaccessanalyzertypes.FindingSummary{{
				Id:                   aws.String("finding-1"),
				Status:               awsaccessanalyzertypes.FindingStatusActive,
				ResourceType:         awsaccessanalyzertypes.ResourceTypeAwsS3Bucket,
				Resource:             aws.String("arn:aws:s3:::prod-bucket"),
				Action:               []string{"s3:GetObject"},
				Condition:            map[string]string{"aws:PrincipalOrgID": "o-secret"},
				Principal:            map[string]string{"AWS": "arn:aws:iam::999999999999:root"},
				ResourceOwnerAccount: aws.String("123456789012"),
				Sources: []awsaccessanalyzertypes.FindingSource{{
					Type: awsaccessanalyzertypes.FindingSourceTypePolicy,
					Detail: &awsaccessanalyzertypes.FindingSourceDetail{
						AccessPointArn: aws.String("arn:aws:s3:us-east-1:123456789012:accesspoint/prod"),
					},
				}},
			}},
		}},
		findingV2Pages: []*awsaccessanalyzer.ListFindingsV2Output{{
			Findings: []awsaccessanalyzertypes.FindingSummaryV2{{
				Id:                   aws.String("unused-1"),
				FindingType:          awsaccessanalyzertypes.FindingTypeUnusedPermission,
				Status:               awsaccessanalyzertypes.FindingStatusActive,
				Resource:             aws.String("arn:aws:iam::123456789012:role/stale-admin"),
				ResourceOwnerAccount: aws.String("123456789012"),
				ResourceType:         awsaccessanalyzertypes.ResourceTypeAwsIamRole,
				AnalyzedAt:           aws.Time(time.Date(2026, 5, 27, 11, 20, 0, 0, time.UTC)),
				UpdatedAt:            aws.Time(time.Date(2026, 5, 27, 11, 25, 0, 0, time.UTC)),
			}},
		}},
		getFindingV2Pages: []*awsaccessanalyzer.GetFindingV2Output{{
			Id:                   aws.String("unused-1"),
			FindingType:          awsaccessanalyzertypes.FindingTypeUnusedPermission,
			Status:               awsaccessanalyzertypes.FindingStatusActive,
			Resource:             aws.String("arn:aws:iam::123456789012:role/stale-admin"),
			ResourceOwnerAccount: aws.String("123456789012"),
			ResourceType:         awsaccessanalyzertypes.ResourceTypeAwsIamRole,
			FindingDetails: []awsaccessanalyzertypes.FindingDetails{
				&awsaccessanalyzertypes.FindingDetailsMemberUnusedPermissionDetails{
					Value: awsaccessanalyzertypes.UnusedPermissionDetails{
						ServiceNamespace: aws.String("iam"),
						LastAccessed:     aws.Time(time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)),
						Actions: []awsaccessanalyzertypes.UnusedAction{{
							Action:       aws.String("iam:DeleteRole"),
							LastAccessed: aws.Time(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)),
						}},
					},
				},
			},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAccessAnalyzer},
	}

	analyzers, err := adapter.ListAnalyzers(context.Background())
	if err != nil {
		t.Fatalf("ListAnalyzers() error = %v, want nil", err)
	}
	if api.getFindingCalls != 0 {
		t.Fatalf("GetFinding calls = %d, want 0; external finding bodies must not be read", api.getFindingCalls)
	}
	if got, want := len(analyzers), 2; got != want {
		t.Fatalf("len(analyzers) = %d, want %d", got, want)
	}
	external := analyzers[0]
	if got, want := len(external.ArchiveRules), 1; got != want {
		t.Fatalf("len(external.ArchiveRules) = %d, want %d", got, want)
	}
	if got, want := external.FindingCounts, []accessanalyzerservice.FindingCount{{Status: "ACTIVE", ResourceType: "AWS::S3::Bucket", Count: 1}}; !equalFindingCounts(got, want) {
		t.Fatalf("FindingCounts = %#v, want %#v", got, want)
	}
	unused := analyzers[1]
	if got, want := len(unused.UnusedAccessSummaries), 1; got != want {
		t.Fatalf("len(unused.UnusedAccessSummaries) = %d, want %d", got, want)
	}
	summary := unused.UnusedAccessSummaries[0]
	if got, want := summary.LastAccessedAt, time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("LastAccessedAt = %v, want %v", got, want)
	}
	if summary.ResourceID != "arn:aws:iam::123456789012:role/stale-admin" {
		t.Fatalf("ResourceID = %q, want unused IAM role ARN", summary.ResourceID)
	}
}

func TestClientCapsUnusedFindingDetailReads(t *testing.T) {
	unusedARN := "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/org-unused"
	api := &fakeAccessAnalyzerAPI{
		analyzerPages: []*awsaccessanalyzer.ListAnalyzersOutput{{
			Analyzers: []awsaccessanalyzertypes.AnalyzerSummary{{
				Arn:    aws.String(unusedARN),
				Name:   aws.String("org-unused"),
				Type:   awsaccessanalyzertypes.TypeOrganizationUnusedAccess,
				Status: awsaccessanalyzertypes.AnalyzerStatusActive,
			}},
		}},
		findingV2Pages: []*awsaccessanalyzer.ListFindingsV2Output{{
			Findings: unusedFindingSummaries(maxUnusedAccessDetailReads + 1),
		}},
		getFindingV2Pages: unusedFindingDetails(maxUnusedAccessDetailReads + 1),
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAccessAnalyzer},
	}

	analyzers, err := adapter.ListAnalyzers(context.Background())
	if err != nil {
		t.Fatalf("ListAnalyzers() error = %v, want nil", err)
	}
	if got, want := api.getFindingV2Calls, maxUnusedAccessDetailReads; got != want {
		t.Fatalf("GetFindingV2 calls = %d, want bounded %d", got, want)
	}
	if got, want := len(analyzers[0].UnusedAccessSummaries), maxUnusedAccessDetailReads; got != want {
		t.Fatalf("len(UnusedAccessSummaries) = %d, want %d", got, want)
	}
	if got, want := len(analyzers[0].Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	warning := analyzers[0].Warnings[0]
	if warning.WarningKind != awscloud.WarningBudgetExhausted {
		t.Fatalf("WarningKind = %q, want %q", warning.WarningKind, awscloud.WarningBudgetExhausted)
	}
}

func equalFindingCounts(got []accessanalyzerservice.FindingCount, want []accessanalyzerservice.FindingCount) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func unusedFindingSummaries(count int) []awsaccessanalyzertypes.FindingSummaryV2 {
	findings := make([]awsaccessanalyzertypes.FindingSummaryV2, 0, count)
	for index := 0; index < count; index++ {
		suffix := strconv.Itoa(index)
		findings = append(findings, awsaccessanalyzertypes.FindingSummaryV2{
			Id:                   aws.String("unused-" + suffix),
			FindingType:          awsaccessanalyzertypes.FindingTypeUnusedPermission,
			Status:               awsaccessanalyzertypes.FindingStatusActive,
			Resource:             aws.String("arn:aws:iam::123456789012:role/stale-admin-" + suffix),
			ResourceOwnerAccount: aws.String("123456789012"),
			ResourceType:         awsaccessanalyzertypes.ResourceTypeAwsIamRole,
		})
	}
	return findings
}

func unusedFindingDetails(count int) []*awsaccessanalyzer.GetFindingV2Output {
	details := make([]*awsaccessanalyzer.GetFindingV2Output, 0, count)
	for index := 0; index < count; index++ {
		details = append(details, &awsaccessanalyzer.GetFindingV2Output{
			Id: aws.String("unused-" + strconv.Itoa(index)),
			FindingDetails: []awsaccessanalyzertypes.FindingDetails{
				&awsaccessanalyzertypes.FindingDetailsMemberUnusedIamRoleDetails{
					Value: awsaccessanalyzertypes.UnusedIamRoleDetails{
						LastAccessed: aws.Time(time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)),
					},
				},
			},
		})
	}
	return details
}

type fakeAccessAnalyzerAPI struct {
	analyzerPages     []*awsaccessanalyzer.ListAnalyzersOutput
	analyzerCalls     int
	archiveRulePages  []*awsaccessanalyzer.ListArchiveRulesOutput
	archiveRuleCalls  int
	findingPages      []*awsaccessanalyzer.ListFindingsOutput
	findingCalls      int
	findingV2Pages    []*awsaccessanalyzer.ListFindingsV2Output
	findingV2Calls    int
	getFindingV2Pages []*awsaccessanalyzer.GetFindingV2Output
	getFindingV2Calls int
	getFindingCalls   int
}

func (f *fakeAccessAnalyzerAPI) ListAnalyzers(
	_ context.Context,
	_ *awsaccessanalyzer.ListAnalyzersInput,
	_ ...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.ListAnalyzersOutput, error) {
	if f.analyzerCalls >= len(f.analyzerPages) {
		return &awsaccessanalyzer.ListAnalyzersOutput{}, nil
	}
	page := f.analyzerPages[f.analyzerCalls]
	f.analyzerCalls++
	return page, nil
}

func (f *fakeAccessAnalyzerAPI) ListArchiveRules(
	_ context.Context,
	_ *awsaccessanalyzer.ListArchiveRulesInput,
	_ ...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.ListArchiveRulesOutput, error) {
	if f.archiveRuleCalls >= len(f.archiveRulePages) {
		return &awsaccessanalyzer.ListArchiveRulesOutput{}, nil
	}
	page := f.archiveRulePages[f.archiveRuleCalls]
	f.archiveRuleCalls++
	return page, nil
}

func (f *fakeAccessAnalyzerAPI) ListFindings(
	_ context.Context,
	_ *awsaccessanalyzer.ListFindingsInput,
	_ ...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.ListFindingsOutput, error) {
	if f.findingCalls >= len(f.findingPages) {
		return &awsaccessanalyzer.ListFindingsOutput{}, nil
	}
	page := f.findingPages[f.findingCalls]
	f.findingCalls++
	return page, nil
}

func (f *fakeAccessAnalyzerAPI) ListFindingsV2(
	_ context.Context,
	_ *awsaccessanalyzer.ListFindingsV2Input,
	_ ...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.ListFindingsV2Output, error) {
	if f.findingV2Calls >= len(f.findingV2Pages) {
		return &awsaccessanalyzer.ListFindingsV2Output{}, nil
	}
	page := f.findingV2Pages[f.findingV2Calls]
	f.findingV2Calls++
	return page, nil
}

func (f *fakeAccessAnalyzerAPI) GetFindingV2(
	_ context.Context,
	_ *awsaccessanalyzer.GetFindingV2Input,
	_ ...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.GetFindingV2Output, error) {
	if f.getFindingV2Calls >= len(f.getFindingV2Pages) {
		return &awsaccessanalyzer.GetFindingV2Output{}, nil
	}
	page := f.getFindingV2Pages[f.getFindingV2Calls]
	f.getFindingV2Calls++
	return page, nil
}

func (f *fakeAccessAnalyzerAPI) GetFinding(
	context.Context,
	*awsaccessanalyzer.GetFindingInput,
	...func(*awsaccessanalyzer.Options),
) (*awsaccessanalyzer.GetFindingOutput, error) {
	f.getFindingCalls++
	return &awsaccessanalyzer.GetFindingOutput{
		Finding: &awsaccessanalyzertypes.Finding{
			Action:    []string{"s3:GetObject"},
			Principal: map[string]string{"AWS": "arn:aws:iam::999999999999:root"},
		},
	}, nil
}
