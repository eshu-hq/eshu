// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsruntime adapts AWS cloud service scanners to workflow-claimed
// collector execution.
//
// The package owns claim parsing, target authorization, claim-scoped
// credential acquisition, scanner-side status updates, and collected-generation
// construction for AWS cloud work items. It also owns per-account concurrency,
// credential lease release, pagination checkpoint expiry, and a package-level
// scanner registry that production runtimes populate at process start
// through service runtimebind packages.
//
// Service scanners own AWS source observation and reducers own canonical graph
// truth. SupportedServiceKinds and SupportsServiceKind report the registered
// production scanner set, including metadata-only families such as GuardDuty,
// to command-side startup validation. ServiceRequiresRedactionKey and
// ServiceKindsRequiringRedactionKey report which scanners declared
// ScannerRegistration.RequiresRedactionKey, so the command derives the
// ESHU_AWS_REDACTION_KEY requirement from the registry instead of a
// hand-maintained service list. The collector-aws-cloud command blank-imports
// awsruntime/bindings to install every scanner before DefaultScannerFactory
// dispatches the first claim. Top-level Smithy access-denied and unsupported
// operation responses classify as terminal permission gaps for the claimed
// scope; transient transport failures remain retryable through the shared
// ClaimedService budget.
//
// For fully offline replay the package also exports FixtureSource, a
// collector.Source that needs no credentials, no AWS SDK, and no network. It
// converts a declarative FixtureConfig (FixtureScope, FixtureResource,
// FixtureRelationship) into the same aws_resource / aws_relationship envelopes
// the live scanners emit by reusing awscloud.NewResourceEnvelope and
// awscloud.NewRelationshipEnvelope. Generation ids derive deterministically
// from the scope id, never the clock, so re-ingest is idempotent and CI is
// reproducible. The collector-aws-cloud command wires FixtureSource into a
// non-claimed collector.Service when run with -mode fixture.
package awsruntime
