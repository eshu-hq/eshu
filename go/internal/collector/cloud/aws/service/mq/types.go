// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

import (
	"context"
	"time"
)

// Client is the metadata-only Amazon MQ read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListBrokers(context.Context) ([]Broker, error)
	ListConfigurations(context.Context) ([]Configuration, error)
}

// Broker is the scanner-owned representation of one Amazon MQ broker. It carries
// inventory metadata only. Broker usernames are recorded for topology, but the
// User Password field is never modeled here, and queue/topic message contents
// are out of the management API and never read.
type Broker struct {
	ARN                     string
	ID                      string
	Name                    string
	EngineType              string
	EngineVersion           string
	DeploymentMode          string
	HostInstanceType        string
	State                   string
	StorageType             string
	AuthStrategy            string
	PubliclyAccessible      bool
	AutoMinorVersionUpgrade bool
	Created                 time.Time
	Tags                    map[string]string
	SubnetIDs               []string
	SecurityGroupIDs        []string
	Encryption              Encryption
	Configuration           *ConfigurationReference
	Logs                    Logs
	Usernames               []string
}

// Encryption captures Amazon MQ encryption-at-rest options without persisting
// key material. UseAWSOwnedKey distinguishes an AWS-owned CMK from a
// customer-managed key referenced by KMSKeyID.
type Encryption struct {
	UseAWSOwnedKey bool
	KMSKeyID       string
}

// ConfigurationReference identifies the broker configuration and revision
// currently applied to a broker. The configuration XML body stays out of the
// scanner contract because it can carry inline credentials and ACL rules.
type ConfigurationReference struct {
	ID       string
	Revision int32
}

// Logs captures Amazon MQ broker log enablement flags and the CloudWatch Logs
// log group names that receive general and audit logs. Log contents are never
// read; only the destination group names are recorded for topology joins.
type Logs struct {
	GeneralEnabled  bool
	GeneralLogGroup string
	AuditEnabled    bool
	AuditLogGroup   string
}

// Configuration is the scanner-owned representation of an Amazon MQ broker
// configuration. The scanner emits identity and latest-revision metadata; the
// raw configuration XML body is never persisted because it commonly contains
// inline credentials, broker ACL rules, and queue/topic names.
type Configuration struct {
	ARN            string
	ID             string
	Name           string
	Description    string
	EngineType     string
	EngineVersion  string
	AuthStrategy   string
	Created        time.Time
	Tags           map[string]string
	LatestRevision ConfigurationRevisionSummary
}

// ConfigurationRevisionSummary identifies one Amazon MQ configuration revision
// without exposing the revision body.
type ConfigurationRevisionSummary struct {
	Revision    int32
	Created     time.Time
	Description string
}
