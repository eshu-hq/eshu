// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elb

import (
	"context"
	"time"
)

// Client is the Classic ELB (v1) read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types. The
// interface is intentionally read-only: it exposes a single bulk read so a
// reflective guard test can prove no create, delete, register, deregister, or
// other mutation operation is reachable from the scanner.
type Client interface {
	// ListLoadBalancers returns every Classic load balancer visible to the
	// configured AWS credentials, with reported listeners, registered instances,
	// subnets, security groups, and tags already attached.
	ListLoadBalancers(context.Context) ([]LoadBalancer, error)
}

// LoadBalancer is the scanner-owned representation of a Classic (v1) Elastic
// Load Balancer. Classic ELBs have no AWS-assigned ARN; the scanner synthesizes
// a partition-aware ARN from the boundary and the load balancer name.
type LoadBalancer struct {
	// Name is the load balancer name, unique per account and region. It is the
	// only stable identity AWS reports for a Classic ELB.
	Name string
	// DNSName is the public or internal DNS name AWS assigned to the load
	// balancer.
	DNSName string
	// CanonicalHostedZoneName is the Route 53 hosted zone DNS name for the load
	// balancer.
	CanonicalHostedZoneName string
	// CanonicalHostedZoneNameID is the Route 53 hosted zone id for the load
	// balancer.
	CanonicalHostedZoneNameID string
	// Scheme is "internet-facing" or "internal".
	Scheme string
	// VPCID is the VPC the load balancer is placed in, when it is VPC-attached.
	VPCID string
	// CreatedAt is the load balancer creation time.
	CreatedAt time.Time
	// AvailabilityZones lists the Availability Zone names the load balancer spans.
	AvailabilityZones []string
	// Subnets lists the subnet ids the load balancer is attached to.
	Subnets []string
	// SecurityGroups lists the security group ids attached to the load balancer.
	SecurityGroups []string
	// SourceSecurityGroupName is the name of the load-balancer-owned source
	// security group instances can allow inbound from. It is reported as a group
	// name (not an id), so it is carried as an attribute, not a graph edge.
	SourceSecurityGroupName string
	// SourceSecurityGroupOwnerAlias is the owner account alias of the source
	// security group.
	SourceSecurityGroupOwnerAlias string
	// InstanceIDs lists the registered EC2 instance ids reported by
	// DescribeLoadBalancers. Live health status is intentionally excluded.
	InstanceIDs []string
	// Listeners are the reported listener configurations. Classic ELB listeners
	// have no ARN; they are carried as load-balancer attributes.
	Listeners []Listener
	// HealthCheck is the reported health-check configuration, not live instance
	// health.
	HealthCheck HealthCheck
	// Tags are the load balancer tags.
	Tags map[string]string
}

// Listener is the scanner-owned representation of a Classic ELB listener. A
// Classic ELB listener has no independent identity, so it is embedded in the
// owning load balancer's attributes rather than emitted as its own resource.
type Listener struct {
	// Protocol is the front-end (load balancer) protocol: HTTP, HTTPS, TCP, or
	// SSL.
	Protocol string
	// LoadBalancerPort is the front-end port the load balancer listens on.
	LoadBalancerPort int32
	// InstanceProtocol is the back-end protocol used to reach instances.
	InstanceProtocol string
	// InstancePort is the back-end port traffic is routed to on instances.
	InstancePort int32
	// SSLCertificateID is the ARN of the server certificate for an HTTPS/SSL
	// listener. It is an ACM certificate ARN or an IAM server-certificate ARN.
	// The certificate body and private key are never read or persisted.
	SSLCertificateID string
}

// HealthCheck captures a Classic ELB's health-check configuration. It is the
// configured probe, not the live per-instance health result.
type HealthCheck struct {
	// Target is the configured probe target string, for example "HTTP:80/health"
	// or "TCP:443".
	Target string
	// IntervalSeconds is the probe interval in seconds.
	IntervalSeconds int32
	// TimeoutSeconds is the probe timeout in seconds.
	TimeoutSeconds int32
	// HealthyThreshold is the number of consecutive successes before an instance
	// is marked healthy.
	HealthyThreshold int32
	// UnhealthyThreshold is the number of consecutive failures before an instance
	// is marked unhealthy.
	UnhealthyThreshold int32
}
