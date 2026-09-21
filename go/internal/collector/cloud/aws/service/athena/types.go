// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"context"
	"time"
)

// Client lists Amazon Athena control-plane metadata for one claimed account and
// region. Implementations must never expose query result rows, named-query SQL
// bodies, prepared-statement query strings, or query history strings.
type Client interface {
	// ListWorkGroups returns Athena workgroups with safe metadata, including any
	// configured S3 result-location and KMS key references.
	ListWorkGroups(ctx context.Context) ([]WorkGroup, error)
	// ListDataCatalogs returns Athena data catalog metadata.
	ListDataCatalogs(ctx context.Context) ([]DataCatalog, error)
	// ListPreparedStatements returns prepared-statement names for each provided
	// workgroup. The adapter must never call GetPreparedStatement so the SQL
	// body cannot enter the scanner.
	ListPreparedStatements(ctx context.Context, workGroupNames []string) ([]PreparedStatement, error)
	// ListNamedQueries returns named-query metadata for each provided workgroup.
	// The adapter calls ListNamedQueries and BatchGetNamedQuery to recover the
	// safe identity attributes (name, database, workgroup, description) and must
	// discard QueryString before returning.
	ListNamedQueries(ctx context.Context, workGroupNames []string) ([]NamedQuery, error)
}

// WorkGroup is the scanner-owned Athena workgroup metadata model. It excludes
// any query result body, query execution result location object contents, and
// per-query history strings.
type WorkGroup struct {
	Name                            string
	State                           string
	Description                     string
	CreationTime                    time.Time
	OutputLocation                  string
	EncryptionOption                string
	KMSKey                          string
	EnforceWorkGroupConfiguration   bool
	PublishCloudWatchMetricsEnabled bool
	RequesterPaysEnabled            bool
	EngineVersion                   string
	EffectiveEngineVersion          string
	BytesScannedCutoffPerQuery      int64
	ExpectedBucketOwner             string
	Tags                            map[string]string
}

// DataCatalog is the scanner-owned Athena data catalog metadata model.
type DataCatalog struct {
	Name        string
	Type        string
	Description string
	Tags        map[string]string
}

// PreparedStatement is the scanner-owned Athena prepared-statement metadata
// model. The QueryStatement field returned by GetPreparedStatement is
// intentionally omitted.
type PreparedStatement struct {
	WorkGroupName    string
	StatementName    string
	LastModifiedTime time.Time
}

// NamedQuery is the scanner-owned Athena named-query metadata model. The
// QueryString field returned by GetNamedQuery and BatchGetNamedQuery is
// intentionally omitted so SQL bodies never reach the scanner or fact store.
type NamedQuery struct {
	NamedQueryID  string
	Name          string
	Description   string
	Database      string
	WorkGroupName string
}
