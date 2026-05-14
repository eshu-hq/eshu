package rds

import "context"

// Client lists metadata-only RDS observations for one AWS claim.
type Client interface {
	ListDBInstances(ctx context.Context) ([]DBInstance, error)
	ListDBClusters(ctx context.Context) ([]DBCluster, error)
	ListDBSubnetGroups(ctx context.Context) ([]DBSubnetGroup, error)
}

// DBInstance is the scanner-owned RDS DB instance model. It contains
// control-plane metadata only and intentionally excludes database names,
// usernames, connection secrets, snapshots, logs, schemas, tables, and data.
type DBInstance struct {
	ARN                              string
	Identifier                       string
	ResourceID                       string
	Class                            string
	Engine                           string
	EngineVersion                    string
	Status                           string
	EndpointAddress                  string
	EndpointPort                     int32
	HostedZoneID                     string
	AvailabilityZone                 string
	SecondaryAvailabilityZone        string
	MultiAZ                          bool
	PubliclyAccessible               bool
	StorageEncrypted                 bool
	KMSKeyID                         string
	IAMDatabaseAuthenticationEnabled bool
	DeletionProtection               bool
	BackupRetentionPeriod            int32
	DBSubnetGroupName                string
	VPCID                            string
	VPCSecurityGroupIDs              []string
	ClusterIdentifier                string
	ParameterGroups                  []ParameterGroup
	OptionGroups                     []OptionGroup
	MonitoringRoleARN                string
	PerformanceInsightsEnabled       bool
	PerformanceInsightsKMSKeyID      string
	Tags                             map[string]string
}

// DBCluster is the scanner-owned RDS DB cluster model. It contains
// control-plane metadata only and intentionally excludes database usernames,
// secrets, snapshots, logs, schemas, tables, and data.
type DBCluster struct {
	ARN                              string
	Identifier                       string
	ResourceID                       string
	Engine                           string
	EngineVersion                    string
	Status                           string
	EndpointAddress                  string
	ReaderEndpointAddress            string
	HostedZoneID                     string
	Port                             int32
	MultiAZ                          bool
	StorageEncrypted                 bool
	KMSKeyID                         string
	IAMDatabaseAuthenticationEnabled bool
	DeletionProtection               bool
	BackupRetentionPeriod            int32
	DBSubnetGroupName                string
	VPCSecurityGroupIDs              []string
	Members                          []ClusterMember
	ParameterGroup                   string
	AssociatedRoleARNs               []string
	Tags                             map[string]string
}

// DBSubnetGroup is the scanner-owned RDS DB subnet group model.
type DBSubnetGroup struct {
	ARN         string
	Name        string
	Description string
	Status      string
	VPCID       string
	SubnetIDs   []string
	Tags        map[string]string
}

// ParameterGroup is reported parameter-group membership metadata.
type ParameterGroup struct {
	Name  string
	State string
}

// OptionGroup is reported option-group membership metadata.
type OptionGroup struct {
	Name  string
	State string
}

// ClusterMember is reported DB cluster membership metadata.
type ClusterMember struct {
	DBInstanceIdentifier string
	IsWriter             bool
}
