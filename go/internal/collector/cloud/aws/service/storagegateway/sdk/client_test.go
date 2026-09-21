// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssg "github.com/aws/aws-sdk-go-v2/service/storagegateway"
	sgtypes "github.com/aws/aws-sdk-go-v2/service/storagegateway/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// TestAPIClientInterfaceExcludesMutationAndCacheOperations is the security gate
// for the Storage Gateway SDK adapter. The scanner contract is metadata-only
// and read-only. The adapter's internal apiClient interface must never expose a
// gateway lifecycle mutation (Activate/Delete/Shutdown/Start/Reboot/Update), a
// file-share or volume mutation (Create/Delete), a cache-refresh, a tape
// operation, a credential/SMB-password read, or any other write. This test
// reflects over the interface and FAILS on any forbidden method, using
// substring matches so a future addition like RefreshCache or CreateNFSFileShare
// cannot slip past.
func TestAPIClientInterfaceExcludesMutationAndCacheOperations(t *testing.T) {
	forbidden := []string{
		// Gateway lifecycle mutations.
		"Activate", "Delete", "Shutdown", "Start", "Reboot", "Update",
		"Disable", "Join", "Associate", "Disassociate",
		// File share / volume mutations.
		"Create", "Refresh", "Reset", "Assign", "Add", "Remove",
		"Attach", "Detach", "Cancel", "Retrieve",
		// Tape, credential, and notification surfaces.
		"Tape", "Cache", "Bandwidth", "Notify", "SetLocalConsolePassword",
		"SetSMBGuestPassword", "Tag", "Untag", "Put",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface exposes no methods; expected the read-only Storage Gateway surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		for _, banned := range forbidden {
			if strings.Contains(method.Name, banned) {
				t.Fatalf("apiClient exposes method %q containing forbidden operation %q; Storage Gateway adapter is metadata-only and read-only", method.Name, banned)
			}
		}
	}
}

