// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lightsail

import (
	"context"
	"time"
)

// Client lists metadata-only Amazon Lightsail observations for one claimed
// account and region. Every method maps to a read-only Lightsail Get* API; the
// interface deliberately excludes every create, delete, reboot, start, stop,
// snapshot, and access-key API so a mutation can never be reached through the
// scanner.
type Client interface {
	ListInstances(ctx context.Context) ([]Instance, error)
	ListDatabases(ctx context.Context) ([]Database, error)
	ListLoadBalancers(ctx context.Context) ([]LoadBalancer, error)
	ListDisks(ctx context.Context) ([]Disk, error)
	ListStaticIPs(ctx context.Context) ([]StaticIP, error)
}

// Instance is the scanner-owned Lightsail instance view. It carries safe
// identity, placement, blueprint/bundle sizing, and reported networking
// addresses. Instance access keys, default key-pair private material, and user
// data stay outside the contract.
type Instance struct {
	ARN              string
	Name             string
	BlueprintID      string
	BlueprintName    string
	BundleID         string
	State            string
	PublicIPAddress  string
	PrivateIPAddress string
	IPv6Addresses    []string
	IsStaticIP       bool
	AvailabilityZone string
	RegionName       string
	SSHKeyName       string
	CreatedAt        time.Time
	Tags             map[string]string
}

// Database is the scanner-owned Lightsail managed relational database view.
// Master user passwords, certificate bodies, and endpoint credential material
// stay outside the contract; only the host endpoint address and port survive.
type Database struct {
	ARN                string
	Name               string
	Engine             string
	EngineVersion      string
	State              string
	BlueprintID        string
	BundleID           string
	MasterDatabaseName string
	MasterUsername     string
	EndpointAddress    string
	EndpointPort       *int32
	PubliclyAccessible bool
	BackupRetention    bool
	AvailabilityZone   string
	SecondaryAZ        string
	RegionName         string
	CreatedAt          time.Time
	Tags               map[string]string
}

// LoadBalancer is the scanner-owned Lightsail load balancer view. Attached
// carries the bare names of the Lightsail instances AWS reports in the load
// balancer's instance-health summary, which the relationship layer keys the
// load-balancer-to-instance edges on.
type LoadBalancer struct {
	ARN              string
	Name             string
	State            string
	DNSName          string
	Protocol         string
	InstancePort     *int32
	PublicPorts      []int32
	IPAddressType    string
	HTTPSRedirection bool
	AvailabilityZone string
	RegionName       string
	CreatedAt        time.Time
	Attached         []string
	Tags             map[string]string
}

// Disk is the scanner-owned Lightsail block-storage disk view. AttachedTo
// carries the bare name of the Lightsail instance AWS reports the disk attached
// to, which the relationship layer keys the instance-to-disk edge on.
type Disk struct {
	ARN              string
	Name             string
	State            string
	Path             string
	SizeInGb         *int32
	IOPS             *int32
	IsAttached       bool
	IsSystemDisk     bool
	AttachedTo       string
	AvailabilityZone string
	RegionName       string
	CreatedAt        time.Time
	Tags             map[string]string
}

// StaticIP is the scanner-owned Lightsail static IP view. AttachedTo carries
// the bare name of the Lightsail instance AWS reports the static IP attached
// to, which the relationship layer keys the instance-to-static-IP edge on.
type StaticIP struct {
	ARN              string
	Name             string
	IPAddress        string
	IsAttached       bool
	AttachedTo       string
	AvailabilityZone string
	RegionName       string
	CreatedAt        time.Time
}
