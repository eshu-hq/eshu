// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"context"
	"time"
)

// Client is the CodeBuild read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records. The
// contract lists no mutation, build data-plane, log-content, or source-
// credential call so the scanner cannot reach beyond metadata.
type Client interface {
	// ListProjects returns CodeBuild build-project metadata for the boundary.
	ListProjects(context.Context) ([]Project, error)
	// ListReportGroups returns CodeBuild report-group metadata for the boundary.
	ListReportGroups(context.Context) ([]ReportGroup, error)
	// ListRecentBuilds returns recent build metadata for the boundary.
	// Implementations bound the result to a recent window so the scanner stays
	// metadata-sized. Build logs are never read or persisted.
	ListRecentBuilds(context.Context) ([]Build, error)
}

// Project is the scanner-owned representation of one CodeBuild build project.
// It carries source, environment, artifact, VPC, and identity metadata only.
// The buildspec body is never present: see Source.Buildspec absence below.
type Project struct {
	Name               string
	ARN                string
	Description        string
	ServiceRoleARN     string
	EncryptionKeyID    string
	TimeoutInMinutes   int32
	QueuedTimeout      int32
	ConcurrentBuilds   int32
	Created            time.Time
	LastModified       time.Time
	Source             ProjectSource
	SecondarySources   []ProjectSource
	Environment        ProjectEnvironment
	Artifacts          ProjectArtifacts
	SecondaryArtifacts []ProjectArtifacts
	VPCConfig          VPCConfig
	Tags               map[string]string
}

// ProjectSource is the scanner-owned representation of one CodeBuild source.
// The buildspec body is deliberately dropped during mapping; this struct has no
// field able to hold it. SourceIdentifier names a secondary source; it is empty
// for the primary source.
type ProjectSource struct {
	Type              string
	Location          string
	SourceIdentifier  string
	ReportBuildStatus bool
}

// ProjectEnvironment is the scanner-owned representation of one CodeBuild build
// environment. EnvironmentVariables carries names and types only; PLAINTEXT
// values are redacted and reference values (PARAMETER_STORE, SECRETS_MANAGER)
// are kept as references for relationship derivation, never as secret content.
type ProjectEnvironment struct {
	Type                 string
	Image                string
	ComputeType          string
	PrivilegedMode       bool
	ImagePullCredentials string
	EnvironmentVariables []EnvironmentVariable
}

// EnvironmentVariable is the scanner-owned representation of one CodeBuild
// environment variable. For PLAINTEXT variables the raw value never reaches
// this record; ValueMarker holds the redaction marker instead. For
// PARAMETER_STORE and SECRETS_MANAGER variables Reference holds the parameter
// name or secret ARN/name AWS reports, which is a resource reference, not a
// secret value.
type EnvironmentVariable struct {
	Name        string
	Type        string
	Reference   string
	ValueMarker map[string]any
}

// ProjectArtifacts is the scanner-owned representation of one CodeBuild build
// artifact configuration. It carries the output type and S3 location reference
// only.
type ProjectArtifacts struct {
	Type               string
	Location           string
	ArtifactIdentifier string
	EncryptionDisabled bool
}

// VPCConfig captures the VPC, subnet, and security-group identifiers a build
// project runs inside.
type VPCConfig struct {
	VPCID            string
	SubnetIDs        []string
	SecurityGroupIDs []string
}

// ReportGroup is the scanner-owned representation of one CodeBuild report
// group, carrying identity, type, status, and export-destination references.
type ReportGroup struct {
	Name           string
	ARN            string
	Type           string
	Status         string
	ExportType     string
	ExportS3Bucket string
	Created        time.Time
	LastModified   time.Time
	Tags           map[string]string
}

// Build is the scanner-owned representation of one recent CodeBuild build. It
// carries identity, status, and duration metadata only. Build log group/stream
// references and log content are never present.
type Build struct {
	ID                    string
	ARN                   string
	ProjectName           string
	BuildNumber           int64
	Status                string
	CurrentPhase          string
	Initiator             string
	BuildComplete         bool
	ResolvedSourceVersion string
	StartTime             time.Time
	EndTime               time.Time
}
