// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychain holds the OpenAPI path fragments for the
// supply-chain routes: container image inventory (Images, ContainerImages),
// tag history (TagHistory), vulnerability scanner metadata
// (VulnerabilityScannerContract), impact analysis (ImpactAggregate, plus the
// unexported impactFindings and impactExplain), security alerts (SecurityAlerts,
// SecurityAlertAggregate), container image identity (
// ContainerImageIdentityAggregate), advisories (AdvisoryCatalog,
// AdvisoryEvidence), SBOM attestations (SBOMAttestations,
// SBOMAttestationAttachmentAggregate), and suppression mutations (the
// unexported suppressionMutation). Routes composes impactFindings,
// impactExplain and suppressionMutation rather than holding a fragment of its
// own — the one file in this package where that is true. Those three stay
// unexported because nothing outside this package reads them.
//
// impact_findings.go and impact_explain.go share the RuntimeContext fragment
// declared in this package's runtime_context.go; no other file here or
// outside this package references it. This package MUST NOT import the
// openapi parent. At 16 files it is the largest leaf under paths/ and
// closest to the 40-non-test-file directory cap the dirgate linter
// enforces; check the cap before adding another fragment here.
package supplychain
