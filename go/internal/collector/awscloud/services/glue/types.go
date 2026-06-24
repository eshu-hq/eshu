// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package glue

import (
	"context"
	"time"
)

// Client lists metadata-only AWS Glue observations for one claimed account and
// region.
type Client interface {
	ListDatabases(ctx context.Context) ([]Database, error)
	ListCrawlers(ctx context.Context) ([]Crawler, error)
	ListJobs(ctx context.Context) ([]Job, error)
	ListTriggers(ctx context.Context) ([]Trigger, error)
	ListWorkflows(ctx context.Context) ([]Workflow, error)
	ListConnections(ctx context.Context) ([]Connection, error)
}

// Database is the scanner-owned Glue Data Catalog database view. It carries
// safe identity, ownership, and timestamp metadata for catalog database
// resources.
type Database struct {
	CatalogID   string
	Name        string
	Description string
	LocationURI string
	CreateTime  time.Time
	Parameters  map[string]string
	Tables      []Table
}

// Table is the scanner-owned Glue Data Catalog table view. It excludes column
// statistics with sample values, partition value samples, and any field that
// can leak row-level content.
type Table struct {
	CatalogID        string
	DatabaseName     string
	Name             string
	Owner            string
	TableType        string
	Description      string
	CreateTime       time.Time
	UpdateTime       time.Time
	LastAccessTime   time.Time
	LastAnalyzedTime time.Time
	Retention        int32
	StorageLocation  string
	InputFormat      string
	OutputFormat     string
	Compressed       bool
	SerdeName        string
	SerdeLibrary     string
	Parameters       map[string]string
	PartitionKeys    []string
	Columns          []string
}

// Crawler is the scanner-owned Glue crawler view. The scanner records source
// target families but not raw S3 sample paths, JDBC strings, or catalog
// custom-classifier patterns.
type Crawler struct {
	Name                 string
	Description          string
	RoleARN              string
	DatabaseName         string
	TablePrefix          string
	State                string
	CreationTime         time.Time
	LastUpdated          time.Time
	Schedule             string
	RecrawlBehavior      string
	S3TargetCount        int
	JDBCTargetCount      int
	DynamoDBTargetCount  int
	CatalogTargetCount   int
	MongoDBTargetCount   int
	DeltaTargetCount     int
	IcebergTargetCount   int
	HudiTargetCount      int
	ConfigurationVersion string
}

// Job is the scanner-owned Glue job view. Job script bodies, default-argument
// values, and security-configuration secret material stay outside the
// contract.
type Job struct {
	Name                  string
	Description           string
	RoleARN               string
	GlueVersion           string
	WorkerType            string
	NumberOfWorkers       int32
	MaxCapacity           float64
	MaxRetries            int32
	Timeout               int32
	ScriptLanguage        string
	ScriptLocation        string
	CommandName           string
	CreatedOn             time.Time
	LastModifiedOn        time.Time
	SecurityConfiguration string
	NonOverridableArgKeys []string
	DefaultArgKeys        []string
}

// Trigger is the scanner-owned Glue trigger view. Triggers carry workflow
// membership and the list of job names invoked by each action.
type Trigger struct {
	Name         string
	Type         string
	State        string
	Description  string
	Schedule     string
	WorkflowName string
	ActionJobs   []string
}

// Workflow is the scanner-owned Glue workflow view. Workflow runs and graph
// payloads stay outside the contract.
type Workflow struct {
	Name             string
	Description      string
	CreatedOn        time.Time
	LastModifiedOn   time.Time
	DefaultRunKeys   []string
	MaxConcurrentRun int32
}

// Connection is the scanner-owned Glue connection view. Connection passwords,
// JDBC URLs that carry credentials, and any property value that smells like a
// secret stay outside the contract. Only property keys are recorded as
// presence evidence.
type Connection struct {
	Name                   string
	Description            string
	ConnectionType         string
	CreationTime           time.Time
	LastUpdatedTime        time.Time
	LastUpdatedBy          string
	MatchCriteria          []string
	PhysicalRequirementsAZ string
	SubnetID               string
	SecurityGroupIDs       []string
	PropertyKeys           []string
}
