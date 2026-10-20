// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package drs maps AWS Elastic Disaster Recovery (DRS) source server, recovery
// instance, and replication configuration template metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for DRS source servers,
// recovery instances, and replication configuration templates, plus relationship
// evidence for source-server-to-recovery-instance association and
// recovery-instance-to-EC2-instance backing. Replication agent secrets,
// replicated disk data, point-in-time snapshot contents, and any recover/start/
// stop/mutation API stay outside this package contract: the scanner is
// metadata-only.
package drs
