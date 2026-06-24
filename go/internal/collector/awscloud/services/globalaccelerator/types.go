// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package globalaccelerator

import (
	"context"
	"time"
)

// Client lists AWS Global Accelerator metadata for one claimed account. Runtime
// adapters translate AWS SDK responses into these scanner-owned types and never
// expose a mutation operation.
type Client interface {
	// ListAccelerators returns every accelerator visible to the configured
	// credentials, each with its listeners and endpoint groups already nested
	// so the scanner walks one topology snapshot.
	ListAccelerators(context.Context) ([]Accelerator, error)
}

// Accelerator is the metadata-only scanner view of a Global Accelerator
// accelerator. Listeners are nested so the scanner emits the full topology from
// one client call.
type Accelerator struct {
	ARN              string
	Name             string
	Status           string
	Enabled          bool
	IPAddressType    string
	DNSName          string
	DualStackDNSName string
	CreatedTime      time.Time
	LastModifiedTime time.Time
	IPSets           []IPSet
	Listeners        []Listener
	Tags             map[string]string
}

// IPSet captures one static IP address set for an accelerator. Static IP
// addresses are public anycast addresses, not secret material.
type IPSet struct {
	IPAddressFamily string
	IPAddresses     []string
}

// Listener is the metadata-only scanner view of a Global Accelerator listener.
// Endpoint groups are nested under the listener that owns them.
type Listener struct {
	ARN            string
	Protocol       string
	ClientAffinity string
	PortRanges     []PortRange
	EndpointGroups []EndpointGroup
}

// PortRange is one inclusive listener port range.
type PortRange struct {
	FromPort int32
	ToPort   int32
}

// EndpointGroup is the metadata-only scanner view of a Global Accelerator
// endpoint group. Endpoints are nested under the group that owns them.
type EndpointGroup struct {
	ARN                        string
	Region                     string
	TrafficDialPercentage      *float32
	HealthCheckProtocol        string
	HealthCheckPath            string
	HealthCheckPort            *int32
	HealthCheckIntervalSeconds *int32
	ThresholdCount             *int32
	PortOverrides              []PortOverride
	Endpoints                  []Endpoint
}

// PortOverride captures a listener-to-endpoint port remap reported by an
// endpoint group.
type PortOverride struct {
	ListenerPort int32
	EndpointPort int32
}

// Endpoint is the metadata-only scanner view of a Global Accelerator endpoint.
// EndpointID references an ALB/NLB ARN, an Elastic IP allocation id, or an EC2
// instance id depending on the endpoint type.
type Endpoint struct {
	EndpointID                  string
	Weight                      *int32
	ClientIPPreservationEnabled *bool
	HealthState                 string
}
