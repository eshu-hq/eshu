// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mgn maps AWS Application Migration Service (MGN) control-plane
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for MGN applications, source
// servers, launch configurations, and jobs plus relationships for
// application-contains-source-server, source-server-launched-EC2-instance,
// launch-configuration-uses-launch-template, and job-targets-source-server
// evidence. Replication-agent credentials, replication configuration secrets,
// replicated disk contents, and every mutation or replication-control API stay
// outside this package contract: the scanner is metadata-only.
package mgn
