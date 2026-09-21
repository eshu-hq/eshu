// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workspaces

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon WorkSpaces observations for one AWS
// claim. Implementations read control-plane describe APIs only and never read
// desktop session contents, user credentials, registration codes, or
// connection state.
type Client interface {
	// Snapshot returns the WorkSpaces, registered directories, account-owned
	// bundles, and IP access control groups visible to the configured AWS
	// credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Amazon WorkSpaces control-plane metadata plus non-fatal
// scan warnings.
type Snapshot struct {
	// Workspaces is the metadata-only set of WorkSpaces virtual desktops.
	Workspaces []Workspace
	// Directories is the metadata-only set of registered WorkSpaces directories.
	Directories []Directory
	// Bundles is the metadata-only set of account-owned WorkSpaces bundles.
	Bundles []Bundle
	// IPGroups is the metadata-only set of IP access control groups.
	IPGroups []IPGroup
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Workspace is the scanner-owned Amazon WorkSpaces virtual-desktop model. It
// carries control-plane metadata only and intentionally excludes desktop
// session contents, IP addresses, error messages with operational detail, and
// any user credential.
type Workspace struct {
	// ID is the WorkSpace identifier (for example "ws-1234567890").
	ID string
	// Name is the user-decoupled WorkSpace name, when one is set.
	Name string
	// DirectoryID is the identifier of the registered WorkSpaces directory the
	// WorkSpace belongs to.
	DirectoryID string
	// BundleID is the identifier of the bundle the WorkSpace was created from.
	BundleID string
	// State is the operational state (for example AVAILABLE or STOPPED).
	State string
	// ComputerName is the machine name as seen by the WorkSpace operating
	// system. It is identity metadata, not session content.
	ComputerName string
	// UserName is the directory user assigned to the WorkSpace. WorkSpaces
	// reports the user name as identity metadata; no credential is read.
	UserName string
	// VolumeEncryptionKey is the KMS key reference (AWS reports a key ARN) used
	// to encrypt the WorkSpace volumes, when volume encryption is enabled.
	VolumeEncryptionKey string
	// RootVolumeEncryptionEnabled reports whether the root volume is encrypted.
	RootVolumeEncryptionEnabled bool
	// UserVolumeEncryptionEnabled reports whether the user volume is encrypted.
	UserVolumeEncryptionEnabled bool
	// Tags carries the WorkSpace resource tags.
	Tags map[string]string
}

// Directory is the scanner-owned Amazon WorkSpaces registered-directory model.
// It carries the WorkSpaces-side registration metadata and the network
// placement references, and intentionally excludes the directory registration
// code and any service-account credential.
type Directory struct {
	// ID is the directory identifier (for example "d-1234567890"). It matches
	// the resource_id the Directory Service scanner publishes.
	ID string
	// Name is the directory name.
	Name string
	// Alias is the directory alias, when one is set.
	Alias string
	// State is the WorkSpaces registration state (for example REGISTERED).
	State string
	// DirectoryType is the WorkSpaces directory type (for example SIMPLE_AD,
	// AD_CONNECTOR, or CUSTOMER_MANAGED).
	DirectoryType string
	// Tenancy reports whether the directory is DEDICATED or SHARED.
	Tenancy string
	// IamRoleID is the IAM role ARN WorkSpaces assumes to call other services on
	// the account's behalf, when one is reported.
	IamRoleID string
	// WorkspaceSecurityGroupID is the bare security group id assigned to new
	// WorkSpaces in the directory, when one is reported.
	WorkspaceSecurityGroupID string
	// SubnetIDs are the bare subnet ids the directory is placed in.
	SubnetIDs []string
	// IPGroupIDs are the identifiers of the IP access control groups associated
	// with the directory.
	IPGroupIDs []string
	// Tags carries the directory resource tags.
	Tags map[string]string
}

// Bundle is the scanner-owned Amazon WorkSpaces bundle model. It carries
// control-plane metadata only.
type Bundle struct {
	// ID is the bundle identifier (for example "wsb-1234567890").
	ID string
	// Name is the bundle name.
	Name string
	// Description is the bundle description.
	Description string
	// Owner is the account identifier of the bundle owner, or "AMAZON" for an
	// Amazon-provided bundle.
	Owner string
	// BundleType is the bundle type (for example REGULAR or POOLS).
	BundleType string
	// ComputeType is the compute type name (for example STANDARD, PERFORMANCE,
	// or POWER).
	ComputeType string
	// RootVolumeSizeGib is the reported root volume capacity in GiB.
	RootVolumeSizeGib string
	// UserVolumeSizeGib is the reported user volume capacity in GiB.
	UserVolumeSizeGib string
	// ImageID is the identifier of the image the bundle was created from.
	ImageID string
	// State is the bundle state, when reported.
	State string
	// CreationTime is when the bundle was created.
	CreationTime time.Time
	// LastUpdatedTime is when the bundle was last updated.
	LastUpdatedTime time.Time
	// Tags carries the bundle resource tags.
	Tags map[string]string
}

// IPGroup is the scanner-owned Amazon WorkSpaces IP access control group model.
// It carries the access-rule CIDR configuration (network metadata) only.
type IPGroup struct {
	// ID is the IP access control group identifier (for example
	// "wsipg-1234567890").
	ID string
	// Name is the group name.
	Name string
	// Description is the group description.
	Description string
	// Rules are the CIDR access rules configured on the group.
	Rules []IPRule
	// Tags carries the IP access control group resource tags.
	Tags map[string]string
}

// IPRule is one CIDR entry in a WorkSpaces IP access control group. The CIDR is
// a network access-control configuration value, not secret material.
type IPRule struct {
	// CIDR is the IP address range in CIDR notation.
	CIDR string
	// Description is the optional rule description.
	Description string
}
