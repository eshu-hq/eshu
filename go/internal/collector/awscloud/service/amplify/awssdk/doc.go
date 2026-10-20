// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Amplify client into the
// metadata-only Client the amplify scanner consumes.
//
// The adapter paginates ListApps, ListBranches, and ListDomainAssociations and
// maps each response into scanner-owned records. It drops every app and branch
// environment-variable map, build-spec body, and basic-auth credential, and
// reduces repository URLs to host and path so an embedded access token cannot
// leak. The adapter exposes no Create, Update, Delete, Start (job/deployment),
// Generate, or webhook API; a reflection guard test asserts the read surface
// stays metadata-only.
package awssdk
