// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workspaces maps Amazon WorkSpaces virtual-desktop, registered-
// directory, bundle, and IP-access-control-group metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for WorkSpaces, registered
// directories, account-owned bundles, and IP access control groups, plus
// relationships for workspace-in-directory, workspace-uses-bundle,
// workspace-uses-KMS-key, and the directory's links to the underlying Directory
// Service directory, placement subnets, assigned security group, WorkSpaces IAM
// role, and associated IP access control groups. Desktop session contents, user
// credentials, directory registration codes, connection state, and any mutation
// or session API stay outside this package contract: the scanner is
// metadata-only.
package workspaces
