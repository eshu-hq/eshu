// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ssmhost materializes admitted SSM host inventory observations as
// canonical graph truth (eshu-hq/eshu#9558, accepted record #9302).
//
// One admitted aws_ssm_host_application fact is one allowlisted
// AWS:Application inventory row for one EC2 managed node in one dated
// capture; one aws_ssm_host_platform fact is that host's platform evidence
// with its provenance. The projection writes one SSMHostApplication node per
// admitted row (uid: host tuple plus capture time plus canonical row hash),
// one SSMHostPlatform node per host and provenance (uid: host tuple plus
// source type), and one REPORTED_WITH edge per application row to each
// skew-paired platform node.
//
// Supersession is per host and type: only a fresh complete application
// capture newer than the committed one supersedes, retracting older captures'
// application nodes and conflicted platform nodes. Mixed-capture, aged,
// duplicate, and silent generations preserve the prior graph truth untouched.
// Conflicting platform content for one host and provenance stays ambiguous
// (its prior node retracts, no replacement) without blocking the coherent
// application capture; an out-of-skew platform still projects, unpaired.
// Status (successful, partial, stale, unknown) is always derived at read time
// from admitted facts, never stored, so the graph and the query surface agree
// by construction.
//
// These are observations, not package findings: no vulnerability verdict, no
// supply-chain impact, no PURL, and no image digest is ever synthesized here.
// Host vulnerability correlation belongs to #9209.
//
// The field spelling mirrors the #9556 typed contracts
// (sdk/go/factschema/aws/v1) until the eshu-sdk release carrying these kinds
// lets this seam decode through schemadecode like the other AWS kinds. The
// stable-key oracles in the payload tests pin the encoding byte-identical to
// the contract functions.
//
// The exported surface is [MaterializationDomainDefinition],
// [MaterializationHandler], [HostGraphWriter], [HostGraphWrite],
// [HostGraphRetract], [CommittedHost], [ExtractObservations],
// [ProjectHosts], [HostProjection], [ProjectionTally], [Bounds],
// [DecodeApplication], [DecodePlatform], [ApplicationStableKey],
// [PlatformStableKey], [PlatformUID], [CanonicalApplicationRowHash], and
// [HostKey]. This package never imports internal/reducer.
package ssmhost
