// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appstream

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon AppStream 2.0 fleet, stack, image
// builder, and image observations for one AWS claim. Implementations read
// control-plane metadata through the AppStream describe/list management APIs and
// never read streaming sessions, user data, session scripts, or any mutation
// surface.
type Client interface {
	// Snapshot returns every AppStream fleet, stack, image builder, and image
	// visible to the configured AWS credentials, along with the reported
	// fleet-to-stack associations.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AppStream control-plane metadata plus non-fatal scan
// warnings for one boundary.
type Snapshot struct {
	// Fleets is the metadata-only set of AppStream fleets.
	Fleets []Fleet
	// Stacks is the metadata-only set of AppStream stacks.
	Stacks []Stack
	// ImageBuilders is the metadata-only set of AppStream image builders.
	ImageBuilders []ImageBuilder
	// Images is the metadata-only set of AppStream images (identity, state, and
	// visibility only).
	Images []Image
	// FleetStackAssociations maps each fleet name to the names of the stacks it is
	// associated with, as reported by ListAssociatedStacks.
	FleetStackAssociations []FleetStackAssociation
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// FleetStackAssociation records that a fleet is associated with a stack, keyed
// by the fleet and stack names AppStream's association APIs report.
type FleetStackAssociation struct {
	// FleetName is the AppStream fleet name.
	FleetName string
	// StackName is the AppStream stack name associated with the fleet.
	StackName string
}

// Fleet is the scanner-owned AppStream fleet model. It carries control-plane
// metadata only and intentionally excludes streaming session, user, and
// session-script contents.
type Fleet struct {
	// ARN is the Amazon Resource Name that uniquely identifies the fleet.
	ARN string
	// Name is the AppStream fleet name.
	Name string
	// DisplayName is the human-friendly fleet name shown to administrators.
	DisplayName string
	// Description is the administrator-supplied fleet description.
	Description string
	// State is the current fleet lifecycle state (for example RUNNING).
	State string
	// FleetType is the fleet billing/availability type (ALWAYS_ON or ON_DEMAND).
	FleetType string
	// InstanceType is the streaming instance type backing the fleet.
	InstanceType string
	// Platform is the operating-system platform of the fleet.
	Platform string
	// StreamView is the AppStream view (APP or DESKTOP) presented to users.
	StreamView string
	// IAMRoleARN is the ARN of the IAM role applied to fleet instances, when set.
	IAMRoleARN string
	// ImageARN is the ARN of the image used to launch the fleet, when set.
	ImageARN string
	// ImageName is the name of the image used to launch the fleet, when set.
	ImageName string
	// EnableDefaultInternetAccess reports whether default internet access is on.
	EnableDefaultInternetAccess bool
	// MaxConcurrentSessions is the maximum concurrent sessions for the fleet.
	MaxConcurrentSessions int32
	// MaxUserDurationInSeconds is the maximum streaming session duration.
	MaxUserDurationInSeconds int32
	// CreatedTime is when the fleet was created.
	CreatedTime time.Time
	// SubnetIDs are the bare VPC subnet ids (subnet-...) attached to the fleet.
	SubnetIDs []string
	// SecurityGroupIDs are the bare VPC security group ids (sg-...) on the fleet.
	SecurityGroupIDs []string
	// Tags carries the fleet resource tags.
	Tags map[string]string
}

// Stack is the scanner-owned AppStream stack model. It carries control-plane
// metadata only and intentionally excludes user settings detail, redirect
// secrets, and embedded-session domains beyond the S3 bucket dependencies.
type Stack struct {
	// ARN is the Amazon Resource Name that uniquely identifies the stack.
	ARN string
	// Name is the AppStream stack name.
	Name string
	// DisplayName is the human-friendly stack name shown to administrators.
	DisplayName string
	// Description is the administrator-supplied stack description.
	Description string
	// ApplicationSettingsEnabled reports whether persistent application settings
	// are enabled for users of the stack.
	ApplicationSettingsEnabled bool
	// ApplicationSettingsS3Bucket is the S3 bucket NAME AppStream reports for
	// persistent application settings storage, when enabled. It is a bucket name,
	// not an ARN.
	ApplicationSettingsS3Bucket string
	// StorageConnectorBuckets are the S3 bucket NAMES AppStream reports for
	// home-folders storage connectors, when configured. They are bucket names,
	// not ARNs.
	StorageConnectorBuckets []string
	// CreatedTime is when the stack was created.
	CreatedTime time.Time
	// Tags carries the stack resource tags.
	Tags map[string]string
}

// ImageBuilder is the scanner-owned AppStream image builder model. It carries
// control-plane metadata only.
type ImageBuilder struct {
	// ARN is the Amazon Resource Name that uniquely identifies the image builder.
	ARN string
	// Name is the AppStream image builder name.
	Name string
	// DisplayName is the human-friendly image builder name.
	DisplayName string
	// Description is the administrator-supplied image builder description.
	Description string
	// State is the current image builder lifecycle state.
	State string
	// InstanceType is the streaming instance type backing the image builder.
	InstanceType string
	// Platform is the operating-system platform of the image builder.
	Platform string
	// IAMRoleARN is the ARN of the IAM role applied to the image builder, when set.
	IAMRoleARN string
	// ImageARN is the ARN of the base image the builder was created from, when set.
	ImageARN string
	// EnableDefaultInternetAccess reports whether default internet access is on.
	EnableDefaultInternetAccess bool
	// CreatedTime is when the image builder was created.
	CreatedTime time.Time
	// SubnetIDs are the bare VPC subnet ids (subnet-...) attached to the builder.
	SubnetIDs []string
	// SecurityGroupIDs are the bare VPC security group ids (sg-...) on the builder.
	SecurityGroupIDs []string
	// Tags carries the image builder resource tags.
	Tags map[string]string
}

// Image is the scanner-owned AppStream image model. It carries identity, state,
// and visibility metadata only; installed applications, image-permission
// grants, and agent contents stay outside the scanner contract.
type Image struct {
	// ARN is the Amazon Resource Name that uniquely identifies the image.
	ARN string
	// Name is the AppStream image name.
	Name string
	// DisplayName is the human-friendly image name.
	DisplayName string
	// State is the current image lifecycle state (for example AVAILABLE).
	State string
	// Visibility is whether the image is PRIVATE, PUBLIC, or SHARED.
	Visibility string
	// ImageType is the AppStream image type (for example custom or native).
	ImageType string
	// Platform is the operating-system platform of the image.
	Platform string
	// BaseImageARN is the ARN of the image this image was created from, when set.
	BaseImageARN string
	// CreatedTime is when the image was created.
	CreatedTime time.Time
	// Tags carries the image resource tags.
	Tags map[string]string
}
