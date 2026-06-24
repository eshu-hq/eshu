// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package doctruth extracts conservative documentation truth evidence,
// verification findings, and drift findings from bounded documentation
// sections.
//
// The package emits mention and claim-candidate evidence without treating prose
// as operational truth. Derived mention facts preserve bounded caller-supplied
// section provenance while keeping source start/end references tied to the
// section identity. Verifier compares explicit documentation claims such as CLI
// commands, HTTP endpoints, environment variables, explicit local repo paths,
// container image refs, and Terraform addresses with caller-supplied truth
// sources, then emits documentation_finding and documentation_evidence_packet
// facts.
// DeploymentDriftAnalyzer compares service_deployment claim candidates with
// caller-supplied Eshu truth and returns read-only findings that preserve match,
// conflict, ambiguous, unsupported, stale, and building states.
//
// When a caller sets SectionInput.SourceACLState from the owning
// source/document fact, the extractor propagates that bounded source access
// posture verbatim onto the derived mention and claim evidence facts'
// acl_summary, so the docs-evidence projection and readbacks carry the posture
// end-to-end. It is omitted when no bounded ACL claim was observed; this is
// factual propagation only and never a disclosure or enforcement decision.
package doctruth
