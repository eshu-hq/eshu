// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Transfer Family client into the
// metadata-only Transfer scanner interface.
//
// The adapter uses ListServers, DescribeServer, ListUsers, and DescribeUser
// only. It intentionally excludes CreateServer, UpdateServer, DeleteServer,
// StartServer, StopServer, CreateUser, UpdateUser, DeleteUser,
// ImportSshPublicKey, DeleteSshPublicKey, ImportHostKey, DeleteHostKey,
// ImportCertificate, and any other mutation or key-material API. Host key
// fingerprints, host key material, login banners, SSH public key bodies, user
// policy JSON, and POSIX UID/GID material are never copied into scanner-owned
// types even when DescribeServer or DescribeUser returns them.
package awssdk
