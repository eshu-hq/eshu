// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

import "context"

// Client lists AWS Directory Service metadata for one claimed account and
// region. It is the scanner-facing surface that adapter packages implement; the
// contract is intentionally narrow so the scanner cannot reach for directory
// admin passwords, the RADIUS shared secret, or any mutation API
// (ResetUserPassword, Create/Delete/Update/Enable/Disable/...).
type Client interface {
	// ListDirectories returns the account's directories visible to the
	// configured credentials.
	ListDirectories(ctx context.Context) ([]Directory, error)
	// ListTrusts returns the trust relationships for one directory id.
	ListTrusts(ctx context.Context, directoryID string) ([]Trust, error)
	// ListSharedDirectories returns the share invitations owned by one directory
	// id. Only the directory owner account observes shares for its directories.
	ListSharedDirectories(ctx context.Context, ownerDirectoryID string) ([]SharedDirectory, error)
	// ListLDAPSSettings returns the client-side LDAPS settings for one directory
	// id. The result is empty for directory types that do not support LDAPS.
	ListLDAPSSettings(ctx context.Context, directoryID string) ([]LDAPSSetting, error)
}

// Directory is the scanner-owned AWS Directory Service directory model. It
// carries control-plane metadata only and intentionally excludes the directory
// admin password, the RADIUS shared secret, the AD Connector service-account
// password, and any other secret material. AWS's DescribeDirectories response
// never returns the admin password; the RADIUS shared secret is excluded by not
// mapping RadiusSettings at all.
type Directory struct {
	ID                string
	Name              string
	ShortName         string
	Type              string
	Edition           string
	Size              string
	Stage             string
	Description       string
	AccessURL         string
	Alias             string
	LDAPSStatuses     []string
	VPCID             string
	SubnetIDs         []string
	SecurityGroupID   string
	AvailabilityZones []string
	ShareMethod       string
	ShareStatus       string
	SsoEnabled        bool
	Tags              map[string]string
}

// Trust is the scanner-owned AWS Directory Service trust relationship model.
type Trust struct {
	ID               string
	DirectoryID      string
	RemoteDomainName string
	Direction        string
	Type             string
	State            string
	SelectiveAuth    string
}

// SharedDirectory is the scanner-owned AWS Directory Service shared directory
// model. The ShareNotes typed message is intentionally excluded because it is a
// free-form operator field, not metadata.
type SharedDirectory struct {
	OwnerAccountID    string
	OwnerDirectoryID  string
	SharedAccountID   string
	SharedDirectoryID string
	ShareMethod       string
	ShareStatus       string
}

// LDAPSSetting is the scanner-owned client-side LDAPS settings model for one
// directory. It records status only; no certificate material is read.
type LDAPSSetting struct {
	Status string
}
