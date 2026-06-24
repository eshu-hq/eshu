// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workflow defines the durable contracts for the workflow control
// plane: runs, work items, claims, collector instances, completeness states,
// and the reducer-facing phase contract per collector family.
//
// Types here are storage-neutral value contracts with Validate methods that
// enforce identity, status-lifecycle, and timestamp invariants. ControlStore
// is the durable surface implemented by storage/postgres. ReconcileRunProgress
// derives run status and completeness rows deterministically from bounded
// collector progress and reducer phase publications, including blocked
// completeness when terminal collector failures appear. The family fairness
// scheduler supplies deterministic claim targets across enabled collector
// instances; the collector claim-dispatch boundary synchronizes scheduler state
// and delegates each selected target to ControlStore.ClaimNextEligible so lease
// fencing remains in storage. Terraform state collector instances also validate
// their discovery config
// before reaching durable storage: they must enable graph discovery, explicit
// seeds, local repo limits, or backend filters. S3 seeds require bucket, key,
// region, and credentials through target scopes or a legacy AWS role ARN; S3
// backend filters require the same credential routing before runtime. OCI
// registry collector instances validate bounded repository targets, package
// registry collector instances validate bounded package metadata targets and
// known document formats, vulnerability intelligence collector instances
// validate bounded source targets, supported exact-version derivation
// ecosystems, mirror URLs, source-cache modes, and cache freshness durations,
// and security-alert collector instances validate repository allowlists plus
// HTTPS credentialed API base URLs before the coordinator plans claimable work
// items. Disabled PagerDuty and Jira collector instances validate only durable
// registration shape while enabled instances validate bounded account,
// service-allowlist, or site targets with credential environment references
// before the coordinator plans claimable evidence work. Optional PagerDuty
// live-configuration validation fields are checked at the same workflow
// boundary so service and integration reads stay bounded. Hosted work items may
// carry tenant, workspace, subject-class, and policy-revision identity together
// so storage and collectors can fence claim eligibility and claimed fact
// commits without changing graph truth per tenant.
package workflow