func TestClientReadsStorageGatewayMetadata(t *testing.T) {
	gatewayARN := "arn:aws:storagegateway:us-east-1:123456789012:gateway/sgw-1"
	api := &fakeAPI{
		gateways: &awssg.ListGatewaysOutput{Gateways: []sgtypes.GatewayInfo{{
			GatewayARN:              awsv2.String(gatewayARN),
			GatewayId:               awsv2.String("sgw-1"),
			GatewayName:             awsv2.String("file-gw"),
			GatewayType:             awsv2.String("FILE_S3"),
			GatewayOperationalState: awsv2.String("ACTIVE"),
		}}},
		gatewayInfo: &awssg.DescribeGatewayInformationOutput{
			GatewayARN:            awsv2.String(gatewayARN),
			GatewayState:          awsv2.String("RUNNING"),
			EndpointType:          awsv2.String("STANDARD"),
			VPCEndpoint:           awsv2.String("vpce-abc123"),
			CloudWatchLogGroupARN: awsv2.String("arn:aws:logs:us-east-1:123456789012:log-group:/g:*"),
			HostEnvironment:       sgtypes.HostEnvironmentEc2,
			GatewayNetworkInterfaces: []sgtypes.NetworkInterface{
				{Ipv4Address: awsv2.String("10.0.0.5")},
			},
			Tags: []sgtypes.Tag{{Key: awsv2.String("env"), Value: awsv2.String("prod")}},
		},
		volumes: &awssg.ListVolumesOutput{VolumeInfos: []sgtypes.VolumeInfo{{
			VolumeARN:         awsv2.String(gatewayARN + "/volume/vol-1"),
			VolumeId:          awsv2.String("vol-1"),
			VolumeType:        awsv2.String("CACHED"),
			VolumeSizeInBytes: 100,
			GatewayARN:        awsv2.String(gatewayARN),
		}}},
		fileShares: &awssg.ListFileSharesOutput{FileShareInfoList: []sgtypes.FileShareInfo{
			{FileShareARN: awsv2.String("arn:aws:storagegateway:us-east-1:123456789012:share/nfs-1"), FileShareType: sgtypes.FileShareTypeNfs},
			{FileShareARN: awsv2.String("arn:aws:storagegateway:us-east-1:123456789012:share/smb-1"), FileShareType: sgtypes.FileShareTypeSmb},
		}},
		nfs: &awssg.DescribeNFSFileSharesOutput{NFSFileShareInfoList: []sgtypes.NFSFileShareInfo{{
			FileShareARN:        awsv2.String("arn:aws:storagegateway:us-east-1:123456789012:share/nfs-1"),
			FileShareId:         awsv2.String("nfs-1"),
			FileShareStatus:     awsv2.String("AVAILABLE"),
			GatewayARN:          awsv2.String(gatewayARN),
			LocationARN:         awsv2.String("arn:aws:s3:::archive/data/"),
			Role:                awsv2.String("arn:aws:iam::123456789012:role/r"),
			KMSKey:              awsv2.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
			AuditDestinationARN: awsv2.String("arn:aws:logs:us-east-1:123456789012:log-group:/g:*"),
			ClientList:          []string{"10.0.0.0/24"},
		}}},
		smb: &awssg.DescribeSMBFileSharesOutput{SMBFileShareInfoList: []sgtypes.SMBFileShareInfo{{
			FileShareARN:    awsv2.String("arn:aws:storagegateway:us-east-1:123456789012:share/smb-1"),
			FileShareId:     awsv2.String("smb-1"),
			FileShareStatus: awsv2.String("AVAILABLE"),
			GatewayARN:      awsv2.String(gatewayARN),
			LocationARN:     awsv2.String("arn:aws:s3:::smb-archive"),
			AdminUserList:   []string{"admin"},
		}}},
	}
	client := &Client{client: api, boundary: aws.Boundary{ServiceKind: aws.ServiceStorageGateway, Region: "us-east-1"}}

	gateways, err := client.ListGateways(context.Background())
	if err != nil {
		t.Fatalf("ListGateways() error = %v", err)
	}
	if len(gateways) != 1 {
		t.Fatalf("ListGateways() len = %d, want 1", len(gateways))
	}
	gateway := gateways[0]
	if gateway.EndpointType != "STANDARD" {
		t.Fatalf("gateway EndpointType = %q, want STANDARD", gateway.EndpointType)
	}
	if gateway.VPCEndpoint != "vpce-abc123" {
		t.Fatalf("gateway VPCEndpoint = %q, want vpce-abc123", gateway.VPCEndpoint)
	}
	if gateway.NetworkInterfaceCount != 1 {
		t.Fatalf("gateway NetworkInterfaceCount = %d, want 1 (IPs must be reduced to a count)", gateway.NetworkInterfaceCount)
	}
	if gateway.State != "RUNNING" {
		t.Fatalf("gateway State = %q, want RUNNING", gateway.State)
	}

	volumes, err := client.ListVolumes(context.Background())
	if err != nil {
		t.Fatalf("ListVolumes() error = %v", err)
	}
	if len(volumes) != 1 || volumes[0].Type != "CACHED" {
		t.Fatalf("ListVolumes() = %#v, want one CACHED volume", volumes)
	}

	shares, err := client.ListFileShares(context.Background())
	if err != nil {
		t.Fatalf("ListFileShares() error = %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("ListFileShares() len = %d, want 2", len(shares))
	}
	var sawNFS, sawSMB bool
	for _, share := range shares {
		switch share.Protocol {
		case "NFS":
			sawNFS = true
			if share.LocationARN != "arn:aws:s3:::archive/data/" {
				t.Fatalf("NFS LocationARN = %q", share.LocationARN)
			}
		case "SMB":
			sawSMB = true
		}
	}
	if !sawNFS || !sawSMB {
		t.Fatalf("ListFileShares() missing NFS or SMB share: nfs=%v smb=%v", sawNFS, sawSMB)
	}
}

type fakeAPI struct {
	gateways    *awssg.ListGatewaysOutput
	gatewayInfo *awssg.DescribeGatewayInformationOutput
	volumes     *awssg.ListVolumesOutput
	fileShares  *awssg.ListFileSharesOutput
	nfs         *awssg.DescribeNFSFileSharesOutput
	smb         *awssg.DescribeSMBFileSharesOutput
}

func (f *fakeAPI) ListGateways(context.Context, *awssg.ListGatewaysInput, ...func(*awssg.Options)) (*awssg.ListGatewaysOutput, error) {
	return f.gateways, nil
}

func (f *fakeAPI) DescribeGatewayInformation(context.Context, *awssg.DescribeGatewayInformationInput, ...func(*awssg.Options)) (*awssg.DescribeGatewayInformationOutput, error) {
	return f.gatewayInfo, nil
}

func (f *fakeAPI) ListVolumes(context.Context, *awssg.ListVolumesInput, ...func(*awssg.Options)) (*awssg.ListVolumesOutput, error) {
	return f.volumes, nil
}

func (f *fakeAPI) ListFileShares(context.Context, *awssg.ListFileSharesInput, ...func(*awssg.Options)) (*awssg.ListFileSharesOutput, error) {
	return f.fileShares, nil
}

func (f *fakeAPI) DescribeNFSFileShares(context.Context, *awssg.DescribeNFSFileSharesInput, ...func(*awssg.Options)) (*awssg.DescribeNFSFileSharesOutput, error) {
	return f.nfs, nil
}

func (f *fakeAPI) DescribeSMBFileShares(context.Context, *awssg.DescribeSMBFileSharesInput, ...func(*awssg.Options)) (*awssg.DescribeSMBFileSharesOutput, error) {
	return f.smb, nil
}
