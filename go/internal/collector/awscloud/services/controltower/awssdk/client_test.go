// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscontroltower "github.com/aws/aws-sdk-go-v2/service/controltower"
	awscontroltowerdocument "github.com/aws/aws-sdk-go-v2/service/controltower/document"
	awscontroltowertypes "github.com/aws/aws-sdk-go-v2/service/controltower/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	controltowerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/controltower"
)

const (
	lzARN   = "arn:aws:controltower:us-east-1:123456789012:landingzone/1A2B3C4D5E6F"
	ouARN   = "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform"
	ctlARN  = "arn:aws:controltower:us-east-1:123456789012:enabledcontrol/AB12CD34"
	baseARN = "arn:aws:controltower:us-east-1:123456789012:enabledbaseline/EB12CD34"
)

func TestClientSnapshotsControlTowerMetadataOnly(t *testing.T) {
	api := &fakeControlTowerAPI{
		landingZones: []awscontroltowertypes.LandingZoneSummary{{Arn: aws.String(lzARN)}},
		landingZone: &awscontroltowertypes.LandingZoneDetail{
			Arn:                    aws.String(lzARN),
			Version:                aws.String("3.3"),
			LatestAvailableVersion: aws.String("3.3"),
			Status:                 awscontroltowertypes.LandingZoneStatusActive,
			DriftStatus: &awscontroltowertypes.LandingZoneDriftStatusSummary{
				Status: awscontroltowertypes.LandingZoneDriftStatusInSync,
			},
			// The manifest body must never reach the snapshot.
			Manifest: awscontroltowerdocument.NewLazyDocument(map[string]any{
				"governedRegions":  []string{"us-east-1"},
				"accessManagement": map[string]any{"enabled": true},
			}),
		},
		baselines: []awscontroltowertypes.EnabledBaselineSummary{{
			Arn:                aws.String(baseARN),
			BaselineIdentifier: aws.String("arn:aws:controltower:us-east-1::baseline/17BSJV3IGJ2QSGA2"),
			BaselineVersion:    aws.String("4.0"),
			TargetIdentifier:   aws.String(ouARN),
			StatusSummary:      &awscontroltowertypes.EnablementStatusSummary{Status: awscontroltowertypes.EnablementStatusSucceeded},
		}},
		controlsByTarget: map[string][]awscontroltowertypes.EnabledControlSummary{
			ouARN: {{
				Arn:               aws.String(ctlARN),
				ControlIdentifier: aws.String("arn:aws:controltower:us-east-1::control/AWS-GR_ENCRYPTED_VOLUMES"),
				TargetIdentifier:  aws.String(ouARN),
				StatusSummary:     &awscontroltowertypes.EnablementStatusSummary{Status: awscontroltowertypes.EnablementStatusSucceeded},
				DriftStatusSummary: &awscontroltowertypes.DriftStatusSummary{
					DriftStatus: awscontroltowertypes.DriftStatusInSync,
				},
			}},
		},
		tags: map[string]map[string]string{
			lzARN: {"Environment": "prod"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if snapshot.LandingZone == nil {
		t.Fatalf("snapshot LandingZone = nil, want one")
	}
	lz := snapshot.LandingZone
	if lz.ARN != lzARN {
		t.Fatalf("landing zone ARN = %q, want %q", lz.ARN, lzARN)
	}
	if lz.Version != "3.3" {
		t.Fatalf("landing zone Version = %q, want 3.3", lz.Version)
	}
	if lz.Status != "ACTIVE" {
		t.Fatalf("landing zone Status = %q, want ACTIVE", lz.Status)
	}
	if lz.DriftStatus != "IN_SYNC" {
		t.Fatalf("landing zone DriftStatus = %q, want IN_SYNC", lz.DriftStatus)
	}
	if lz.Tags["Environment"] != "prod" {
		t.Fatalf("landing zone tag Environment = %q, want prod", lz.Tags["Environment"])
	}

	if len(snapshot.EnabledBaselines) != 1 {
		t.Fatalf("len(EnabledBaselines) = %d, want 1", len(snapshot.EnabledBaselines))
	}
	baseline := snapshot.EnabledBaselines[0]
	if baseline.ARN != baseARN || baseline.TargetIdentifier != ouARN {
		t.Fatalf("baseline = %#v, want ARN %q target %q", baseline, baseARN, ouARN)
	}
	if baseline.BaselineVersion != "4.0" || baseline.Status != "SUCCEEDED" {
		t.Fatalf("baseline version/status = %q/%q, want 4.0/SUCCEEDED", baseline.BaselineVersion, baseline.Status)
	}

	if len(snapshot.EnabledControls) != 1 {
		t.Fatalf("len(EnabledControls) = %d, want 1", len(snapshot.EnabledControls))
	}
	control := snapshot.EnabledControls[0]
	if control.ARN != ctlARN || control.TargetIdentifier != ouARN {
		t.Fatalf("control = %#v, want ARN %q target %q", control, ctlARN, ouARN)
	}
	if control.Status != "SUCCEEDED" || control.DriftStatus != "IN_SYNC" {
		t.Fatalf("control status/drift = %q/%q, want SUCCEEDED/IN_SYNC", control.Status, control.DriftStatus)
	}

	// Prove the snapshot carries no manifest body anywhere. The scanner-owned
	// LandingZone type has no manifest field by construction; this asserts the
	// adapter never read it through GetLandingZone into any reachable surface.
	if api.getLandingZoneCalls == 0 {
		t.Fatalf("GetLandingZone was never called; landing-zone detail not fetched")
	}
}

func TestClientReturnsNilLandingZoneWhenNoneDeployed(t *testing.T) {
	api := &fakeControlTowerAPI{}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if snapshot.LandingZone != nil {
		t.Fatalf("LandingZone = %#v, want nil for an account with no landing zone", snapshot.LandingZone)
	}
	if api.getLandingZoneCalls != 0 {
		t.Fatalf("GetLandingZone called %d times with no landing zone ARN, want 0", api.getLandingZoneCalls)
	}
}

func TestClientDeduplicatesEnabledControlsAcrossTargets(t *testing.T) {
	ou2 := "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-payments"
	api := &fakeControlTowerAPI{
		baselines: []awscontroltowertypes.EnabledBaselineSummary{
			{Arn: aws.String(baseARN), BaselineIdentifier: aws.String("b"), TargetIdentifier: aws.String(ouARN)},
			{Arn: aws.String(baseARN + "2"), BaselineIdentifier: aws.String("b"), TargetIdentifier: aws.String(ou2)},
		},
		controlsByTarget: map[string][]awscontroltowertypes.EnabledControlSummary{
			ouARN: {{Arn: aws.String(ctlARN), TargetIdentifier: aws.String(ouARN)}},
			ou2:   {{Arn: aws.String(ctlARN), TargetIdentifier: aws.String(ou2)}},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.EnabledControls) != 1 {
		t.Fatalf("len(EnabledControls) = %d, want 1 (same control ARN seen under two OUs must de-dup)", len(snapshot.EnabledControls))
	}
}

type fakeControlTowerAPI struct {
	landingZones        []awscontroltowertypes.LandingZoneSummary
	landingZone         *awscontroltowertypes.LandingZoneDetail
	getLandingZoneCalls int
	baselines           []awscontroltowertypes.EnabledBaselineSummary
	controlsByTarget    map[string][]awscontroltowertypes.EnabledControlSummary
	tags                map[string]map[string]string
}

func (f *fakeControlTowerAPI) ListLandingZones(
	_ context.Context,
	_ *awscontroltower.ListLandingZonesInput,
	_ ...func(*awscontroltower.Options),
) (*awscontroltower.ListLandingZonesOutput, error) {
	return &awscontroltower.ListLandingZonesOutput{LandingZones: f.landingZones}, nil
}

func (f *fakeControlTowerAPI) GetLandingZone(
	_ context.Context,
	_ *awscontroltower.GetLandingZoneInput,
	_ ...func(*awscontroltower.Options),
) (*awscontroltower.GetLandingZoneOutput, error) {
	f.getLandingZoneCalls++
	return &awscontroltower.GetLandingZoneOutput{LandingZone: f.landingZone}, nil
}

func (f *fakeControlTowerAPI) ListEnabledControls(
	_ context.Context,
	input *awscontroltower.ListEnabledControlsInput,
	_ ...func(*awscontroltower.Options),
) (*awscontroltower.ListEnabledControlsOutput, error) {
	return &awscontroltower.ListEnabledControlsOutput{
		EnabledControls: f.controlsByTarget[aws.ToString(input.TargetIdentifier)],
	}, nil
}

func (f *fakeControlTowerAPI) ListEnabledBaselines(
	_ context.Context,
	_ *awscontroltower.ListEnabledBaselinesInput,
	_ ...func(*awscontroltower.Options),
) (*awscontroltower.ListEnabledBaselinesOutput, error) {
	return &awscontroltower.ListEnabledBaselinesOutput{EnabledBaselines: f.baselines}, nil
}

func (f *fakeControlTowerAPI) ListTagsForResource(
	_ context.Context,
	input *awscontroltower.ListTagsForResourceInput,
	_ ...func(*awscontroltower.Options),
) (*awscontroltower.ListTagsForResourceOutput, error) {
	return &awscontroltower.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceControlTower,
	}
}

var _ controltowerservice.Client = (*Client)(nil)
