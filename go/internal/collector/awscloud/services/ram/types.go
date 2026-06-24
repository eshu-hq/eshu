// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ram

import (
	"context"
	"time"
)

// Client is the metadata-only AWS Resource Access Manager read surface consumed
// by Scanner. Runtime adapters translate AWS SDK responses into these
// scanner-owned records. The contract exposes no mutation and never returns a
// permission policy document body.
type Client interface {
	// ListResourceShares returns the resource shares owned by the claimed
	// account (resource owner SELF), including each share's associated shared
	// resources, principals, and managed-permission summaries.
	ListResourceShares(context.Context) ([]ResourceShare, error)
}

// ResourceShare is the scanner-owned representation of one RAM resource share.
// It carries share identity and configuration metadata only. Permission policy
// document bodies are intentionally outside this contract.
type ResourceShare struct {
	ARN                     string
	Name                    string
	Status                  string
	StatusMessage           string
	OwningAccountID         string
	AllowExternalPrincipals bool
	FeatureSet              string
	CreationTime            time.Time
	LastUpdatedTime         time.Time
	Tags                    map[string]string
	Resources               []SharedResource
	Principals              []Principal
	Permissions             []Permission
}

// SharedResource is the scanner-owned representation of one resource associated
// with a resource share. Type takes the RAM service-code:resource-code form
// (for example ec2:subnet).
type SharedResource struct {
	ARN         string
	Type        string
	Status      string
	RegionScope string
}

// Principal is the scanner-owned representation of one principal associated with
// a resource share. ID is an AWS account id, an Organizations OU ARN, or an
// Organizations organization or root ARN. External reports whether the
// principal is outside the share owner's organization.
type Principal struct {
	ID       string
	External bool
}

// Permission is the scanner-owned representation of one managed permission
// associated with a resource share. It carries permission metadata only; the
// permission policy document body is never read or persisted.
type Permission struct {
	ARN            string
	Name           string
	Version        string
	PermissionType string
	ResourceType   string
	Status         string
	DefaultVersion bool
}
