// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"context"
	"time"
)

// Client lists metadata-only AWS DataSync observations for one claimed account
// and region. It exposes only read APIs (ListTasks/DescribeTask,
// ListLocations/DescribeLocation*, ListAgents/DescribeAgent); it never exposes
// a create, start, update, or delete operation, so transfer execution and
// resource mutation are unreachable from this contract.
type Client interface {
	ListTasks(ctx context.Context) ([]Task, error)
	ListLocations(ctx context.Context) ([]Location, error)
	ListAgents(ctx context.Context) ([]Agent, error)
}

// Task is the scanner-owned DataSync transfer task view. It carries safe
// identity, the source and destination location ARNs, an optional CloudWatch
// log group ARN, the schedule expression, and status. Transferred object or
// record contents, include/exclude filter patterns, and manifest bodies stay
// outside the scanner contract.
type Task struct {
	ARN                    string
	Name                   string
	Status                 string
	SourceLocationARN      string
	DestinationLocationARN string
	CloudWatchLogGroupARN  string
	ScheduleExpression     string
	ScheduleStatus         string
	TaskMode               string
	CreationTime           time.Time
}

// Location is the scanner-owned DataSync location view. It carries the location
// ARN, the location type (S3, EFS, FSX_LUSTRE, FSX_ONTAP, FSX_OPENZFS,
// FSX_WINDOWS, NFS, SMB, OBJECT_STORAGE, HDFS, AZURE_BLOB), and a host/path-only
// URI. The scanner never persists object-storage access keys, server
// certificates, SMB/object-storage passwords, or any secret-shaped credential.
//
// The backing-resource identity fields below are derived from the location
// configuration so relationships can join the location to the storage resource
// another scanner already publishes. They are bare AWS identifiers or an ARN
// the DataSync API reports directly; the scanner synthesizes partition-aware
// ARNs from the bare identifiers, never a hardcoded `arn:aws:` prefix.
type Location struct {
	ARN  string
	Type string
	URI  string

	// S3BucketName is the bucket name parsed from an S3 location URI
	// (`s3://bucket/prefix`). Empty for non-S3 locations.
	S3BucketName string
	// EFSFileSystemID is the EFS file system id parsed from an EFS location URI
	// global id (`region.fs-xxxxxxxx`). Empty for non-EFS locations.
	EFSFileSystemID string
	// FSxFileSystemID is the FSx file system id parsed from an FSx location URI
	// global id (`fs-xxxxxxxx`). Empty for non-FSx locations or when the API
	// reports the file system ARN directly.
	FSxFileSystemID string
	// FSxFileSystemARN is the FSx file system ARN reported directly by the
	// DataSync API (FSx for NetApp ONTAP locations). Empty otherwise.
	FSxFileSystemARN string
	// IAMRoleARN is the IAM role ARN DataSync uses to access the backing AWS
	// storage (S3 bucket access role, EFS file-system access role). Empty when
	// the location has no AWS access role.
	IAMRoleARN string

	CreationTime time.Time
}

// Agent is the scanner-owned DataSync agent view. It carries the agent ARN,
// name, status, endpoint type, and platform version. Agent private-link
// endpoint network details beyond the endpoint type stay outside the contract.
type Agent struct {
	ARN             string
	Name            string
	Status          string
	EndpointType    string
	PlatformVersion string
	CreationTime    time.Time
}
