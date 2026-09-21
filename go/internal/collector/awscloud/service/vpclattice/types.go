// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpclattice

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon VPC Lattice service network, service,
// target group, and listener observations for one AWS claim. Implementations
// read control-plane list/get metadata only and never read or persist
// auth-policy bodies, resource-policy bodies, or any data-plane payload.
type Client interface {
	// Snapshot returns every VPC Lattice service network, service, and target
	// group visible to the configured AWS credentials, each carrying the
	// metadata and association evidence used to emit resources and edges.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures VPC Lattice control-plane metadata plus non-fatal scan
// warnings for one boundary.
type Snapshot struct {
	// ServiceNetworks is the metadata-only set of VPC Lattice service networks,
	// each carrying its VPC and service associations.
	ServiceNetworks []ServiceNetwork
	// Services is the metadata-only set of VPC Lattice services, each carrying
	// its listeners and certificate reference.
	Services []Service
	// TargetGroups is the metadata-only set of VPC Lattice target groups, each
	// carrying its backing VPC, served services, and registered targets.
	TargetGroups []TargetGroup
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// ServiceNetwork is the scanner-owned VPC Lattice service network model. It
// carries control-plane metadata only.
type ServiceNetwork struct {
	// ARN is the Amazon Resource Name that uniquely identifies the network.
	ARN string
	// ID is the service network id (sn-...).
	ID string
	// Name is the service network name.
	Name string
	// NumberOfAssociatedServices is the count of associated services AWS reports.
	NumberOfAssociatedServices int64
	// NumberOfAssociatedVPCs is the count of associated VPCs AWS reports.
	NumberOfAssociatedVPCs int64
	// NumberOfAssociatedResourceConfigurations is the count of associated
	// resource configurations AWS reports.
	NumberOfAssociatedResourceConfigurations int64
	// CreatedAt is when the service network was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the service network was last updated.
	LastUpdatedAt time.Time
	// Tags carries the service network resource tags.
	Tags map[string]string
	// VPCAssociations are the metadata-only VPC associations on this network.
	VPCAssociations []VPCAssociation
	// ServiceAssociations are the metadata-only service associations on this
	// network.
	ServiceAssociations []ServiceAssociation
}

// VPCAssociation captures one service-network-to-VPC association. AWS reports
// the bare VPC id, which is the resource_id the EC2 scanner publishes for a VPC.
type VPCAssociation struct {
	// ID is the association id.
	ID string
	// VPCID is the bare VPC id (vpc-...) the network is associated with.
	VPCID string
	// Status is the association status AWS reports.
	Status string
}

// ServiceAssociation captures one service-network-to-service association. AWS
// reports the service ARN, which is the resource_id this scanner publishes for
// a VPC Lattice service.
type ServiceAssociation struct {
	// ID is the association id.
	ID string
	// ServiceARN is the associated VPC Lattice service ARN.
	ServiceARN string
	// ServiceID is the associated service id (svc-...).
	ServiceID string
	// Status is the association status AWS reports.
	Status string
}

// Service is the scanner-owned VPC Lattice service model. It carries
// control-plane metadata only and never the auth-policy body.
type Service struct {
	// ARN is the Amazon Resource Name that uniquely identifies the service.
	ARN string
	// ID is the service id (svc-...).
	ID string
	// Name is the service name.
	Name string
	// Status is the service status AWS reports.
	Status string
	// CustomDomainName is the optional custom domain name configured on the
	// service.
	CustomDomainName string
	// DNSEntryDomainName is the VPC Lattice-generated DNS entry domain name.
	DNSEntryDomainName string
	// AuthType is the service auth type (NONE or AWS_IAM). The auth-policy body
	// is never read.
	AuthType string
	// CertificateARN is the ACM certificate ARN bound to the custom domain, when
	// configured.
	CertificateARN string
	// CreatedAt is when the service was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the service was last updated.
	LastUpdatedAt time.Time
	// Tags carries the service resource tags.
	Tags map[string]string
	// Listeners are the metadata-only listeners that live under this service.
	Listeners []Listener
}

// Listener is the scanner-owned VPC Lattice listener model. It carries
// control-plane metadata only and never expands rule action bodies.
type Listener struct {
	// ARN is the Amazon Resource Name that uniquely identifies the listener.
	ARN string
	// ID is the listener id (listener-...).
	ID string
	// Name is the listener name.
	Name string
	// Protocol is the listener protocol (HTTP, HTTPS, TLS_PASSTHROUGH).
	Protocol string
	// Port is the listener port.
	Port int32
}

// TargetGroup is the scanner-owned VPC Lattice target group model. It carries
// control-plane metadata only.
type TargetGroup struct {
	// ARN is the Amazon Resource Name that uniquely identifies the target group.
	ARN string
	// ID is the target group id (tg-...).
	ID string
	// Name is the target group name.
	Name string
	// Type is the target group type (IP, LAMBDA, INSTANCE, ALB).
	Type string
	// Protocol is the target group protocol, when reported.
	Protocol string
	// Port is the target group port, when reported.
	Port int32
	// IPAddressType is the target group IP address type, when reported.
	IPAddressType string
	// Status is the target group status AWS reports.
	Status string
	// VPCID is the bare backing VPC id (vpc-...), when the type uses a VPC.
	VPCID string
	// ServiceARNs are the VPC Lattice service ARNs that use this target group.
	ServiceARNs []string
	// CreatedAt is when the target group was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the target group was last updated.
	LastUpdatedAt time.Time
	// Tags carries the target group resource tags.
	Tags map[string]string
	// Targets are the metadata-only registered targets. Each target id resolves
	// to a Lambda function ARN, a bare EC2 instance id, an ALB ARN, or an IP
	// address depending on the target group type.
	Targets []Target
}

// Target captures one registered target's membership identity. The scanner
// records only the target id, port, and status, never any data-plane payload.
type Target struct {
	// ID is the registered target id. For a LAMBDA target group it is the Lambda
	// function ARN; for INSTANCE the bare instance id; for ALB the load balancer
	// ARN; for IP an IP address.
	ID string
	// Port is the target port, when reported.
	Port int32
	// Status is the target health status AWS reports.
	Status string
}
