package msk

import (
	"context"
	"time"
)

// Client is the metadata-only MSK read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListClusters(context.Context) ([]Cluster, error)
	ListConfigurations(context.Context) ([]Configuration, error)
	ListReplicators(context.Context) ([]Replicator, error)
}

// Cluster is the scanner-owned representation of an MSK provisioned or
// serverless cluster.
type Cluster struct {
	ARN            string
	Name           string
	Type           string
	State          string
	CurrentVersion string
	CreationTime   time.Time
	Tags           map[string]string
	Provisioned    *ProvisionedCluster
	Serverless     *ServerlessCluster
}

// ProvisionedCluster carries MSK provisioned-mode metadata.
type ProvisionedCluster struct {
	KafkaVersion           string
	EnhancedMonitoring     string
	NumberOfBrokerNodes    int32
	BrokerNodeGroup        BrokerNodeGroup
	EncryptionAtRestKMSKey string
	EncryptionInTransit    EncryptionInTransit
	ClientAuthentication   ClientAuthentication
	CurrentConfiguration   *ConfigurationReference
	StorageMode            string
}

// ServerlessCluster carries MSK serverless-mode metadata.
type ServerlessCluster struct {
	VPCConfigs           []VPCConfig
	ClientAuthentication ClientAuthentication
}

// BrokerNodeGroup captures MSK provisioned broker placement metadata used for
// EC2 topology joins.
type BrokerNodeGroup struct {
	InstanceType     string
	ClientSubnets    []string
	SecurityGroupIDs []string
	StorageGiB       int32
}

// VPCConfig captures one MSK serverless VPC placement reported by AWS.
type VPCConfig struct {
	SubnetIDs        []string
	SecurityGroupIDs []string
}

// EncryptionInTransit captures MSK transport encryption metadata without
// persisting certificate bodies.
type EncryptionInTransit struct {
	ClientBroker string
	InCluster    bool
}

// ClientAuthentication captures MSK auth-method enablement flags without
// reading SCRAM secrets or TLS certificate bodies.
type ClientAuthentication struct {
	SASLIAMEnabled            bool
	SASLSCRAMEnabled          bool
	TLSEnabled                bool
	TLSCertificateAuthorities []string
	UnauthenticatedEnabled    bool
}

// ConfigurationReference captures the broker configuration ARN and revision
// currently applied to an MSK cluster. Raw server.properties bodies stay out
// of the scanner contract.
type ConfigurationReference struct {
	ARN      string
	Revision int64
}

// Configuration is the scanner-owned representation of an MSK broker
// configuration. The scanner emits identity and revision metadata; the raw
// server.properties body is never persisted because it can carry passwords or
// secret-shaped patterns.
type Configuration struct {
	ARN            string
	Name           string
	Description    string
	State          string
	CreationTime   time.Time
	KafkaVersions  []string
	LatestRevision ConfigurationRevisionSummary
}

// ConfigurationRevisionSummary identifies one MSK configuration revision
// without exposing the revision body.
type ConfigurationRevisionSummary struct {
	Revision     int64
	CreationTime time.Time
	Description  string
}

// Replicator is the scanner-owned representation of an MSK Replicator.
type Replicator struct {
	ARN                     string
	Name                    string
	State                   string
	CurrentVersion          string
	CreationTime            time.Time
	ServiceExecutionRoleARN string
	Tags                    map[string]string
	KafkaClusters           []ReplicatorKafkaCluster
	ReplicationInfo         []ReplicationInfo
}

// ReplicatorKafkaCluster identifies one source or target cluster registered
// with an MSK Replicator. Bootstrap broker strings for non-MSK clusters are
// excluded to keep the scanner free of broker-endpoint material.
type ReplicatorKafkaCluster struct {
	Alias               string
	MSKClusterARN       string
	VPCSubnetIDs        []string
	VPCSecurityGroupIDs []string
}

// ReplicationInfo captures the per-pair replication settings reported by AWS
// without persisting per-topic detail beyond aggregate filter counts.
type ReplicationInfo struct {
	SourceClusterARN                 string
	TargetClusterARN                 string
	SourceAlias                      string
	TargetAlias                      string
	TargetCompression                string
	TopicIncludePatternCount         int
	TopicExcludePatternCount         int
	ConsumerGroupIncludePatternCount int
	ConsumerGroupExcludePatternCount int
}
