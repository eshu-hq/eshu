// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"slices"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsguardduty "github.com/aws/aws-sdk-go-v2/service/guardduty"
	gdtypes "github.com/aws/aws-sdk-go-v2/service/guardduty/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListDetectorsReadsMetadataAndNeverFetchesFindingBodiesOrListContents(t *testing.T) {
	detectorID := "12abc34d567e8fa901bc2d34eexample"
	destinationARN := "arn:aws:s3:::guardduty-findings"
	threatListLocation := "arn:aws:s3:::guardduty-threat-intel/list.txt"
	ipSetLocation := "arn:aws:s3:::guardduty-ip-set/list.txt"
	featureUpdatedAt := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	api := &fakeGuardDutyAPI{
		detectorPages: []*awsguardduty.ListDetectorsOutput{{
			DetectorIds: []string{detectorID},
		}},
		detectors: map[string]*awsguardduty.GetDetectorOutput{
			detectorID: {
				Status:                     gdtypes.DetectorStatusEnabled,
				FindingPublishingFrequency: gdtypes.FindingPublishingFrequencyFifteenMinutes,
				CreatedAt:                  awsv2.String("2026-05-27T12:00:00Z"),
				UpdatedAt:                  awsv2.String("2026-05-27T12:05:00Z"),
				Tags:                       map[string]string{"Environment": "prod"},
				Features: []gdtypes.DetectorFeatureConfigurationResult{{
					Name:      gdtypes.DetectorFeatureResult("S3_DATA_EVENTS"),
					Status:    gdtypes.FeatureStatusEnabled,
					UpdatedAt: awsv2.Time(featureUpdatedAt),
					AdditionalConfiguration: []gdtypes.DetectorAdditionalConfigurationResult{{
						Name:      gdtypes.FeatureAdditionalConfiguration("EKS_ADDON_MANAGEMENT"),
						Status:    gdtypes.FeatureStatusDisabled,
						UpdatedAt: awsv2.Time(featureUpdatedAt.Add(time.Minute)),
					}},
				}},
			},
		},
		members: map[string][]*awsguardduty.ListMembersOutput{
			detectorID: {{
				Members: []gdtypes.Member{{
					AccountId:          awsv2.String("111122223333"),
					AdministratorId:    awsv2.String("123456789012"),
					DetectorId:         awsv2.String("member-detector-id"),
					Email:              awsv2.String("security@example.com"),
					RelationshipStatus: awsv2.String("Enabled"),
					UpdatedAt:          awsv2.String("2026-05-27T12:10:00Z"),
				}},
			}},
		},
		filters: map[string][]*awsguardduty.ListFiltersOutput{
			detectorID: {{
				FilterNames: []string{"archive-known-benign"},
			}},
		},
		publishing: map[string][]*awsguardduty.ListPublishingDestinationsOutput{
			detectorID: {{
				Destinations: []gdtypes.Destination{{
					DestinationId:   awsv2.String("dest-1"),
					DestinationType: gdtypes.DestinationTypeS3,
					Status:          gdtypes.PublishingStatusPublishing,
				}},
			}},
		},
		publishingDetails: map[string]*awsguardduty.DescribePublishingDestinationOutput{
			detectorID + "/dest-1": {
				DestinationId:   awsv2.String("dest-1"),
				DestinationType: gdtypes.DestinationTypeS3,
				Status:          gdtypes.PublishingStatusPublishing,
				DestinationProperties: &gdtypes.DestinationProperties{
					DestinationArn: awsv2.String(destinationARN),
				},
				Tags: map[string]string{"Pipeline": "security"},
			},
		},
		threatSets: map[string][]*awsguardduty.ListThreatIntelSetsOutput{
			detectorID: {{
				ThreatIntelSetIds: []string{"threat-1"},
			}},
		},
		threatSetDetails: map[string]*awsguardduty.GetThreatIntelSetOutput{
			detectorID + "/threat-1": {
				Name:     awsv2.String("known-threats"),
				Format:   gdtypes.ThreatIntelSetFormatTxt,
				Status:   gdtypes.ThreatIntelSetStatusActive,
				Location: awsv2.String(threatListLocation),
				Tags:     map[string]string{"Source": "security"},
			},
		},
		ipSets: map[string][]*awsguardduty.ListIPSetsOutput{
			detectorID: {{
				IpSetIds: []string{"ipset-1"},
			}},
		},
		ipSetDetails: map[string]*awsguardduty.GetIPSetOutput{
			detectorID + "/ipset-1": {
				Name:     awsv2.String("trusted-egress"),
				Format:   gdtypes.IpSetFormatTxt,
				Status:   gdtypes.IpSetStatusActive,
				Location: awsv2.String(ipSetLocation),
				Tags:     map[string]string{"Source": "network"},
			},
		},
		statistics: map[gdtypes.GroupByType]*awsguardduty.GetFindingsStatisticsOutput{
			gdtypes.GroupByTypeSeverity: {
				FindingStatistics: &gdtypes.FindingStatistics{
					GroupedBySeverity: []gdtypes.SeverityStatistics{{
						Severity:      awsv2.Float64(7),
						TotalFindings: awsv2.Int32(3),
					}},
				},
			},
			gdtypes.GroupByTypeFindingType: {
				FindingStatistics: &gdtypes.FindingStatistics{
					GroupedByFindingType: []gdtypes.FindingTypeStatistics{{
						FindingType:   awsv2.String("UnauthorizedAccess:IAMUser/InstanceCredentialExfiltration"),
						TotalFindings: awsv2.Int32(2),
					}},
				},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceGuardDuty},
	}

	detectors, err := adapter.ListDetectors(context.Background())
	if err != nil {
		t.Fatalf("ListDetectors() error = %v, want nil", err)
	}
	if got, want := len(detectors), 1; got != want {
		t.Fatalf("len(detectors) = %d, want %d", got, want)
	}
	detector := detectors[0]
	if detector.ID != detectorID {
		t.Fatalf("detector.ID = %q, want %q", detector.ID, detectorID)
	}
	if detector.FindingCountsBySeverity["7"] != 3 {
		t.Fatalf("FindingCountsBySeverity = %#v, want 7=3", detector.FindingCountsBySeverity)
	}
	if detector.FindingCountsByType["UnauthorizedAccess:IAMUser/InstanceCredentialExfiltration"] != 2 {
		t.Fatalf("FindingCountsByType = %#v, want type count 2", detector.FindingCountsByType)
	}
	if detector.ThreatIntelSets[0].LocationARN != threatListLocation {
		t.Fatalf("ThreatIntelSet location = %q, want %q", detector.ThreatIntelSets[0].LocationARN, threatListLocation)
	}
	if detector.IPSets[0].LocationARN != ipSetLocation {
		t.Fatalf("IPSet location = %q, want %q", detector.IPSets[0].LocationARN, ipSetLocation)
	}

	for _, forbidden := range []string{"GetFindings", "ListFindings", "GetFilter", "ResolveThreatIntelLocation", "ResolveIPSetLocation"} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden GuardDuty call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

type fakeGuardDutyAPI struct {
	detectorPages     []*awsguardduty.ListDetectorsOutput
	detectorCalls     int
	detectors         map[string]*awsguardduty.GetDetectorOutput
	members           map[string][]*awsguardduty.ListMembersOutput
	memberCalls       map[string]int
	filters           map[string][]*awsguardduty.ListFiltersOutput
	filterCalls       map[string]int
	publishing        map[string][]*awsguardduty.ListPublishingDestinationsOutput
	publishingCalls   map[string]int
	publishingDetails map[string]*awsguardduty.DescribePublishingDestinationOutput
	threatSets        map[string][]*awsguardduty.ListThreatIntelSetsOutput
	threatSetCalls    map[string]int
	threatSetDetails  map[string]*awsguardduty.GetThreatIntelSetOutput
	ipSets            map[string][]*awsguardduty.ListIPSetsOutput
	ipSetCalls        map[string]int
	ipSetDetails      map[string]*awsguardduty.GetIPSetOutput
	statistics        map[gdtypes.GroupByType]*awsguardduty.GetFindingsStatisticsOutput
	calls             []string
}

func (f *fakeGuardDutyAPI) ListDetectors(
	_ context.Context,
	_ *awsguardduty.ListDetectorsInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListDetectorsOutput, error) {
	f.calls = append(f.calls, "ListDetectors")
	page := nextPage(f.detectorPages, &f.detectorCalls)
	if page == nil {
		return &awsguardduty.ListDetectorsOutput{}, nil
	}
	return page, nil
}

func (f *fakeGuardDutyAPI) GetDetector(
	_ context.Context,
	input *awsguardduty.GetDetectorInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.GetDetectorOutput, error) {
	f.calls = append(f.calls, "GetDetector")
	return f.detectors[awsv2.ToString(input.DetectorId)], nil
}

func (f *fakeGuardDutyAPI) ListMembers(
	_ context.Context,
	input *awsguardduty.ListMembersInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListMembersOutput, error) {
	f.calls = append(f.calls, "ListMembers")
	return f.nextMemberPage(awsv2.ToString(input.DetectorId)), nil
}

func (f *fakeGuardDutyAPI) ListFilters(
	_ context.Context,
	input *awsguardduty.ListFiltersInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListFiltersOutput, error) {
	f.calls = append(f.calls, "ListFilters")
	return f.nextFilterPage(awsv2.ToString(input.DetectorId)), nil
}

func (f *fakeGuardDutyAPI) ListPublishingDestinations(
	_ context.Context,
	input *awsguardduty.ListPublishingDestinationsInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListPublishingDestinationsOutput, error) {
	f.calls = append(f.calls, "ListPublishingDestinations")
	return f.nextPublishingPage(awsv2.ToString(input.DetectorId)), nil
}

func (f *fakeGuardDutyAPI) DescribePublishingDestination(
	_ context.Context,
	input *awsguardduty.DescribePublishingDestinationInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.DescribePublishingDestinationOutput, error) {
	f.calls = append(f.calls, "DescribePublishingDestination")
	return f.publishingDetails[awsv2.ToString(input.DetectorId)+"/"+awsv2.ToString(input.DestinationId)], nil
}

func (f *fakeGuardDutyAPI) ListThreatIntelSets(
	_ context.Context,
	input *awsguardduty.ListThreatIntelSetsInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListThreatIntelSetsOutput, error) {
	f.calls = append(f.calls, "ListThreatIntelSets")
	return f.nextThreatSetPage(awsv2.ToString(input.DetectorId)), nil
}

func (f *fakeGuardDutyAPI) GetThreatIntelSet(
	_ context.Context,
	input *awsguardduty.GetThreatIntelSetInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.GetThreatIntelSetOutput, error) {
	f.calls = append(f.calls, "GetThreatIntelSet")
	return f.threatSetDetails[awsv2.ToString(input.DetectorId)+"/"+awsv2.ToString(input.ThreatIntelSetId)], nil
}

func (f *fakeGuardDutyAPI) ListIPSets(
	_ context.Context,
	input *awsguardduty.ListIPSetsInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.ListIPSetsOutput, error) {
	f.calls = append(f.calls, "ListIPSets")
	return f.nextIPSetPage(awsv2.ToString(input.DetectorId)), nil
}

func (f *fakeGuardDutyAPI) GetIPSet(
	_ context.Context,
	input *awsguardduty.GetIPSetInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.GetIPSetOutput, error) {
	f.calls = append(f.calls, "GetIPSet")
	return f.ipSetDetails[awsv2.ToString(input.DetectorId)+"/"+awsv2.ToString(input.IpSetId)], nil
}

func (f *fakeGuardDutyAPI) GetFindingsStatistics(
	_ context.Context,
	input *awsguardduty.GetFindingsStatisticsInput,
	_ ...func(*awsguardduty.Options),
) (*awsguardduty.GetFindingsStatisticsOutput, error) {
	f.calls = append(f.calls, "GetFindingsStatistics")
	return f.statistics[input.GroupBy], nil
}

func (f *fakeGuardDutyAPI) GetFindings(
	context.Context,
	*awsguardduty.GetFindingsInput,
	...func(*awsguardduty.Options),
) (*awsguardduty.GetFindingsOutput, error) {
	f.calls = append(f.calls, "GetFindings")
	return &awsguardduty.GetFindingsOutput{}, nil
}

func (f *fakeGuardDutyAPI) ListFindings(
	context.Context,
	*awsguardduty.ListFindingsInput,
	...func(*awsguardduty.Options),
) (*awsguardduty.ListFindingsOutput, error) {
	f.calls = append(f.calls, "ListFindings")
	return &awsguardduty.ListFindingsOutput{}, nil
}

func (f *fakeGuardDutyAPI) GetFilter(
	context.Context,
	*awsguardduty.GetFilterInput,
	...func(*awsguardduty.Options),
) (*awsguardduty.GetFilterOutput, error) {
	f.calls = append(f.calls, "GetFilter")
	return &awsguardduty.GetFilterOutput{}, nil
}

func (f *fakeGuardDutyAPI) nextMemberPage(detectorID string) *awsguardduty.ListMembersOutput {
	return nextMapPage(f.members, f.memberCalls, detectorID, &awsguardduty.ListMembersOutput{})
}

func (f *fakeGuardDutyAPI) nextFilterPage(detectorID string) *awsguardduty.ListFiltersOutput {
	return nextMapPage(f.filters, f.filterCalls, detectorID, &awsguardduty.ListFiltersOutput{})
}

func (f *fakeGuardDutyAPI) nextPublishingPage(detectorID string) *awsguardduty.ListPublishingDestinationsOutput {
	return nextMapPage(f.publishing, f.publishingCalls, detectorID, &awsguardduty.ListPublishingDestinationsOutput{})
}

func (f *fakeGuardDutyAPI) nextThreatSetPage(detectorID string) *awsguardduty.ListThreatIntelSetsOutput {
	return nextMapPage(f.threatSets, f.threatSetCalls, detectorID, &awsguardduty.ListThreatIntelSetsOutput{})
}

func (f *fakeGuardDutyAPI) nextIPSetPage(detectorID string) *awsguardduty.ListIPSetsOutput {
	return nextMapPage(f.ipSets, f.ipSetCalls, detectorID, &awsguardduty.ListIPSetsOutput{})
}

func nextPage[T any](pages []*T, calls *int) *T {
	if *calls >= len(pages) {
		return nil
	}
	page := pages[*calls]
	*calls = *calls + 1
	return page
}

func nextMapPage[T any](pages map[string][]*T, calls map[string]int, key string, empty *T) *T {
	if calls == nil {
		calls = make(map[string]int)
	}
	index := calls[key]
	calls[key] = index + 1
	if index >= len(pages[key]) {
		return empty
	}
	return pages[key][index]
}
