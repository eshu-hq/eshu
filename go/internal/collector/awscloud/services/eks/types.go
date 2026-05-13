package eks

import (
	"context"
	"time"
)

// Client is the EKS read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListClusters(context.Context) ([]Cluster, error)
	ListNodegroups(context.Context, Cluster) ([]Nodegroup, error)
	ListAddons(context.Context, Cluster) ([]Addon, error)
}

// Cluster is the scanner-owned representation of an EKS cluster.
type Cluster struct {
	ARN             string
	Name            string
	Version         string
	PlatformVersion string
	Status          string
	Endpoint        string
	RoleARN         string
	CreatedAt       time.Time
	OIDCProvider    *OIDCProvider
	VPCConfig       VPCConfig
	Tags            map[string]string
}

// OIDCProvider captures EKS-associated IAM OIDC provider evidence for IRSA
// trust-chain joins.
type OIDCProvider struct {
	ARN         string
	IssuerURL   string
	Thumbprints []string
	ClientIDs   []string
}

// VPCConfig carries EKS control-plane networking evidence for EC2 topology
// joins.
type VPCConfig struct {
	VPCID                  string
	SubnetIDs              []string
	SecurityGroupIDs       []string
	ClusterSecurityGroupID string
	EndpointPublicAccess   bool
	EndpointPrivateAccess  bool
	PublicAccessCIDRs      []string
}

// Nodegroup is the scanner-owned representation of an EKS managed node group.
type Nodegroup struct {
	ARN            string
	Name           string
	ClusterName    string
	Version        string
	ReleaseVersion string
	Status         string
	NodeRoleARN    string
	CapacityType   string
	AMIType        string
	InstanceTypes  []string
	Subnets        []string
	ScalingConfig  ScalingConfig
	Tags           map[string]string
}

// ScalingConfig captures desired, minimum, and maximum managed-node counts.
type ScalingConfig struct {
	DesiredSize int32
	MinSize     int32
	MaxSize     int32
}

// Addon is the scanner-owned representation of an EKS managed add-on.
type Addon struct {
	ARN                   string
	Name                  string
	ClusterName           string
	Version               string
	Status                string
	ServiceAccountRoleARN string
	CreatedAt             time.Time
	ModifiedAt            time.Time
	Tags                  map[string]string
}
