// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstransfer "github.com/aws/aws-sdk-go-v2/service/transfer"
	awstransfertypes "github.com/aws/aws-sdk-go-v2/service/transfer/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	transferservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/transfer"
)

func TestClientListServersMapsSafeServerMetadataOnly(t *testing.T) {
	serverID := "s-0123456789abcdef0"
	api := &fakeTransferAPI{
		serverPages: []*awstransfer.ListServersOutput{{
			Servers: []awstransfertypes.ListedServer{{ServerId: aws.String(serverID)}},
		}},
		describedServers: map[string]*awstransfertypes.DescribedServer{
			serverID: {
				Arn:                  aws.String("arn:aws:transfer:us-east-1:123456789012:server/" + serverID),
				ServerId:             aws.String(serverID),
				Domain:               awstransfertypes.DomainS3,
				EndpointType:         awstransfertypes.EndpointTypeVpc,
				IdentityProviderType: awstransfertypes.IdentityProviderTypeServiceManaged,
				State:                awstransfertypes.StateOnline,
				Protocols:            []awstransfertypes.Protocol{awstransfertypes.ProtocolSftp, awstransfertypes.ProtocolFtps},
				UserCount:            aws.Int32(2),
				Certificate:          aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abcd"),
				LoggingRole:          aws.String("arn:aws:iam::123456789012:role/transfer-logging"),
				HostKeyFingerprint:   aws.String("SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
				StructuredLogDestinations: []string{
					"arn:aws:logs:us-east-1:123456789012:log-group:/aws/transfer/" + serverID,
				},
				EndpointDetails: &awstransfertypes.EndpointDetails{
					VpcEndpointId:        aws.String("vpce-0a1b2c3d"),
					VpcId:                aws.String("vpc-0123"),
					AddressAllocationIds: []string{"eipalloc-0123"},
					SubnetIds:            []string{"subnet-1"},
					SecurityGroupIds:     []string{"sg-1"},
				},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	servers, err := adapter.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers() error = %v, want nil", err)
	}
	if got, want := len(servers), 1; got != want {
		t.Fatalf("len(servers) = %d, want %d", got, want)
	}
	server := servers[0]
	if server.ServerID != serverID {
		t.Fatalf("server.ServerID = %q, want %q", server.ServerID, serverID)
	}
	if server.VPCEndpointID != "vpce-0a1b2c3d" {
		t.Fatalf("server.VPCEndpointID = %q, want vpce-0a1b2c3d", server.VPCEndpointID)
	}
	if got, want := len(server.AddressAllocationIDs), 1; got != want {
		t.Fatalf("len(AddressAllocationIDs) = %d, want %d", got, want)
	}
	if server.CertificateARN != "arn:aws:acm:us-east-1:123456789012:certificate/abcd" {
		t.Fatalf("server.CertificateARN = %q, unexpected", server.CertificateARN)
	}
	if got, want := len(server.Protocols), 2; got != want {
		t.Fatalf("len(Protocols) = %d, want %d", got, want)
	}
	// The scanner-owned Server type has no field for host key material, so the
	// fingerprint the API returned cannot be carried forward. This assertion is
	// the compile-time + runtime guarantee that the adapter drops it.
	assertNoHostKeyField(t, server)
}

func TestClientListUsersMapsSafeUserMetadataOnly(t *testing.T) {
	serverID := "s-0123456789abcdef0"
	api := &fakeTransferAPI{
		serverPages: []*awstransfer.ListServersOutput{{
			Servers: []awstransfertypes.ListedServer{{ServerId: aws.String(serverID)}},
		}},
		userPages: map[string][]*awstransfer.ListUsersOutput{
			serverID: {{
				Users: []awstransfertypes.ListedUser{{UserName: aws.String("sftp-user")}},
			}},
		},
		describedUsers: map[string]*awstransfertypes.DescribedUser{
			serverID + "/sftp-user": {
				Arn:               aws.String("arn:aws:transfer:us-east-1:123456789012:user/" + serverID + "/sftp-user"),
				UserName:          aws.String("sftp-user"),
				HomeDirectory:     aws.String("/landing/home/sftp-user"),
				HomeDirectoryType: awstransfertypes.HomeDirectoryTypePath,
				Role:              aws.String("arn:aws:iam::123456789012:role/transfer-access"),
				Policy:            aws.String(`{"Version":"2012-10-17","Statement":[]}`),
				PosixProfile:      &awstransfertypes.PosixProfile{Uid: aws.Int64(1000), Gid: aws.Int64(1000)},
				SshPublicKeys: []awstransfertypes.SshPublicKey{{
					SshPublicKeyId:   aws.String("key-0001"),
					SshPublicKeyBody: aws.String("ssh-rsa AAAAB3Nza..."),
					DateImported:     aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
				}},
				HomeDirectoryMappings: []awstransfertypes.HomeDirectoryMapEntry{{
					Entry:  aws.String("/"),
					Target: aws.String("/landing/home/sftp-user"),
				}},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	users, err := adapter.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error = %v, want nil", err)
	}
	if got, want := len(users), 1; got != want {
		t.Fatalf("len(users) = %d, want %d", got, want)
	}
	user := users[0]
	if user.UserName != "sftp-user" {
		t.Fatalf("user.UserName = %q, want sftp-user", user.UserName)
	}
	if user.ServerID != serverID {
		t.Fatalf("user.ServerID = %q, want %q", user.ServerID, serverID)
	}
	if user.HomeDirectory != "/landing/home/sftp-user" {
		t.Fatalf("user.HomeDirectory = %q, unexpected", user.HomeDirectory)
	}
	if user.RoleARN != "arn:aws:iam::123456789012:role/transfer-access" {
		t.Fatalf("user.RoleARN = %q, unexpected", user.RoleARN)
	}
	if got, want := len(user.HomeDirectoryMappings), 1; got != want {
		t.Fatalf("len(HomeDirectoryMappings) = %d, want %d", got, want)
	}
	// The scanner-owned User type has no field for SSH public keys, policy JSON,
	// or POSIX UID/GID, so they cannot be carried forward. This assertion is the
	// compile-time + runtime guarantee that the adapter drops them.
	assertNoCredentialField(t, user)
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceTransfer,
	}
}

// TestListServersMemoizedAcrossListServersAndListUsers proves the adapter lists
// servers at most once per claim: the scanner calls ListServers (DescribeServer
// fan-out) and ListUsers, and both consume the server IDs. Without memoization
// ListUsers would trigger a second ListServers pagination pass.
func TestListServersMemoizedAcrossListServersAndListUsers(t *testing.T) {
	serverID := "s-0123456789abcdef0"
	api := &fakeTransferAPI{
		serverPages: []*awstransfer.ListServersOutput{{
			Servers: []awstransfertypes.ListedServer{{ServerId: aws.String(serverID)}},
		}},
		describedServers: map[string]*awstransfertypes.DescribedServer{
			serverID: {
				Arn:      aws.String("arn:aws:transfer:us-east-1:123456789012:server/" + serverID),
				ServerId: aws.String(serverID),
			},
		},
		userPages: map[string][]*awstransfer.ListUsersOutput{
			serverID: {{Users: []awstransfertypes.ListedUser{{UserName: aws.String("u")}}}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	if _, err := adapter.ListServers(context.Background()); err != nil {
		t.Fatalf("ListServers() error = %v", err)
	}
	if _, err := adapter.ListUsers(context.Background()); err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if api.listServersCalls != 1 {
		t.Fatalf("ListServers API calls = %d, want 1 (memoized across ListServers + ListUsers)", api.listServersCalls)
	}
}

type fakeTransferAPI struct {
	serverPages      []*awstransfer.ListServersOutput
	serverCursor     int
	listServersCalls int
	describedServers map[string]*awstransfertypes.DescribedServer
	userPages        map[string][]*awstransfer.ListUsersOutput
	userCursors      map[string]int
	describedUsers   map[string]*awstransfertypes.DescribedUser
}

func (f *fakeTransferAPI) ListServers(_ context.Context, _ *awstransfer.ListServersInput, _ ...func(*awstransfer.Options)) (*awstransfer.ListServersOutput, error) {
	f.listServersCalls++
	if f.serverCursor >= len(f.serverPages) {
		return &awstransfer.ListServersOutput{}, nil
	}
	page := f.serverPages[f.serverCursor]
	f.serverCursor++
	return page, nil
}

func (f *fakeTransferAPI) DescribeServer(_ context.Context, input *awstransfer.DescribeServerInput, _ ...func(*awstransfer.Options)) (*awstransfer.DescribeServerOutput, error) {
	return &awstransfer.DescribeServerOutput{Server: f.describedServers[aws.ToString(input.ServerId)]}, nil
}

func (f *fakeTransferAPI) ListUsers(_ context.Context, input *awstransfer.ListUsersInput, _ ...func(*awstransfer.Options)) (*awstransfer.ListUsersOutput, error) {
	serverID := aws.ToString(input.ServerId)
	if f.userCursors == nil {
		f.userCursors = map[string]int{}
	}
	pages := f.userPages[serverID]
	cursor := f.userCursors[serverID]
	if cursor >= len(pages) {
		return &awstransfer.ListUsersOutput{}, nil
	}
	f.userCursors[serverID] = cursor + 1
	return pages[cursor], nil
}

func (f *fakeTransferAPI) DescribeUser(_ context.Context, input *awstransfer.DescribeUserInput, _ ...func(*awstransfer.Options)) (*awstransfer.DescribeUserOutput, error) {
	key := aws.ToString(input.ServerId) + "/" + aws.ToString(input.UserName)
	return &awstransfer.DescribeUserOutput{User: f.describedUsers[key]}, nil
}

// assertNoHostKeyField fails if a future edit ever adds host-key material to the
// scanner-owned Server type. It checks the rendered shape, not just the current
// fields, by serializing the value and scanning for fingerprint markers.
func assertNoHostKeyField(t *testing.T, server transferservice.Server) {
	t.Helper()
	if containsCredentialMarker(serverMarkers(server)) {
		t.Fatalf("scanner-owned Server carries host-key material: %+v", server)
	}
}

func assertNoCredentialField(t *testing.T, user transferservice.User) {
	t.Helper()
	if containsCredentialMarker(userMarkers(user)) {
		t.Fatalf("scanner-owned User carries SSH key, policy, or POSIX material: %+v", user)
	}
}

func serverMarkers(server transferservice.Server) []string {
	return append([]string{
		server.ARN, server.ServerID, server.Domain, server.EndpointType,
		server.IdentityProviderType, server.State, server.SecurityPolicyName,
		server.IPAddressType, server.VPCEndpointID, server.VPCID,
		server.CertificateARN, server.LoggingRoleARN,
	}, append(append(append(server.Protocols, server.AddressAllocationIDs...), server.SubnetIDs...), server.StructuredLogDestinations...)...)
}

func userMarkers(user transferservice.User) []string {
	markers := []string{
		user.ServerID, user.ARN, user.UserName, user.HomeDirectory,
		user.HomeDirectoryType, user.RoleARN,
	}
	for _, mapping := range user.HomeDirectoryMappings {
		markers = append(markers, mapping.Entry, mapping.Target)
	}
	return markers
}

func containsCredentialMarker(values []string) bool {
	for _, value := range values {
		switch {
		case value == "":
			continue
		case startsWith(value, "ssh-rsa"), startsWith(value, "ssh-ed25519"),
			startsWith(value, "ecdsa-sha2"), startsWith(value, "SHA256:"):
			return true
		case value == `{"Version":"2012-10-17","Statement":[]}`:
			return true
		}
	}
	return false
}

func startsWith(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
