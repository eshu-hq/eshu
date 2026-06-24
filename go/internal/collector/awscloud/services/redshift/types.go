package redshift

import (
	"context"
	"time"
)

// Client lists metadata-only Redshift observations for one AWS claim. It covers
// both provisioned Redshift and Redshift Serverless control-plane reads.
type Client interface {
	ListClusters(ctx context.Context) ([]Cluster, error)
	ListClusterParameterGroups(ctx context.Context) ([]ClusterParameterGroup, error)
	ListClusterSubnetGroups(ctx context.Context) ([]ClusterSubnetGroup, error)
	ListClusterSnapshots(ctx context.Context) ([]ClusterSnapshot, error)
	ListScheduledActions(ctx context.Context) ([]ScheduledAction, error)
	ListServerlessNamespaces(ctx context.Context) ([]ServerlessNamespace, error)
	ListServerlessWorkgroups(ctx context.Context) ([]ServerlessWorkgroup, error)
}

// Cluster is the scanner-owned provisioned Redshift cluster model. It contains
// control-plane metadata only. The scanner deliberately omits master user
// passwords, secret values, warehouse query results, and table contents. The
// master username is intentionally not persisted because no downstream
// correlation depends on it.
type Cluster struct {
	ARN                              string
	Identifier                       string
	NodeType                         string
	ClusterStatus                    string
	ClusterAvailabilityStatus        string
	DBName                           string
	Endpoint                         string
	EndpointPort                     int32
	HostedZoneID                     string
	ClusterCreateTime                time.Time
	AutomatedSnapshotRetentionPeriod int32
	ManualSnapshotRetentionPeriod    int32
	ClusterSecurityGroups            []string
	VPCSecurityGroupIDs              []string
	ClusterParameterGroup            string
	ClusterSubnetGroupName           string
	VPCID                            string
	AvailabilityZone                 string
	PreferredMaintenanceWindow       string
	PendingModifiedValuesPresent     bool
	ClusterVersion                   string
	AllowVersionUpgrade              bool
	NumberOfNodes                    int32
	PubliclyAccessible               bool
	Encrypted                        bool
	KMSKeyID                         string
	EnhancedVPCRouting               bool
	IAMRoleARNs                      []string
	MaintenanceTrackName             string
	DeferredMaintenanceWindows       []string
	NextMaintenanceWindowStartTime   time.Time
	AvailabilityZoneRelocationStatus string
	MultiAZ                          bool
	Tags                             map[string]string
}

// ClusterParameterGroup is the scanner-owned Redshift cluster parameter group
// metadata model.
type ClusterParameterGroup struct {
	ARN         string
	Name        string
	Family      string
	Description string
	Tags        map[string]string
}

// ClusterSubnetGroup is the scanner-owned Redshift cluster subnet group
// metadata model.
type ClusterSubnetGroup struct {
	ARN         string
	Name        string
	VPCID       string
	Description string
	Status      string
	SubnetIDs   []string
	Tags        map[string]string
}

// ClusterSnapshot is the scanner-owned Redshift cluster snapshot metadata
// model. The scanner records snapshot identity, retention, and encryption
// metadata only; it never persists snapshot contents, query results, or table
// data.
type ClusterSnapshot struct {
	ARN                           string
	Identifier                    string
	ClusterIdentifier             string
	SnapshotType                  string
	Status                        string
	NodeType                      string
	NumberOfNodes                 int32
	DBName                        string
	VPCID                         string
	Encrypted                     bool
	KMSKeyID                      string
	SnapshotCreateTime            time.Time
	ClusterCreateTime             time.Time
	SnapshotRetentionStartTime    time.Time
	ManualSnapshotRetentionPeriod int32
	EngineFullVersion             string
	AvailabilityZone              string
	SourceRegion                  string
	Tags                          map[string]string
	RestorableNodeTypes           []string
}

// ScheduledAction is the scanner-owned Redshift scheduled action metadata
// model. It captures the action name, schedule, IAM role, and the target
// cluster identifier when the action targets one; mutation payload fields are
// not persisted.
type ScheduledAction struct {
	Name                    string
	Schedule                string
	IAMRoleARN              string
	Description             string
	State                   string
	StartTime               time.Time
	EndTime                 time.Time
	NextInvocationTime      time.Time
	TargetActionName        string
	TargetClusterIdentifier string
}

// ServerlessNamespace is the scanner-owned Redshift Serverless namespace
// metadata model.
type ServerlessNamespace struct {
	ARN            string
	Name           string
	NamespaceID    string
	Status         string
	DBName         string
	DefaultIAMRole string
	IAMRoleARNs    []string
	KMSKeyID       string
	LogExports     []string
	CreationDate   time.Time
	Tags           map[string]string
}

// ServerlessWorkgroup is the scanner-owned Redshift Serverless workgroup
// metadata model.
type ServerlessWorkgroup struct {
	ARN                string
	Name               string
	WorkgroupID        string
	NamespaceName      string
	Status             string
	BaseCapacity       int32
	MaxCapacity        int32
	EnhancedVPCRouting bool
	PubliclyAccessible bool
	ConfigParameters   []ServerlessConfigParameter
	SubnetIDs          []string
	SecurityGroupIDs   []string
	EndpointAddress    string
	EndpointPort       int32
	CreationDate       time.Time
	Tags               map[string]string
}

// ServerlessConfigParameter captures a non-secret Redshift Serverless
// workgroup parameter override.
type ServerlessConfigParameter struct {
	Key   string
	Value string
}
