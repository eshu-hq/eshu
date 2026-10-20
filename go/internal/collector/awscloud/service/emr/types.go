// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"context"
	"time"
)

// Client is the EMR read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses for EMR on EC2, EMR Serverless, and EMR Studio
// into these scanner-owned metadata types. The interface exposes only
// List/Describe/Get-class reads; it carries no job, step, cluster, application,
// or studio mutation method, and no reader for step command lines, bootstrap
// action bodies, security configuration policy bodies, or Serverless job-run
// entry-point arguments.
type Client interface {
	// ListClusters returns running and recently terminated EMR on EC2
	// clusters, each with its uniform instance groups or instance fleets.
	ListClusters(context.Context) ([]Cluster, error)
	// ListSecurityConfigurations returns EMR security configuration metadata
	// (name and creation time only; never the policy body).
	ListSecurityConfigurations(context.Context) ([]SecurityConfiguration, error)
	// ListServerlessApplications returns EMR Serverless applications.
	ListServerlessApplications(context.Context) ([]ServerlessApplication, error)
	// ListStudios returns EMR Studios, each with its session mappings.
	ListStudios(context.Context) ([]Studio, error)
}

// Cluster is the scanner-owned representation of an EMR on EC2 cluster. It
// carries inventory and networking metadata only. Step command lines,
// bootstrap action script bodies, and configuration property values are never
// part of this type.
type Cluster struct {
	ARN                  string
	ID                   string
	Name                 string
	State                string
	ReleaseLabel         string
	Applications         []string
	ServiceRole          string
	AutoScalingRole      string
	InstanceProfile      string
	SecurityConfigName   string
	LogEncryptionKMSKey  string
	LogURI               string
	MasterPublicDNSName  string
	ScaleDownBehavior    string
	AutoTerminate        bool
	TerminationProtected bool
	VisibleToAllUsers    bool
	InstanceCollection   string
	SubnetID             string
	RequestedSubnetIDs   []string
	SecurityGroupIDs     []string
	AvailabilityZone     string
	CreatedAt            time.Time
	ReadyAt              time.Time
	EndedAt              time.Time
	Tags                 map[string]string
	InstanceGroups       []InstanceGroup
	InstanceFleets       []InstanceFleet
}

// InstanceGroup is the scanner-owned representation of an EMR uniform instance
// group within a cluster.
type InstanceGroup struct {
	ID            string
	Name          string
	GroupType     string
	InstanceType  string
	Market        string
	State         string
	RequestedSize int32
	RunningSize   int32
}

// InstanceFleet is the scanner-owned representation of an EMR instance fleet
// within a cluster.
type InstanceFleet struct {
	ID                     string
	Name                   string
	FleetType              string
	State                  string
	TargetOnDemandCapacity int32
	TargetSpotCapacity     int32
	ProvisionedOnDemand    int32
	ProvisionedSpot        int32
	InstanceTypeSpecs      []string
}

// SecurityConfiguration is the scanner-owned representation of an EMR security
// configuration. It is name-only metadata: the encryption and authentication
// policy JSON body is intentionally outside this type and is never fetched.
type SecurityConfiguration struct {
	Name      string
	CreatedAt time.Time
}

// ServerlessApplication is the scanner-owned representation of an EMR
// Serverless application. Job-run details, including SparkSubmit entry-point
// arguments, are intentionally outside this type.
type ServerlessApplication struct {
	ARN              string
	ID               string
	Name             string
	State            string
	ReleaseLabel     string
	Type             string
	Architecture     string
	ImageURI         string
	DiskEncryptKMS   string
	SubnetIDs        []string
	SecurityGroupIDs []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Tags             map[string]string
}

// Studio is the scanner-owned representation of an EMR Studio, including its
// session mappings.
type Studio struct {
	ARN               string
	ID                string
	Name              string
	AuthMode          string
	VPCID             string
	SubnetIDs         []string
	EngineSecGroupID  string
	WorkspaceSecGroup string
	ServiceRole       string
	UserRole          string
	EncryptionKeyARN  string
	URL               string
	DefaultS3Location string
	CreatedAt         time.Time
	Tags              map[string]string
	SessionMappings   []StudioSessionMapping
}

// StudioSessionMapping is the scanner-owned representation of an EMR Studio
// session mapping. SessionPolicyARN is a reference to a managed policy, not the
// policy body. Only CreatedAt is recorded: ListStudioSessionMappings returns
// SessionMappingSummary, which carries CreationTime but no last-modified time.
// The AWS SDK exposes LastModifiedTime only through the per-mapping
// GetStudioSessionMapping detail call, which the metadata-only scanner does not
// make, so no last-modified field is tracked or emitted.
type StudioSessionMapping struct {
	StudioID         string
	IdentityID       string
	IdentityName     string
	IdentityType     string
	SessionPolicyARN string
	CreatedAt        time.Time
}
